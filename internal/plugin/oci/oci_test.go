package oci

import (
	"context"
	"strings"
	"testing"

	"github.com/ar4mirez/samuel/internal/plugin"
	"github.com/ar4mirez/samuel/internal/plugin/capability"
	"github.com/ar4mirez/samuel/internal/plugin/manifest"
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
	eng := &fakeEngine{digest: "sha256:deadbeef"}
	m := manifest.Manifest{
		Name: "claude-runner", Version: "1.0.0", Kind: manifest.KindOci,
		OCI: &manifest.OCIBlock{Image: "ghcr.io/samuelpkg/samuel-runner-claude:1.0.0"},
	}
	p := New(m, t.TempDir(), eng, nil)
	res, err := p.Install(context.Background(), plugin.InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if p.Digest != "sha256:deadbeef" {
		t.Errorf("digest not pinned: %s", p.Digest)
	}
	if len(res.Mutations) != 1 || res.Mutations[0].Kind != plugin.MutationOciPulled {
		t.Errorf("mutation wrong: %+v", res.Mutations)
	}
}

func TestOCI_DetectChecksUninstall(t *testing.T) {
	eng := &fakeEngine{digest: "sha256:deadbeef", available: map[string]string{"ghcr.io/x/y:1.0.0": "sha256:deadbeef"}}
	m := manifest.Manifest{
		Name: "y", Version: "1.0.0", Kind: manifest.KindOci,
		OCI: &manifest.OCIBlock{Image: "ghcr.io/x/y:1.0.0"},
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
