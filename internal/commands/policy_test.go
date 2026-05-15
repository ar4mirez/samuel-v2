package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/samuelpkg/samuel/internal/plugin/oci/policy"
	"github.com/samuelpkg/samuel/internal/ui"
)

func setupPolicyStub(t *testing.T) *policy.Store {
	t.Helper()
	ResetFlagsForTest()
	t.Cleanup(ResetFlagsForTest)
	s := policy.NewStore(t.TempDir())
	prev := policyStore
	policyStore = func() (*policy.Store, error) { return s, nil }
	t.Cleanup(func() { policyStore = prev })
	return s
}

// captureStdout redirects ui.Print* output into a buffer for assertions.
func captureStdout(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	ui.SetWriters(buf, nil)
	t.Cleanup(func() {
		// Reset to defaults — the ui package owns the global, so we
		// just point it back at the real stdout/stderr.
		ui.SetWriters(os.Stdout, os.Stderr)
	})
	return buf
}

func TestPolicy_PreauthAllow(t *testing.T) {
	s := setupPolicyStub(t)
	rootCmd.SetArgs([]string{"policy", "preauth", "--plugin", "foo", "--host", "api.example.com", "--allow"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := s.Lookup("foo", "api.example.com"); !ok {
		t.Error("preauth allow not persisted")
	}
}

func TestPolicy_PreauthRequiresFlag(t *testing.T) {
	setupPolicyStub(t)
	rootCmd.SetArgs([]string{"policy", "preauth", "--plugin", "foo", "--host", "api.example.com"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error without --allow/--deny")
	}
}

func TestPolicy_List_Human(t *testing.T) {
	s := setupPolicyStub(t)
	_ = s.Record("foo", "api.example.com", policy.DecisionAllowAlways)
	out := captureStdout(t)
	rootCmd.SetArgs([]string{"policy", "list"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "foo") || !strings.Contains(out.String(), "api.example.com") {
		t.Errorf("unexpected output:\n%s", out.String())
	}
}

func TestPolicy_Reset_ScopedToPlugin(t *testing.T) {
	s := setupPolicyStub(t)
	_ = s.Record("a", "x.example.com", policy.DecisionAllowAlways)
	_ = s.Record("b", "y.example.com", policy.DecisionAllowAlways)
	rootCmd.SetArgs([]string{"policy", "reset", "--plugin", "a", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := s.Lookup("a", "x.example.com"); ok {
		t.Error("plugin a not cleared")
	}
	if _, ok := s.Lookup("b", "y.example.com"); !ok {
		t.Error("plugin b should remain")
	}
}
