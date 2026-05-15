// Package policy implements the OCI tier's deny-by-default network
// policy (PRD 0010 §Functional 4 + 5):
//
//   - Per-plugin host allowlists derived from the manifest's
//     [capabilities.network] allowed_hosts list.
//   - Per-call consent prompts when a plugin tries to reach a host that
//     is not on its allowlist.
//   - Consent persistence keyed by (plugin, host) at
//     ~/.samuel/policy/network.toml.
//   - Audit log at ~/.samuel/policy/audit.log capturing every consent
//     decision + every block.
//   - SAMUEL_POLICY env hook that auto-resolves consent prompts in CI:
//     deny-all (default for CI), allow-once (per-process), or allow-all
//     (interactive-dev override).
//
// The package is transport-agnostic. A separate proxy package
// (internal/plugin/oci/policy/proxy) wires the engine to an HTTP/CONNECT
// listener that asks Engine.Decide() before brokering each connection.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"

	samerrors "github.com/samuelpkg/samuel/internal/errors"
)

// Component is the structured-error namespace.
const Component = "plugin/oci/policy"

// Decision enumerates the outcomes of a consent check.
type Decision string

const (
	// DecisionAllowOnce permits this single call. Not persisted.
	DecisionAllowOnce Decision = "allow-once"
	// DecisionAllowAlways permits this and every subsequent call for
	// the same (plugin, host) tuple. Persisted.
	DecisionAllowAlways Decision = "allow-always"
	// DecisionDeny rejects this single call. Not persisted.
	DecisionDeny Decision = "deny"
	// DecisionDenyForever rejects this and every subsequent call for
	// the same (plugin, host) tuple. Persisted.
	DecisionDenyForever Decision = "deny-forever"
)

// Allowed reports whether the decision authorizes the call.
func (d Decision) Allowed() bool {
	return d == DecisionAllowOnce || d == DecisionAllowAlways
}

// Persistent reports whether the decision should be written to the
// consent store.
func (d Decision) Persistent() bool {
	return d == DecisionAllowAlways || d == DecisionDenyForever
}

// Mode reflects the SAMUEL_POLICY env-driven auto-resolution behavior.
type Mode string

const (
	// ModeInteractive is the default: real consent prompts.
	ModeInteractive Mode = "interactive"
	// ModeDenyAll auto-denies every prompt (CI default).
	ModeDenyAll Mode = "deny-all"
	// ModeAllowOnce auto-allows every prompt for the current process.
	// Used for CI one-off runs where a single trusted human has
	// authorized the entire batch.
	ModeAllowOnce Mode = "allow-once"
	// ModeAllowAll auto-allows + persists every prompt. Interactive
	// dev only; doctor warns when set in CI environments.
	ModeAllowAll Mode = "allow-all"
)

// ModeFromEnv reads SAMUEL_POLICY and resolves to a Mode. Unknown values
// fall back to interactive with a structured warning so the user knows
// the env var was malformed.
func ModeFromEnv(env string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "", "interactive":
		return ModeInteractive, nil
	case "deny-all":
		return ModeDenyAll, nil
	case "allow-once":
		return ModeAllowOnce, nil
	case "allow-all":
		return ModeAllowAll, nil
	default:
		return ModeInteractive, &samerrors.Error{
			Component:   Component,
			Problem:     "SAMUEL_POLICY has an unrecognized value",
			Cause:       env,
			Fix:         "set SAMUEL_POLICY to one of: deny-all, allow-once, allow-all (default: interactive)",
			Recoverable: true,
		}
	}
}

// PromptFn is the abstract per-call consent prompt. The CLI wires a
// huh-based interactive form; tests inject a deterministic stub.
type PromptFn func(plugin, host string) Decision

// AlwaysDeny is a PromptFn used by ModeDenyAll.
var AlwaysDeny PromptFn = func(string, string) Decision { return DecisionDeny }

// AlwaysAllowOnce is a PromptFn used by ModeAllowOnce.
var AlwaysAllowOnce PromptFn = func(string, string) Decision { return DecisionAllowOnce }

// AlwaysAllowAlways is a PromptFn used by ModeAllowAll.
var AlwaysAllowAlways PromptFn = func(string, string) Decision { return DecisionAllowAlways }

// PromptForMode returns the deterministic PromptFn for a given Mode, or
// nil when the mode requires an interactive prompt.
func PromptForMode(m Mode) PromptFn {
	switch m {
	case ModeDenyAll:
		return AlwaysDeny
	case ModeAllowOnce:
		return AlwaysAllowOnce
	case ModeAllowAll:
		return AlwaysAllowAlways
	default:
		return nil
	}
}

// ConsentEntry is one persisted (plugin, host, decision, first-seen)
// tuple.
type ConsentEntry struct {
	Plugin    string `toml:"plugin"`
	Host      string `toml:"host"`
	Decision  string `toml:"decision"`
	FirstSeen string `toml:"first_seen"`
	LastSeen  string `toml:"last_seen,omitempty"`
}

