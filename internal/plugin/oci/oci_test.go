package oci

import (
	"context"
	"strings"
	"testing"

	"github.com/samuelpkg/samuel/internal/plugin"
	"github.com/samuelpkg/samuel/internal/plugin/capability"
	"github.com/samuelpkg/samuel/internal/plugin/manifest"
)

// fakeEngine implements Engine for tests.
type fakeEngine struct {
	pullErr   error
	digest    string
	available map[string]string
	removed   []string
}

func (f *fakeEngine) Pull(_ context.Context, image string) (string, error) {
	if f.pullErr != nil {
		return "", f.pullErr
	}
	if f.available == nil {
		f.available = map[string]string{}
	}
	f.available[image] = f.digest
	return f.digest, nil
}

func (f *fakeEngine) Inspect(_ context.Context, image string) (string, error) {
	if d, ok := f.available[image]; ok {
		return d, nil
	}
	return "", &notFound{image: image}
}

func (f *fakeEngine) Remove(_ context.Context, image string) error {
	delete(f.available, image)
	f.removed = append(f.removed, image)
	return nil
}

type notFound struct{ image string }

func (n *notFound) Error() string { return "no such image: " + n.image }

func TestParseImageName(t *testing.T) {
	cases := map[string]bool{
		"ghcr.io/samuelpkg/samuel-runner:1.0.0":     true,
		"docker.io/library/alpine:latest":          true,
		"ghcr.io/owner/repo@sha256:" + strings.Repeat("a", 64): true,
		"badspaces / x / y":                        false,
		"":                                         false,
		"oneparte:tag":                             false,
	}
	for in, want := range cases {
		_, err := ParseImageName(in)
		if (err == nil) != want {
			t.Errorf("ParseImageName(%q) ok=%v want=%v err=%v", in, err == nil, want, err)
		}
	}
}

