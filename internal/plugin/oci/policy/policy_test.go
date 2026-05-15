package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModeFromEnv(t *testing.T) {
	cases := map[string]Mode{
		"":            ModeInteractive,
		"deny-all":    ModeDenyAll,
		"allow-once":  ModeAllowOnce,
		"allow-all":   ModeAllowAll,
		"interactive": ModeInteractive,
	}
	for in, want := range cases {
		got, err := ModeFromEnv(in)
		if err != nil {
			t.Errorf("ModeFromEnv(%q) err: %v", in, err)
		}
		if got != want {
			t.Errorf("ModeFromEnv(%q) = %v want %v", in, got, want)
		}
	}
	if _, err := ModeFromEnv("yolo"); err == nil {
		t.Error("expected error for unrecognized mode")
	}
}

func TestStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if err := s.Record("foo", "api.example.com", DecisionAllowAlways); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := s.Record("foo", "deny.example.com", DecisionDenyForever); err != nil {
		t.Fatalf("Record deny: %v", err)
	}
	// Non-persistent decisions should not create new entries.
	if err := s.Record("foo", "transient.example.com", DecisionAllowOnce); err != nil {
		t.Fatalf("Record once: %v", err)
	}

	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	entries := s2.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (persistent only), got %d: %+v", len(entries), entries)
	}
	if _, ok := s2.Lookup("foo", "api.example.com"); !ok {
		t.Error("api.example.com lookup miss")
	}
	if _, ok := s2.Lookup("foo", "transient.example.com"); ok {
		t.Error("transient.example.com should not have been persisted")
	}
}

func TestStore_Reset(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	_ = s.Record("a", "x.example.com", DecisionAllowAlways)
	_ = s.Record("b", "y.example.com", DecisionAllowAlways)
	if err := s.ResetPlugin("a"); err != nil {
		t.Fatalf("ResetPlugin: %v", err)
	}
	if _, ok := s.Lookup("a", "x.example.com"); ok {
		t.Error("plugin a should be cleared")
	}
	if _, ok := s.Lookup("b", "y.example.com"); !ok {
		t.Error("plugin b should be intact")
	}
	if err := s.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if len(s.List()) != 0 {
		t.Error("Reset did not clear store")
	}
}

func TestEngine_ManifestAllowlistAutoAllows(t *testing.T) {
	s := NewStore(t.TempDir())
	e := &Engine{
		Plugin:       "foo",
		AllowedHosts: []string{"api.example.com", "*.cdn.example.com"},
		Store:        s,
		Mode:         ModeInteractive,
		Prompt:       AlwaysDeny,
	}
	if d := e.Decide("api.example.com"); !d.Allowed() {
		t.Errorf("manifest allowlist should allow api.example.com, got %v", d)
	}
	if d := e.Decide("static.cdn.example.com"); !d.Allowed() {
		t.Errorf("wildcard should match subdomain, got %v", d)
	}
	if d := e.Decide("evil.example.com"); d.Allowed() {
		t.Errorf("non-allowlisted host should be denied by stub prompt, got %v", d)
	}
}

func TestEngine_ModeDenyAll(t *testing.T) {
	s := NewStore(t.TempDir())
	e := &Engine{
		Plugin: "foo",
		Store:  s,
		Mode:   ModeDenyAll,
	}
	if d := e.Decide("api.example.com"); d.Allowed() {
		t.Errorf("deny-all mode should block: %v", d)
	}
}

func TestEngine_ModeAllowOnce(t *testing.T) {
	s := NewStore(t.TempDir())
	e := &Engine{
		Plugin: "foo",
		Store:  s,
		Mode:   ModeAllowOnce,
	}
	if d := e.Decide("api.example.com"); !d.Allowed() {
		t.Errorf("allow-once mode should permit: %v", d)
	}
	// Non-persistent — fresh call still gets the same temporary allow.
	if len(s.List()) != 0 {
		t.Errorf("allow-once should not persist: %+v", s.List())
	}
}

func TestEngine_PersistedDecisionWins(t *testing.T) {
	s := NewStore(t.TempDir())
	_ = s.Record("foo", "evil.example.com", DecisionDenyForever)
	e := &Engine{
		Plugin: "foo",
		Store:  s,
		Mode:   ModeAllowAll,
		Prompt: AlwaysAllowAlways,
	}
	if d := e.Decide("evil.example.com"); d.Allowed() {
		t.Errorf("persisted deny-forever should beat allow-all: %v", d)
	}
}

func TestEngine_InteractivePromptPersistsAlways(t *testing.T) {
	s := NewStore(t.TempDir())
	called := 0
	prompt := func(plugin, host string) Decision {
		called++
		return DecisionAllowAlways
	}
	e := &Engine{
		Plugin: "foo",
		Store:  s,
		Mode:   ModeInteractive,
		Prompt: prompt,
	}
	e.Decide("api.example.com")
	if called != 1 {
		t.Errorf("expected prompt to fire once, got %d", called)
	}
	// Second call hits persistence, not the prompt.
	e.Decide("api.example.com")
	if called != 1 {
		t.Errorf("expected prompt to be skipped on second call, got %d", called)
	}
}

func TestPreauth(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Preauth("foo", "api.example.com", true); err != nil {
		t.Fatalf("Preauth: %v", err)
	}
	if _, ok := s.Lookup("foo", "api.example.com"); !ok {
		t.Fatal("preauth allow not persisted")
	}
	if err := s.Preauth("foo", "evil.example.com", false); err != nil {
		t.Fatalf("Preauth deny: %v", err)
	}
	entries := s.List()
	if len(entries) != 2 {
		t.Errorf("want 2 entries, got %d", len(entries))
	}
}

func TestAuditLogIsWritten(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Audit("foo", "api.example.com", DecisionAllowOnce, "manifest-allowlist"); err != nil {
		t.Fatalf("Audit: %v", err)
	}
	body := readFile(t, filepath.Join(dir, "audit.log"))
	if !strings.Contains(body, "api.example.com") || !strings.Contains(body, "allow-once") {
		t.Errorf("audit log missing fields:\n%s", body)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(body)
}