// Store is the on-disk consent registry. Default location is
// ~/.samuel/policy/network.toml; tests pin Path to a tempdir.
type Store struct {
	Path     string
	AuditLog string

	mu      sync.Mutex
	entries map[string]ConsentEntry // key = plugin + "\x00" + host
}

// DefaultStore returns the user-scoped store at
// ~/.samuel/policy/network.toml.
func DefaultStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".samuel", "policy")
	return &Store{
		Path:     filepath.Join(dir, "network.toml"),
		AuditLog: filepath.Join(dir, "audit.log"),
		entries:  map[string]ConsentEntry{},
	}, nil
}

// NewStore returns a store rooted at dir. The dir is created on first
// write; reads on a missing file return an empty store.
func NewStore(dir string) *Store {
	return &Store{
		Path:     filepath.Join(dir, "network.toml"),
		AuditLog: filepath.Join(dir, "audit.log"),
		entries:  map[string]ConsentEntry{},
	}
}

// Load reads the on-disk consent store into memory. A missing file is
// not an error — it represents a clean slate.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]ConsentEntry{}
	body, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return (&samerrors.Error{
			Component: Component,
			Problem:   "cannot read consent store",
			Path:      s.Path,
			Cause:     err.Error(),
		}).Wrap(err)
	}
	var doc struct {
		Entries []ConsentEntry `toml:"entries"`
	}
	if err := toml.Unmarshal(body, &doc); err != nil {
		return (&samerrors.Error{
			Component: Component,
			Problem:   "consent store is not valid TOML",
			Path:      s.Path,
			Cause:     err.Error(),
		}).Wrap(err)
	}
	for _, e := range doc.Entries {
		s.entries[key(e.Plugin, e.Host)] = e
	}
	return nil
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	var doc struct {
		Entries []ConsentEntry `toml:"entries"`
	}
	for _, e := range s.entries {
		doc.Entries = append(doc.Entries, e)
	}
	body, err := toml.Marshal(doc)
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// Lookup returns the persisted decision for (plugin, host), or empty
// when no record exists.
func (s *Store) Lookup(plugin, host string) (ConsentEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key(plugin, host)]
	return e, ok
}

// Record persists d for (plugin, host) and stamps the timestamps. Only
// persistent decisions are stored; non-persistent decisions update
// last_seen on existing rows but never create new ones.
func (s *Store) Record(plugin, host string, d Decision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	k := key(plugin, host)
	existing, ok := s.entries[k]
	if !d.Persistent() {
		if ok {
			existing.LastSeen = now
			s.entries[k] = existing
			return s.save()
		}
		return nil
	}
	if !ok {
		existing = ConsentEntry{Plugin: plugin, Host: host, FirstSeen: now}
	}
	existing.Decision = string(d)
	existing.LastSeen = now
	s.entries[k] = existing
	return s.save()
}

// Reset clears every recorded consent. The audit log is preserved
// (history matters for forensics).
func (s *Store) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]ConsentEntry{}
	if _, err := os.Stat(s.Path); err == nil {
		if rmErr := os.Remove(s.Path); rmErr != nil && !os.IsNotExist(rmErr) {
			return rmErr
		}
	}
	return nil
}

// ResetPlugin clears every recorded consent for a single plugin.
func (s *Store) ResetPlugin(plugin string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.entries {
		if e.Plugin == plugin {
			delete(s.entries, k)
		}
	}
	return s.save()
}

// List returns every persisted entry sorted by (plugin, host).
func (s *Store) List() []ConsentEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ConsentEntry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e)
	}
	sortEntries(out)
	return out
}

// Preauth records an explicit decision without ever prompting. Used by
// `samuel policy preauth` for CI workflows that pre-allowlist hosts.
func (s *Store) Preauth(plugin, host string, allow bool) error {
	d := DecisionDenyForever
	if allow {
		d = DecisionAllowAlways
	}
	if err := s.Record(plugin, host, d); err != nil {
		return err
	}
	return s.Audit(plugin, host, d, "preauth")
}

