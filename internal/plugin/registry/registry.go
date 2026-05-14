// Package registry implements the plugin index protocol Samuel v2 uses
// to discover installable plugins.
//
// The on-disk schema is `index.toml` (one file per registry). Two TOML
// shapes are accepted; both decode into the same in-memory Index:
//
// Array-of-tables (preferred, matches the official registry generator):
//
//	schema_version = 1
//
//	[[plugins]]
//	name        = "go-guide"
//	repo        = "github.com/samuelpkg/samuel-go-guide"
//	latest      = "1.4.2"
//	description = "Go language guardrails and patterns"
//	categories  = ["language"]
//	tags        = ["go", "golang"]
//	kind        = "skill"           # optional
//
// Map-of-tables (legacy):
//
//	[plugin.mcp-builder]
//	repo = "github.com/anthropics/skills"
//	subpath = "mcp-builder"
//	latest = "main"
//	upstream = true
//
// When both shapes appear in the same file, array entries override map
// entries on name collision (array is treated as the newer source).
//
// A registry source URL can be:
//
//   - "github.com/<owner>/<repo>"  — fetched as
//     https://raw.githubusercontent.com/<owner>/<repo>/<branch>/index.toml
//   - "https://...index.toml"      — fetched as-is
//   - "file:///abs/path/index.toml" — local fixture (tests)
//
// Indexes are cached at ~/.samuel/cache/registries/<host>/<path>/index.toml
// and refreshed when older than ttl (default 24h) or on `samuel update`.
// Multi-registry resolution is first-match-wins by plugin name; the
// caller iterates Sources in declared order.
package registry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/samuelpkg/samuel/internal/errors"
	"github.com/samuelpkg/samuel/internal/plugin/semver"
)

// Component is the structured-error namespace for this package.
const Component = "plugin/registry"

// IndexFileName is the on-disk filename inside a registry repo.
const IndexFileName = "index.toml"

// DefaultTTL is how long a cached index is treated as fresh.
const DefaultTTL = 24 * time.Hour

// Index is one parsed index.toml.
type Index struct {
	SchemaVersion int               `toml:"schema_version"`
	Plugins       map[string]Plugin `toml:"plugin"`
}

// Plugin is one entry under [plugin.<name>].
type Plugin struct {
	Repo        string   `toml:"repo"`
	Subpath     string   `toml:"subpath,omitempty"`
	Latest      string   `toml:"latest"`
	Versions    []string `toml:"versions,omitempty"`
	Description string   `toml:"description,omitempty"`
	Categories  []string `toml:"categories,omitempty"`
	Tags        []string `toml:"tags,omitempty"`
	Kind        string   `toml:"kind,omitempty"`
	Upstream    bool     `toml:"upstream,omitempty"`
	// SignatureBundle is the URL of the sigstore-go bundle JSON that
	// proves the artifact's signature. Required for v2.1+ when
	// signed_default is true and the registry is not in
	// allow_unsigned_for. Field is nullable for backwards
	// compatibility with v2.0 registry indexes; the install path
	// emits a structured error pointing at signing docs when the
	// bundle URL is missing AND --allow-unsigned is not set.
	SignatureBundle string `toml:"signature_bundle,omitempty"`
}

// Source is one configured registry. Mirrors config.Registry but lives
// here so internal/config doesn't need to know about index.toml shape.
type Source struct {
	Name string
	URL  string
}

// Client fetches and caches registry indexes.
type Client struct {
	cacheDir string
	ttl      time.Duration
	http     *http.Client
	// fakeFetch is set in tests to bypass HTTP and return canned bytes
	// for a URL. Production code always uses http.
	fakeFetch func(ctx context.Context, url string) ([]byte, error)
}

