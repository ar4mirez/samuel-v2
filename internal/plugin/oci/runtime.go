// Package oci implements the OCI-tier plugin loader: container images
// pulled by Podman (rootless), Podman (root), or Docker and launched on
// demand with a fixed mount layout and a Unix-socket bridge for the
// framework hooks (see internal/plugin/oci/bridge/).
//
// Detection order: SAMUEL_RUNTIME override → rootless Podman → root
// Podman → Docker → ErrNoRuntime. The detected runtime is cached
// per-process via DetectRuntime; callers can also pass DetectedRuntime
// around explicitly so tests can stub it.
package oci

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	samerrors "github.com/samuelpkg/samuel/internal/errors"
)

// Component is the structured-error namespace.
const Component = "plugin/oci"

// RuntimeKind enumerates the supported container engines.
type RuntimeKind string

const (
	RuntimePodman     RuntimeKind = "podman"
	RuntimePodmanRoot RuntimeKind = "podman-root"
	RuntimeDocker     RuntimeKind = "docker"
)

// Reasons recorded in DetectedRuntime.Reason.
const (
	ReasonEnvOverride    = "env-override"
	ReasonPodmanRootless = "podman-rootless"
	ReasonPodmanRoot     = "podman-root"
	ReasonDocker         = "docker"
)

// ErrNoRuntime is returned when no container runtime is detected.
// Wrapped with samerrors.Error for the CLI's structured rendering.
var ErrNoRuntime = errors.New("no container runtime found")

// DetectedRuntime carries the resolved engine + how it was chosen.
type DetectedRuntime struct {
	Kind RuntimeKind
	// Path is the absolute path to the CLI binary.
	Path string
	// Reason records how the engine was picked (see ReasonXxx).
	Reason string
	// Version is the engine's reported version (best-effort).
	Version string
	// Rootless reports whether the engine runs in rootless mode.
	Rootless bool
}

var (
	cacheOnce sync.Once
	cacheRT   DetectedRuntime
	cacheErr  error
)

// detectRunner is the shell-out hook tests stub. Production wires it to
// exec.Command. Returns combined stdout for parsing.
type detectRunner func(name string, args ...string) ([]byte, error)

var defaultRunner detectRunner = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// lookPath is the PATH-resolver hook tests stub.
var lookPath = exec.LookPath

// ResetRuntimeCacheForTest clears the per-process cache. Tests call this
// between invocations; production code never does.
func ResetRuntimeCacheForTest() {
	cacheOnce = sync.Once{}
	cacheRT = DetectedRuntime{}
	cacheErr = nil
}

// DetectRuntime resolves the container engine following the order in
// the PRD. Result is cached per-process; subsequent calls are O(1).
//
// SAMUEL_RUNTIME values:
//   - "podman"      → first available podman binary (probed as rootless)
//   - "podman-root" → podman, run as root
//   - "docker"      → docker
//
// Anything else is rejected as an explicit error so users don't get a
// silent fallback to a different engine.
func DetectRuntime() (DetectedRuntime, error) {
	cacheOnce.Do(func() {
		cacheRT, cacheErr = detectRuntimeOnce(defaultRunner)
	})
	return cacheRT, cacheErr
}

// DetectRuntimeWith is the testable form. It honors the same order but
// does not consult the per-process cache and takes the runner hook
// directly so tests can stub PATH + version output.
func DetectRuntimeWith(runner detectRunner) (DetectedRuntime, error) {
	return detectRuntimeOnce(runner)
}

func detectRuntimeOnce(runner detectRunner) (DetectedRuntime, error) {
	if override := strings.TrimSpace(os.Getenv("SAMUEL_RUNTIME")); override != "" {
		return resolveOverride(override, runner)
	}
	// Rootless Podman first.
	if path, err := lookPath("podman"); err == nil {
		rt := DetectedRuntime{
			Kind:     RuntimePodman,
			Path:     path,
			Reason:   ReasonPodmanRootless,
			Rootless: true,
		}
		if v, ok := probeVersion(runner, path, "podman"); ok {
			rt.Version = v
		}
		// If we can detect root-only podman explicitly via the env, we
		// degrade gracefully — but the default order assumes rootless.
		return rt, nil
	}
	if path, err := lookPath("docker"); err == nil {
		rt := DetectedRuntime{
			Kind:   RuntimeDocker,
			Path:   path,
			Reason: ReasonDocker,
		}
		if v, ok := probeVersion(runner, path, "docker"); ok {
			rt.Version = v
		}
		return rt, nil
	}
	return DetectedRuntime{}, &samerrors.Error{
		Component:   Component,
		Problem:     "no container runtime found",
		Fix:         "install podman or docker, or set SAMUEL_RUNTIME=<binary>",
		DocsURL:     "https://samuelpkg.github.io/samuel/docs/concepts/oci-runtime",
		Recoverable: true,
	}
}

