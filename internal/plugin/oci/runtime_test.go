package oci

import (
	stderrors "errors"
	"os/exec"
	"strings"
	"testing"

	samerrors "github.com/samuelpkg/samuel/internal/errors"
)

// withLookPath restores the global lookPath stub at test end.
func withLookPath(t *testing.T, stub func(string) (string, error)) {
	t.Helper()
	prev := lookPath
	lookPath = stub
	t.Cleanup(func() { lookPath = prev })
}

func TestDetect_PodmanRootlessFirst(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		switch name {
		case "podman":
			return "/usr/local/bin/podman", nil
		default:
			return "", stderrors.New("not found")
		}
	})
	t.Setenv("SAMUEL_RUNTIME", "")
	rt, err := DetectRuntimeWith(func(_ string, _ ...string) ([]byte, error) {
		return []byte("4.9.3\n"), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.Kind != RuntimePodman || !rt.Rootless || rt.Reason != ReasonPodmanRootless {
		t.Fatalf("unexpected: %+v", rt)
	}
	if rt.Version != "4.9.3" {
		t.Errorf("version not parsed: %+v", rt)
	}
}

func TestDetect_DockerFallback(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		switch name {
		case "podman":
			return "", stderrors.New("not found")
		case "docker":
			return "/usr/bin/docker", nil
		default:
			return "", stderrors.New("not found")
		}
	})
	t.Setenv("SAMUEL_RUNTIME", "")
	rt, err := DetectRuntimeWith(func(_ string, _ ...string) ([]byte, error) {
		return []byte("24.0.7\n"), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.Kind != RuntimeDocker || rt.Reason != ReasonDocker {
		t.Fatalf("unexpected: %+v", rt)
	}
}

func TestDetect_ErrNoRuntime(t *testing.T) {
	withLookPath(t, func(string) (string, error) {
		return "", stderrors.New("not found")
	})
	t.Setenv("SAMUEL_RUNTIME", "")
	_, err := DetectRuntimeWith(func(_ string, _ ...string) ([]byte, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected ErrNoRuntime")
	}
	var se *samerrors.Error
	if !stderrors.As(err, &se) {
		t.Fatalf("expected structured error, got %T", err)
	}
	if !strings.Contains(se.Problem, "no container runtime") {
		t.Errorf("unexpected problem: %s", se.Problem)
	}
}

func TestDetect_OverridePodmanRoot(t *testing.T) {
	withLookPath(t, func(name string) (string, error) {
		if name == "podman" {
			return "/usr/local/bin/podman", nil
		}
		return "", stderrors.New("not found")
	})
	t.Setenv("SAMUEL_RUNTIME", "podman-root")
	rt, err := DetectRuntimeWith(func(_ string, _ ...string) ([]byte, error) {
		return []byte("podman version 5.0.0\n"), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.Kind != RuntimePodmanRoot || rt.Rootless || rt.Reason != ReasonEnvOverride {
		t.Fatalf("unexpected: %+v", rt)
	}
	if rt.Version != "5.0.0" {
		t.Errorf("version not parsed: %+v", rt)
	}
}

func TestDetect_OverrideUnknownValue(t *testing.T) {
	t.Setenv("SAMUEL_RUNTIME", "nerdctl")
	_, err := DetectRuntimeWith(func(_ string, _ ...string) ([]byte, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected override error")
	}
	var se *samerrors.Error
	if !stderrors.As(err, &se) {
		t.Fatalf("expected structured error, got %T", err)
	}
	if !strings.Contains(se.Problem, "SAMUEL_RUNTIME") {
		t.Errorf("unexpected problem: %s", se.Problem)
	}
}

func TestValidateDigestPinned(t *testing.T) {
	pinned := "ghcr.io/samuelpkg/foo@sha256:" + strings.Repeat("a", 64)
	if err := ValidateDigestPinned(pinned); err != nil {
		t.Fatalf("digest-pinned ref should be valid: %v", err)
	}
	if err := ValidateDigestPinned("ghcr.io/samuelpkg/foo:1.0.0"); err == nil {
		t.Fatal("tag-only ref should be rejected")
	}
}

func TestProbeVersion_RegexFallback(t *testing.T) {
	runner := func(_ string, _ ...string) ([]byte, error) {
		return []byte("Docker version 24.0.7, build afdd53b\n"), nil
	}
	v, ok := probeVersion(runner, "/usr/bin/docker", "docker")
	if !ok || v != "24.0.7" {
		t.Errorf("expected 24.0.7, got %q (ok=%v)", v, ok)
	}
}

// silence unused-import check when only some helpers are used.
var _ = exec.Command
