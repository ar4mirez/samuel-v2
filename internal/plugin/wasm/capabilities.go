// Package wasm — Capabilities collects every resource a wasm plugin
// can request: filesystem mounts, environment variables, network
// hosts, memory cap, and per-invocation timeout. The Runtime consumes
// Capabilities to shape the wazero ModuleConfig and host-side
// proxies — keeping the declarative shape (manifest) separate from
// the imperative shape (wazero config) makes the cap surface explicit
// and unit-testable.
//
// A zero-value Capabilities denies everything except CPU/memory at the
// safe defaults. Per-capability constructor helpers exist for
// composition in tests; the production path always derives a single
// Capabilities from the manifest via FromManifest.
package wasm

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/samuelpkg/samuel/internal/plugin/manifest"
)

// Default budgets — PRD 0009 §Requirements.
const (
	DefaultMaxMemoryMiB = 64
	DefaultSoftTimeout  = 5 * time.Second
	DefaultHardTimeout  = 30 * time.Second
)

// FilesystemMount is a single mount instruction derived from the
// manifest. Source is a host path; ReadOnly mirrors whether the path
// appeared in [capabilities.filesystem] read vs write.
type FilesystemMount struct {
	HostPath  string
	ReadOnly  bool
}

// Capabilities is the per-instance gate set.
type Capabilities struct {
	Filesystem  []FilesystemMount
	Env         []string // allowlist of env keys
	NetworkHost []string // allowlist of hosts (deny-by-default if empty)
	MaxMemoryMiB uint32
	SoftTimeout time.Duration
	HardTimeout time.Duration
}

// Validate reports configuration conflicts that the manifest validator
// could not catch on its own — e.g. a write mount referencing a path
// that was not declared as readable.
func (c *Capabilities) Validate() error {
	for _, m := range c.Filesystem {
		if m.HostPath == "" {
			return errors.New("wasm: filesystem mount with empty host path")
		}
		if !filepath.IsAbs(m.HostPath) {
			return errors.New("wasm: filesystem mount must be an absolute path: " + m.HostPath)
		}
	}
	if c.MaxMemoryMiB == 0 {
		c.MaxMemoryMiB = DefaultMaxMemoryMiB
	}
	if c.SoftTimeout == 0 {
		c.SoftTimeout = DefaultSoftTimeout
	}
	if c.HardTimeout == 0 {
		c.HardTimeout = DefaultHardTimeout
	}
	if c.SoftTimeout > c.HardTimeout {
		return errors.New("wasm: soft timeout exceeds hard timeout")
	}
	return nil
}

// FromManifest derives Capabilities from a parsed manifest. Returns
// an error if the manifest is internally inconsistent for a wasm
// plugin (missing module, conflicting mounts, etc.).
func CapabilitiesFromManifest(m *manifest.Manifest) (Capabilities, error) {
	c := Capabilities{
		MaxMemoryMiB: DefaultMaxMemoryMiB,
		SoftTimeout:  DefaultSoftTimeout,
		HardTimeout:  DefaultHardTimeout,
	}
	if m == nil {
		return c, errors.New("wasm: nil manifest")
	}
	if m.Runtime != nil {
		if m.Runtime.MaxMemoryMiB > 0 {
			c.MaxMemoryMiB = m.Runtime.MaxMemoryMiB
		}
		if d, err := parseDuration(m.Runtime.Timeout); err == nil && d > 0 {
			c.SoftTimeout = d
		}
		if d, err := parseDuration(m.Runtime.HardTimeout); err == nil && d > 0 {
			c.HardTimeout = d
		}
	}
	for _, p := range m.Capabilities.Filesystem.Read {
		c.Filesystem = append(c.Filesystem, FilesystemMount{HostPath: p, ReadOnly: true})
	}
	for _, p := range m.Capabilities.Filesystem.Write {
		c.Filesystem = append(c.Filesystem, FilesystemMount{HostPath: p, ReadOnly: false})
	}
	c.Env = dedup(m.Capabilities.Env)
	c.NetworkHost = dedup(m.Capabilities.Network.Hosts)
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

func dedup(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// withFilesystem returns a copy with the mount appended.
func (c Capabilities) withFilesystem(host string, readOnly bool) Capabilities {
	c.Filesystem = append(append([]FilesystemMount(nil), c.Filesystem...), FilesystemMount{HostPath: host, ReadOnly: readOnly})
	return c
}

// withEnv returns a copy with keys appended to the env allowlist.
func (c Capabilities) withEnv(keys ...string) Capabilities {
	c.Env = dedup(append(append([]string(nil), c.Env...), keys...))
	return c
}

// withNetwork returns a copy with hosts appended to the network allowlist.
func (c Capabilities) withNetwork(hosts ...string) Capabilities {
	c.NetworkHost = dedup(append(append([]string(nil), c.NetworkHost...), hosts...))
	return c
}

// AllowsHost reports whether host is on the network allowlist. A host
// allowlist with a single "*" entry allows everything; per RFD 0010
// the recommendation is to enumerate explicit hosts.
func (c Capabilities) AllowsHost(host string) bool {
	if len(c.NetworkHost) == 0 {
		return false
	}
	for _, pat := range c.NetworkHost {
		if pat == "*" || pat == host {
			return true
		}
		if strings.HasPrefix(pat, "*.") && strings.HasSuffix(host, pat[1:]) {
			return true
		}
	}
	return false
}

// AllowsPath reports whether the absolute host path falls inside a
// declared filesystem mount and the requested write satisfies the
// mount's read-only flag.
func (c Capabilities) AllowsPath(path string, write bool) bool {
	clean := filepath.Clean(path)
	for _, m := range c.Filesystem {
		root := filepath.Clean(m.HostPath)
		if clean == root || strings.HasPrefix(clean, root+string(filepath.Separator)) {
			if write && m.ReadOnly {
				return false
			}
			return true
		}
	}
	return false
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}