func resolveOverride(override string, runner detectRunner) (DetectedRuntime, error) {
	switch strings.ToLower(override) {
	case "podman":
		path, err := lookPath("podman")
		if err != nil {
			return DetectedRuntime{}, overrideErr(override, "podman not on PATH")
		}
		rt := DetectedRuntime{Kind: RuntimePodman, Path: path, Reason: ReasonEnvOverride, Rootless: true}
		if v, ok := probeVersion(runner, path, "podman"); ok {
			rt.Version = v
		}
		return rt, nil
	case "podman-root":
		path, err := lookPath("podman")
		if err != nil {
			return DetectedRuntime{}, overrideErr(override, "podman not on PATH")
		}
		rt := DetectedRuntime{Kind: RuntimePodmanRoot, Path: path, Reason: ReasonEnvOverride}
		if v, ok := probeVersion(runner, path, "podman"); ok {
			rt.Version = v
		}
		return rt, nil
	case "docker":
		path, err := lookPath("docker")
		if err != nil {
			return DetectedRuntime{}, overrideErr(override, "docker not on PATH")
		}
		rt := DetectedRuntime{Kind: RuntimeDocker, Path: path, Reason: ReasonEnvOverride}
		if v, ok := probeVersion(runner, path, "docker"); ok {
			rt.Version = v
		}
		return rt, nil
	default:
		return DetectedRuntime{}, &samerrors.Error{
			Component:   Component,
			Problem:     "SAMUEL_RUNTIME has an unrecognized value",
			Cause:       override,
			Fix:         "set SAMUEL_RUNTIME to one of: podman, podman-root, docker",
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/concepts/oci-runtime",
			Recoverable: true,
		}
	}
}

func overrideErr(override, cause string) error {
	return &samerrors.Error{
		Component:   Component,
		Problem:     "SAMUEL_RUNTIME override could not be resolved",
		Cause:       fmt.Sprintf("%s: %s", override, cause),
		Fix:         "unset SAMUEL_RUNTIME or install the requested runtime",
		DocsURL:     "https://samuelpkg.github.io/samuel/docs/concepts/oci-runtime",
		Recoverable: true,
	}
}

// versionRE captures the X.Y.Z form from either
//   - "podman version 4.9.3"
//   - "Docker version 24.0.7, build afdd53b"
var versionRE = regexp.MustCompile(`(?i)version\s+v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`)