func TestOCI_InstallPullsAndDigestPins(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	imageRef := "ghcr.io/samuelpkg/samuel-runner-claude@" + digest
	eng := &fakeEngine{digest: digest}
	m := manifest.Manifest{
		Name: "claude-runner", Version: "1.0.0", Kind: manifest.KindOci,
		OCI: &manifest.OCIBlock{Image: imageRef},
	}
	p := New(m, t.TempDir(), eng, nil)
	res, err := p.Install(context.Background(), plugin.InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if p.Digest != digest {
		t.Errorf("digest not pinned: %s", p.Digest)
	}
	if len(res.Mutations) != 1 || res.Mutations[0].Kind != plugin.MutationOciPulled {
		t.Errorf("mutation wrong: %+v", res.Mutations)
	}
}

// TestOCI_Install_RejectsTagOnlyRef enforces PRD 0010 §Functional 2 at
// install time — a manifest with a tag-only image must fail before any
// pull is attempted.
func TestOCI_Install_RejectsTagOnlyRef(t *testing.T) {
	eng := &fakeEngine{digest: "sha256:deadbeef"}
	m := manifest.Manifest{
		Name: "y", Version: "1.0.0", Kind: manifest.KindOci,
		OCI: &manifest.OCIBlock{Image: "ghcr.io/x/y:1.0.0"},
	}
	p := New(m, t.TempDir(), eng, nil)
	if _, err := p.Install(context.Background(), plugin.InstallOptions{}); err == nil {
		t.Fatal("expected digest-pinned rejection")
	}
}

func TestOCI_DetectChecksUninstall(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	imageRef := "ghcr.io/x/y@" + digest
	eng := &fakeEngine{digest: digest, available: map[string]string{imageRef: digest}}
	m := manifest.Manifest{
		Name: "y", Version: "1.0.0", Kind: manifest.KindOci,
		OCI: &manifest.OCIBlock{Image: imageRef},
	}
	p := New(m, t.TempDir(), eng, nil)
	det, _ := p.Detect(context.Background())
	if !det.Installed {
		t.Errorf("detect should report installed")
	}
	st := p.Check(context.Background())
	if !st.OK {
		t.Errorf("check should be ok: %+v", st)
	}
	if _, err := p.Uninstall(context.Background(), plugin.UninstallOptions{}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	det2, _ := p.Detect(context.Background())
	if det2.Installed {
		t.Errorf("detect should report uninstalled")
	}
}

func TestBuildRunArgs_ReadOnlyByDefault(t *testing.T) {
	args := BuildRunArgs(LaunchOptions{
		Image: "ghcr.io/x/y:1.0.0",
		Layout: MountLayout{
			Workspace:    "/home/u/project",
			Skills:       "/home/u/.samuel/builtins",
			SamuelRun:    "/home/u/project/.samuel/run",
			BridgeSocket: "/home/u/project/.samuel/run/y.sock",
		},
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "/home/u/project:/workspace:ro") {
		t.Errorf("workspace should mount ro: %v", args)
	}
	if !strings.Contains(joined, "--network none") {
		t.Errorf("network should default to none: %v", args)
	}
}

func TestBuildRunArgs_WriteCapability(t *testing.T) {
	args := BuildRunArgs(LaunchOptions{
		Image: "x",
		Layout: MountLayout{Workspace: "/p"},
		Grants: []capability.Grant{
			{Kind: capability.KindFilesystemWrite, Targets: []string{"/workspace/**"}},
		},
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "/p:/workspace ") {
		t.Errorf("workspace should mount rw: %v", args)
	}
}

func TestBuildRunArgs_NetworkPolicyOpenWhenAllowlistPresent(t *testing.T) {
	args := BuildRunArgs(LaunchOptions{
		Image:  "x",
		Layout: MountLayout{},
		Grants: []capability.Grant{
			{Kind: capability.KindNetworkOutbound, Targets: []string{"api.openai.com"}},
		},
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--network bridge") {
		t.Errorf("network should be bridge: %v", args)
	}
}

func TestBuildRunArgs_EnvAllowlistFilters(t *testing.T) {
	args := BuildRunArgs(LaunchOptions{
		Image:        "x",
		Layout:       MountLayout{},
		HostEnv:      []string{"SECRET=hideme", "MY_API=ok"},
		EnvAllowlist: []string{"MY_API"},
	})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "SECRET") {
		t.Errorf("env-allowlist should strip SECRET: %v", args)
	}
	if !strings.Contains(joined, "MY_API=ok") {
		t.Errorf("MY_API should pass through: %v", args)
	}
}

// PRD 0010 §Functional 3.1 — manifest [capabilities.filesystem] entries
// become -v mounts. Read-only unless write = true; the same path in both
// lists resolves to read-write.
func TestBuildRunArgs_ExtraMountsReadOnlyDefault(t *testing.T) {
	args := BuildRunArgs(LaunchOptions{
		Image:  "x",
		Layout: MountLayout{},
		ExtraMounts: []CapabilityMount{
			{HostPath: "/host/data", ContainerPath: "/workspace/data", ReadOnly: true},
			{HostPath: "/host/out", ContainerPath: "/workspace/out", ReadOnly: false},
		},
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "/host/data:/workspace/data:ro") {
		t.Errorf("read-only mount missing: %v", args)
	}
	if !strings.Contains(joined, "/host/out:/workspace/out ") && !strings.HasSuffix(joined, "/host/out:/workspace/out") {
		t.Errorf("read-write mount missing: %v", args)
	}
	if strings.Contains(joined, "/host/out:/workspace/out:ro") {
		t.Errorf("write mount must not carry :ro: %v", args)
	}
}

// PRD 0010 §Functional 3.3 — cpu_quota → --cpus, memory_limit → --memory.
func TestBuildRunArgs_ResourceLimits(t *testing.T) {
	args := BuildRunArgs(LaunchOptions{
		Image:       "x",
		Layout:      MountLayout{},
		CPUQuota:    "1.5",
		MemoryLimit: "512m",
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--cpus 1.5") {
		t.Errorf("--cpus missing: %v", args)
	}
	if !strings.Contains(joined, "--memory 512m") {
		t.Errorf("--memory missing: %v", args)
	}
}

// PRD 0010 §Functional 3 — workdir + entrypoint override land verbatim.
func TestBuildRunArgs_WorkdirAndEntrypoint(t *testing.T) {
	args := BuildRunArgs(LaunchOptions{
		Image:      "x",
		Layout:     MountLayout{},
		Workdir:    "/workspace",
		Entrypoint: []string{"/opt/run", "--mode=oci"},
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-w /workspace") {
		t.Errorf("--workdir missing: %v", args)
	}
	if !strings.Contains(joined, "--entrypoint /opt/run") {
		t.Errorf("--entrypoint missing: %v", args)
	}
	if !strings.Contains(joined, "x --mode=oci") {
		t.Errorf("entrypoint extra args should follow image: %v", args)
	}
}

// CapabilityMountsFromManifest derives -v entries from
// [capabilities.filesystem]. Relative paths anchor at /workspace; the
// same path declared write-only stays writable even if also declared
// read.
func TestCapabilityMountsFromManifest_AnchorsToWorkspace(t *testing.T) {
	m := &manifest.Manifest{
		Capabilities: manifest.CapabilitiesBlock{
			Filesystem: manifest.FilesystemCaps{
				Read:  []string{"docs", "/etc/ssl/certs"},
				Write: []string{"/workspace/docs"},
			},
		},
	}
	mounts := CapabilityMountsFromManifest(m, "/repo")
	if len(mounts) != 2 {
		t.Fatalf("want 2 mounts (docs deduped + /etc/ssl/certs), got %d: %+v", len(mounts), mounts)
	}
	for _, mm := range mounts {
		switch mm.ContainerPath {
		case "/workspace/docs":
			if mm.ReadOnly {
				t.Errorf("docs should be writable when both read+write declared: %+v", mm)
			}
			if mm.HostPath != "/repo/docs" {
				t.Errorf("docs host path should be projectDir-relative: %+v", mm)
			}
		case "/etc/ssl/certs":
			if !mm.ReadOnly {
				t.Errorf("absolute non-workspace path should be read-only: %+v", mm)
			}
			if mm.HostPath != "/etc/ssl/certs" {
				t.Errorf("absolute path should pass through verbatim: %+v", mm)
			}
		}
	}
}