// AuditEvent is one row in the audit log.
type AuditEvent struct {
	Time     string `json:"time"`
	Plugin   string `json:"plugin"`
	Host     string `json:"host"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// Audit appends one row to the audit log. The log is JSON Lines so it
// can be tailed and parsed by external tooling. Reasons describe how
// the decision was reached: "manifest-allowlist", "user-prompt",
// "samuel-policy-env", "preauth", "block-no-allowlist".
func (s *Store) Audit(plugin, host string, d Decision, reason string) error {
	if s.AuditLog == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.AuditLog), 0o700); err != nil {
		return err
	}
	ev := AuditEvent{
		Time:     time.Now().UTC().Format(time.RFC3339),
		Plugin:   plugin,
		Host:     host,
		Decision: string(d),
		Reason:   reason,
	}
	body, _ := json.Marshal(ev)
	body = append(body, '\n')
	f, err := os.OpenFile(s.AuditLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(body)
	return err
}

// Engine is the runtime decision-maker the proxy calls per request.
type Engine struct {
	Plugin string
	// AllowedHosts is the manifest-declared list of hosts that are
	// auto-allowed without prompting.
	AllowedHosts []string
	// Store backs persistent consent decisions.
	Store *Store
	// Mode is the SAMUEL_POLICY env-derived mode. When ModeInteractive
	// the engine calls Prompt.
	Mode Mode
	// Prompt is the human-or-stub consent prompt.
	Prompt PromptFn
}

// Decide returns the resolved decision for one outbound request. The
// resolution order:
//
//   1. Persisted consent record (allow-always / deny-forever).
//   2. Manifest allowlist (auto-allow).
//   3. SAMUEL_POLICY env mode (deny-all / allow-once / allow-all).
//   4. Interactive prompt (Engine.Prompt).
//
// Every resolution path writes one audit log entry so the user always
// has a record of what was blocked or allowed and why.
func (e *Engine) Decide(host string) Decision {
	if e == nil {
		return DecisionDeny
	}
	// Persisted decision wins outright.
	if e.Store != nil {
		if entry, ok := e.Store.Lookup(e.Plugin, host); ok {
			d := Decision(entry.Decision)
			_ = e.Store.Audit(e.Plugin, host, d, "persisted")
			return d
		}
	}
	if hostMatches(host, e.AllowedHosts) {
		if e.Store != nil {
			_ = e.Store.Audit(e.Plugin, host, DecisionAllowOnce, "manifest-allowlist")
		}
		return DecisionAllowOnce
	}
	// SAMUEL_POLICY override.
	if fn := PromptForMode(e.Mode); fn != nil {
		d := fn(e.Plugin, host)
		if e.Store != nil {
			_ = e.Store.Record(e.Plugin, host, d)
			_ = e.Store.Audit(e.Plugin, host, d, "samuel-policy-env")
		}
		return d
	}
	if e.Prompt == nil {
		if e.Store != nil {
			_ = e.Store.Audit(e.Plugin, host, DecisionDeny, "no-prompt-available")
		}
		return DecisionDeny
	}
	d := e.Prompt(e.Plugin, host)
	if e.Store != nil {
		_ = e.Store.Record(e.Plugin, host, d)
		_ = e.Store.Audit(e.Plugin, host, d, "user-prompt")
	}
	return d
}

// hostMatches reports whether host satisfies any allowlist entry.
// Patterns:
//
//   - "*"          → matches any host
//   - "host.tld"   → exact match
//   - "*.tld"      → subdomain or apex (TLD is "tld")
//   - "*.example.com" → any subdomain (api.example.com, x.y.example.com)
func hostMatches(host string, allow []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, pat := range allow {
		pat = strings.ToLower(strings.TrimSpace(pat))
		switch {
		case pat == "":
			continue
		case pat == "*":
			return true
		case pat == host:
			return true
		case strings.HasPrefix(pat, "*."):
			suf := pat[1:] // ".example.com"
			if host == suf[1:] {
				return true
			}
			if strings.HasSuffix(host, suf) {
				return true
			}
		}
	}
	return false
}

func key(plugin, host string) string {
	return plugin + "\x00" + host
}

// sortEntries sorts entries in-place by (plugin, host). The
// stdlib's sort.Slice keeps the package dependency-light vs pulling in
// internal/sort variants.
func sortEntries(in []ConsentEntry) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0; j-- {
			if less(in[j-1], in[j]) {
				break
			}
			in[j-1], in[j] = in[j], in[j-1]
		}
	}
}

func less(a, b ConsentEntry) bool {
	if a.Plugin != b.Plugin {
		return a.Plugin < b.Plugin
	}
	return a.Host < b.Host
}

// IsBlocked is a tiny convenience for callers (proxy, tests) that only
// care about the boolean outcome.
func (e *Engine) IsBlocked(host string) bool { return !e.Decide(host).Allowed() }

// DenyError is the structured-error type the proxy returns when a
// connection is blocked. The message follows the deny-by-default
// pattern: explicit about *why* + how to fix.
func DenyError(plugin, host string) error {
	return &samerrors.Error{
		Component:   Component,
		Problem:     "outbound network call blocked by samuel policy",
		Cause:       fmt.Sprintf("plugin=%s host=%s", plugin, host),
		Fix:         "add the host to [capabilities.network] allowed_hosts in samuel-plugin.toml, or run `samuel policy preauth --plugin " + plugin + " --host " + host + " --allow`",
		DocsURL:     "https://samuelpkg.github.io/samuel/docs/concepts/network-policy",
		Recoverable: true,
	}
}