// NewClient returns a Client whose cache lives under cacheDir (callers
// usually pass ~/.samuel/cache/registries).
func NewClient(cacheDir string) *Client {
	return &Client{
		cacheDir: cacheDir,
		ttl:      DefaultTTL,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// WithTTL overrides the freshness window (used by tests).
func (c *Client) WithTTL(d time.Duration) *Client { c.ttl = d; return c }

// WithFakeFetch installs a stub fetcher (tests only).
func (c *Client) WithFakeFetch(fn func(ctx context.Context, url string) ([]byte, error)) *Client {
	c.fakeFetch = fn
	return c
}

// LoadIndex returns the parsed index for src. If a fresh cache exists
// it is preferred; otherwise the remote is fetched and the cache is
// repopulated atomically.
//
// forceRefresh ignores any cached copy.
func (c *Client) LoadIndex(ctx context.Context, src Source, forceRefresh bool) (*Index, error) {
	cachePath := c.cachePathFor(src.URL)
	if !forceRefresh {
		if idx, ok, _ := c.readFreshCache(cachePath); ok {
			return idx, nil
		}
	}
	body, err := c.fetch(ctx, c.resolveURL(src.URL))
	if err != nil {
		// Stale cache fallback when the network is down.
		if idx, ok, _ := c.readAnyCache(cachePath); ok {
			return idx, nil
		}
		return nil, err
	}
	idx, err := parseIndex(body)
	if err != nil {
		return nil, err
	}
	_ = c.writeCache(cachePath, body)
	return idx, nil
}

// Refresh forces a fresh fetch of every source, returning the indexes
// keyed by source name. Used by `samuel update` (no args).
func (c *Client) Refresh(ctx context.Context, sources []Source) (map[string]*Index, error) {
	out := map[string]*Index{}
	var errs []error
	for _, s := range sources {
		idx, err := c.LoadIndex(ctx, s, true)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name, err))
			continue
		}
		out[s.Name] = idx
	}
	if len(errs) > 0 && len(out) == 0 {
		return nil, fmt.Errorf("registry refresh failed for all sources: %v", errs)
	}
	return out, nil
}

// FindFirst walks sources in order and returns the first index that
// contains plugin name + the source it came from. Returns nil/empty +
// non-nil error when no source carries the plugin.
func (c *Client) FindFirst(ctx context.Context, sources []Source, name string) (*Index, Source, Plugin, error) {
	for _, s := range sources {
		idx, err := c.LoadIndex(ctx, s, false)
		if err != nil {
			continue
		}
		if p, ok := idx.Plugins[name]; ok {
			return idx, s, p, nil
		}
	}
	return nil, Source{}, Plugin{}, &NotFoundError{Name: name}
}

// ResolveVersion picks the best version match for the constraint. If
// versions is empty it falls back to "latest". If constraint is empty
// or "*" it returns "latest" too.
func ResolveVersion(p Plugin, constraint string, allowPrerelease bool) (string, error) {
	cand := p.Versions
	if len(cand) == 0 && p.Latest != "" {
		cand = []string{p.Latest}
	}
	if constraint == "" || constraint == "*" {
		if p.Latest != "" {
			return p.Latest, nil
		}
		if len(cand) > 0 {
			return cand[len(cand)-1], nil
		}
		return "", fmt.Errorf("registry: plugin has no versions")
	}
	r, err := semver.ParseRange(constraint)
	if err != nil {
		return "", err
	}
	parsed := make([]semver.Version, 0, len(cand))
	for _, c := range cand {
		v, perr := semver.Parse(c)
		if perr != nil {
			continue
		}
		parsed = append(parsed, v)
	}
	v, err := r.Resolve(parsed, semver.ResolveOptions{AllowPrerelease: allowPrerelease})
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

// Search returns matching plugin entries from idx, ranked by simple
// substring + tag relevance. Empty query returns every plugin.
func Search(idx *Index, query string) []SearchHit {
	q := strings.ToLower(strings.TrimSpace(query))
	hits := make([]SearchHit, 0, len(idx.Plugins))
	for name, p := range idx.Plugins {
		score := relevance(name, p, q)
		if q != "" && score == 0 {
			continue
		}
		hits = append(hits, SearchHit{Name: name, Plugin: p, Score: score})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Name < hits[j].Name
	})
	return hits
}

// SearchHit is one entry returned by Search.
type SearchHit struct {
	Name   string
	Plugin Plugin
	Score  int
}

func relevance(name string, p Plugin, q string) int {
	if q == "" {
		return 1
	}
	score := 0
	nameLower := strings.ToLower(name)
	if nameLower == q {
		score += 100
	}
	if strings.Contains(nameLower, q) {
		score += 30
	}
	if strings.Contains(strings.ToLower(p.Description), q) {
		score += 10
	}
	for _, t := range p.Tags {
		if strings.ToLower(t) == q {
			score += 20
		}
		if strings.Contains(strings.ToLower(t), q) {
			score += 5
		}
	}
	for _, c := range p.Categories {
		if strings.ToLower(c) == q {
			score += 10
		}
	}
	return score
}

func (c *Client) resolveURL(src string) string {
	src = strings.TrimSpace(src)
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") || strings.HasPrefix(src, "file://") {
		return src
	}
	// Bare github.com/... shorthand → raw.githubusercontent.com
	if strings.HasPrefix(src, "github.com/") {
		parts := strings.Split(strings.TrimPrefix(src, "github.com/"), "/")
		if len(parts) >= 2 {
			return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/%s", parts[0], parts[1], IndexFileName)
		}
	}
	return src
}

