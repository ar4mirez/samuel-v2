package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelpkg/samuel/internal/plugin/manifest"
)

// PRD 0010 §Functional 9: `samuel new plugin --kind=oci --name=hello`
// must produce a buildable scaffold whose samuel-plugin.toml is
// digest-pinned (placeholder accepted) and parses cleanly.
func TestNew_OciScaffold_ParsesAndIsDigestPinned(t *testing.T) {
	defer pinTestEnv(t)()
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	ResetFlagsForTest()
	rootCmd.SetArgs([]string{"new", "plugin", "--kind=oci", "--name=hello"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	pluginDir := filepath.Join(dir, "hello")
	for _, f := range []string{"samuel-plugin.toml", "Containerfile", "Makefile", "README.md", ".github/workflows/release.yml"} {
		if _, err := os.Stat(filepath.Join(pluginDir, f)); err != nil {
			t.Errorf("missing scaffold file: %s", f)
		}
	}
	m, err := manifest.LoadFromDir(pluginDir)
	if err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
	if m.Kind != manifest.KindOci {
		t.Errorf("expected oci kind, got %s", m.Kind)
	}
	if !strings.Contains(m.OCI.Image, "@sha256:") {
		t.Errorf("scaffold manifest should be digest-pinned, got: %s", m.OCI.Image)
	}
}