// probeVersion shells out to `<runtime> version` and parses the X.Y.Z.
// The empty-string + false return path is taken when the runtime is on
// PATH but its version output is unparseable — this is informational
// only (doctor surfaces it) so it never blocks detection.
func probeVersion(runner detectRunner, path, kind string) (string, bool) {
	if runner == nil {
		return "", false
	}
	out, err := runner(path, "version", "--format", versionFormatFor(kind))
	if err != nil || len(out) == 0 {
		// Fall back to plain `version`.
		out, err = runner(path, "version")
		if err != nil {
			return "", false
		}
	}
	if v := strings.TrimSpace(string(out)); v != "" && !strings.ContainsAny(v, "\n ") {
		return v, true
	}
	m := versionRE.FindStringSubmatch(string(out))
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

func versionFormatFor(kind string) string {
	switch kind {
	case "podman":
		return "{{.Version}}"
	case "docker":
		return "{{.Server.Version}}"
	}
	return ""
}

// imageNameRE matches OCI image references:
//
//	<registry>/<owner>/<name>[:<tag>][@<digest>]
//
// The registry-host portion accepts hostnames + optional port. The
// owner+name segments use the OCI-canonical lowercase alnum/_/-/. rule.
var imageNameRE = regexp.MustCompile(`^(?P<registry>(?:[A-Za-z0-9][A-Za-z0-9.-]*(?::[0-9]+)?))/(?P<owner>[a-z0-9]+(?:[._-][a-z0-9]+)*)/(?P<name>[a-z0-9]+(?:[._-][a-z0-9]+)*)(?::(?P<tag>[A-Za-z0-9_][A-Za-z0-9._-]{0,127}))?(?:@(?P<digest>sha256:[a-f0-9]{64}))?$`)

// ImageRef is the parsed shape of an OCI image reference.
type ImageRef struct {
	Registry string
	Owner    string
	Name     string
	Tag      string
	Digest   string
}

// ParseImageName validates ref and returns the structured form. Empty
// tag falls back to "latest" (matching docker pull semantics).
func ParseImageName(ref string) (ImageRef, error) {
	matches := imageNameRE.FindStringSubmatch(ref)
	if matches == nil {
		return ImageRef{}, &samerrors.Error{
			Component:   Component,
			Problem:     "invalid OCI image reference",
			Path:        ref,
			Fix:         "use registry/owner/name[:tag][@digest]",
			Recoverable: true,
		}
	}
	out := ImageRef{}
	for i, name := range imageNameRE.SubexpNames() {
		switch name {
		case "registry":
			out.Registry = matches[i]
		case "owner":
			out.Owner = matches[i]
		case "name":
			out.Name = matches[i]
		case "tag":
			out.Tag = matches[i]
		case "digest":
			out.Digest = matches[i]
		}
	}
	if out.Tag == "" {
		out.Tag = "latest"
	}
	return out, nil
}

// String renders the canonical "registry/owner/name:tag" form (digest
// is appended when present).
func (r ImageRef) String() string {
	s := fmt.Sprintf("%s/%s/%s:%s", r.Registry, r.Owner, r.Name, r.Tag)
	if r.Digest != "" {
		s += "@" + r.Digest
	}
	return s
}

// ValidateDigestPinned reports whether ref carries an explicit
// sha256:<digest> suffix. PRD 0010 §Functional 2 requires that every
// OCI manifest image reference be digest-pinned.
func ValidateDigestPinned(ref string) error {
	parsed, err := ParseImageName(ref)
	if err != nil {
		return err
	}
	if parsed.Digest == "" {
		return &samerrors.Error{
			Component:   Component,
			Problem:     "OCI image reference is not digest-pinned",
			Path:        ref,
			Fix:         "append @sha256:<64-hex-digest> to the image reference",
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/plugin-authors/oci",
			Recoverable: true,
		}
	}
	return nil
}

// Engine is the per-Runtime CLI invoker. Tests inject a FakeEngine.
type Engine interface {
	// Pull pulls the image and returns the content digest.
	Pull(ctx context.Context, image string) (string, error)
	// Inspect returns the digest if the image is locally available.
	Inspect(ctx context.Context, image string) (string, error)
	// Remove deletes a local image. Returns nil if absent.
	Remove(ctx context.Context, image string) error
}

// CLI wraps the resolved runtime binary.
type CLI struct {
	rt      DetectedRuntime
	timeout time.Duration

	// PullRetries controls the backoff loop in Pull(); default 3.
	PullRetries int
	// PullBackoff is the initial inter-attempt wait. Doubled on each
	// failure.
	PullBackoff time.Duration

	// commandFn is the shell-out hook tests stub. Production uses
	// exec.CommandContext.
	commandFn func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewCLI constructs an Engine backed by the user's container runtime.
func NewCLI(rt DetectedRuntime) *CLI {
	return &CLI{
		rt:          rt,
		timeout:     5 * time.Minute,
		PullRetries: 3,
		PullBackoff: 500 * time.Millisecond,
		commandFn:   exec.CommandContext,
	}
}

// WithTimeout overrides the default 5-minute timeout (tests pin a short
// timeout; production keeps the slow default for large pulls).
func (c *CLI) WithTimeout(d time.Duration) *CLI { c.timeout = d; return c }

// WithCommandFn injects a stubbed exec.CommandContext for tests.
func (c *CLI) WithCommandFn(fn func(ctx context.Context, name string, args ...string) *exec.Cmd) *CLI {
	c.commandFn = fn
	return c
}

// Pull invokes `<runtime> pull <image>` with retry-with-backoff (PRD
// 0010 §Non-functional). Returns the local content digest.
func (c *CLI) Pull(ctx context.Context, image string) (string, error) {
	var lastErr error
	retries := c.PullRetries
	if retries <= 0 {
		retries = 3
	}
	backoff := c.PullBackoff
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	for attempt := 1; attempt <= retries; attempt++ {
		ctx2, cancel := context.WithTimeout(ctx, c.timeout)
		cmd := c.commandFn(ctx2, c.rt.Path, "pull", image)
		out, err := cmd.CombinedOutput()
		cancel()
		if err == nil {
			return c.Inspect(ctx, image)
		}
		lastErr = (&samerrors.Error{
			Component:   Component,
			Problem:     "image pull failed",
			Cause:       fmt.Sprintf("%s: %s (attempt %d/%d)", err, strings.TrimSpace(string(out)), attempt, retries),
			Path:        image,
			DocsURL:     "https://samuelpkg.github.io/samuel/docs/concepts/oci-runtime",
			Recoverable: true,
		}).Wrap(err)
		if attempt < retries {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
	}
	return "", lastErr
}

// Inspect returns the local image's content digest (sha256:...).
func (c *CLI) Inspect(ctx context.Context, image string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	cmd := c.commandFn(ctx, c.rt.Path, "image", "inspect", image, "--format", "{{.Id}}")
	out, err := cmd.Output()
	if err != nil {
		return "", (&samerrors.Error{
			Component:   Component,
			Problem:     "image inspect failed",
			Cause:       err.Error(),
			Path:        image,
			Recoverable: true,
		}).Wrap(err)
	}
	digest := strings.TrimSpace(string(out))
	if digest == "" {
		return "", &samerrors.Error{
			Component:   Component,
			Problem:     "image inspect returned empty digest",
			Path:        image,
			Recoverable: true,
		}
	}
	return digest, nil
}

// Remove deletes a local image. A "not found" condition is mapped to nil.
func (c *CLI) Remove(ctx context.Context, image string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	cmd := c.commandFn(ctx, c.rt.Path, "image", "rm", image)
	if out, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "No such image") {
			return nil
		}
		return (&samerrors.Error{
			Component:   Component,
			Problem:     "image rm failed",
			Cause:       fmt.Sprintf("%s: %s", err, strings.TrimSpace(string(out))),
			Path:        image,
			Recoverable: true,
		}).Wrap(err)
	}
	return nil
}