func (c *Client) cachePathFor(src string) string {
	if c.cacheDir == "" {
		return ""
	}
	u, err := url.Parse(c.resolveURL(src))
	if err != nil {
		return filepath.Join(c.cacheDir, sanitize(src), IndexFileName)
	}
	host := u.Host
	if host == "" {
		host = "local"
	}
	p := strings.TrimPrefix(u.Path, "/")
	p = strings.TrimSuffix(p, IndexFileName)
	p = strings.TrimSuffix(p, "/")
	return filepath.Join(c.cacheDir, host, sanitize(p), IndexFileName)
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "?", "_")
	return s
}

func (c *Client) readFreshCache(path string) (*Index, bool, error) {
	if path == "" {
		return nil, false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if time.Since(info.ModTime()) > c.ttl {
		return nil, false, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	idx, err := parseIndex(body)
	if err != nil {
		return nil, false, err
	}
	return idx, true, nil
}

func (c *Client) readAnyCache(path string) (*Index, bool, error) {
	if path == "" {
		return nil, false, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	idx, err := parseIndex(body)
	if err != nil {
		return nil, false, err
	}
	return idx, true, nil
}

func (c *Client) writeCache(path string, body []byte) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Client) fetch(ctx context.Context, fullURL string) ([]byte, error) {
	if c.fakeFetch != nil {
		return c.fakeFetch(ctx, fullURL)
	}
	if strings.HasPrefix(fullURL, "file://") {
		return os.ReadFile(strings.TrimPrefix(fullURL, "file://"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, (&errors.Error{
			Component:   Component,
			Problem:     "cannot fetch registry index",
			Path:        fullURL,
			Recoverable: true,
		}).Wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, &errors.Error{
			Component:   Component,
			Problem:     fmt.Sprintf("registry index returned HTTP %d", resp.StatusCode),
			Path:        fullURL,
			Recoverable: true,
		}
	}
	return io.ReadAll(resp.Body)
}

// rawIndex is an intermediate decode target that accepts both TOML
// shapes documented on the package. parseIndex normalizes it into the
// public Index (always keyed by name).
type rawIndex struct {
	SchemaVersion int               `toml:"schema_version"`
	PluginMap     map[string]Plugin `toml:"plugin"`
	PluginList    []namedPlugin     `toml:"plugins"`
}

// namedPlugin mirrors Plugin with an explicit `name` field so [[plugins]]
// entries can carry their identity inline.
type namedPlugin struct {
	Name        string   `toml:"name"`
	Repo        string   `toml:"repo"`
	Subpath     string   `toml:"subpath,omitempty"`
	Latest      string   `toml:"latest"`
	Versions    []string `toml:"versions,omitempty"`
	Description string   `toml:"description,omitempty"`
	Categories  []string `toml:"categories,omitempty"`
	Tags        []string `toml:"tags,omitempty"`
	Kind        string   `toml:"kind,omitempty"`
	Upstream    bool     `toml:"upstream,omitempty"`
}

func (np namedPlugin) toPlugin() Plugin {
	return Plugin{
		Repo:        np.Repo,
		Subpath:     np.Subpath,
		Latest:      np.Latest,
		Versions:    np.Versions,
		Description: np.Description,
		Categories:  np.Categories,
		Tags:        np.Tags,
		Kind:        np.Kind,
		Upstream:    np.Upstream,
	}
}

func parseIndex(body []byte) (*Index, error) {
	var raw rawIndex
	if err := toml.Unmarshal(body, &raw); err != nil {
		return nil, (&errors.Error{
			Component:   Component,
			Problem:     "registry index.toml is not valid TOML",
			Recoverable: true,
		}).Wrap(err)
	}
	idx := &Index{
		SchemaVersion: raw.SchemaVersion,
		Plugins:       make(map[string]Plugin, len(raw.PluginMap)+len(raw.PluginList)),
	}
	for name, p := range raw.PluginMap {
		idx.Plugins[name] = p
	}
	for _, np := range raw.PluginList {
		if np.Name == "" {
			continue
		}
		idx.Plugins[np.Name] = np.toPlugin()
	}
	return idx, nil
}

// NotFoundError is returned by FindFirst when no source carries the
// requested plugin name.
type NotFoundError struct{ Name string }

func (e *NotFoundError) Error() string { return "registry: plugin not found: " + e.Name }
