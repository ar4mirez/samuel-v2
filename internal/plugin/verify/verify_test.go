package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStub_RejectsUnknownSourceWhenSigningRequired(t *testing.T) {
	v := StubVerifier{}
	_, err := v.VerifyBlob(context.Background(), "/nonexistent", Request{
		Policy: DefaultPolicy(),
		Source: "github.com/random-stranger/plugin",
	})
	if err == nil {
		t.Fatalf("stub should reject unknown source under default policy")
	}
}

func TestStub_AcceptsSamuelpkgSource(t *testing.T) {
	v := StubVerifier{}
	res, err := v.VerifyBlob(context.Background(), "/any", Request{
		Policy: DefaultPolicy(),
		Source: "github.com/samuelpkg/samuel-go-guide",
	})
	if err != nil {
		t.Fatalf("samuelpkg/* should be accepted: %v", err)
	}
	if !res.Verified {
		t.Errorf("expected verified result")
	}
}

func TestStub_AllowUnsignedBypasses(t *testing.T) {
	v := StubVerifier{}
	res, err := v.VerifyBlob(context.Background(), "/any", Request{
		Policy:        DefaultPolicy(),
		Source:        "github.com/random-stranger/plugin",
		AllowUnsigned: true,
	})
	if err != nil {
		t.Fatalf("AllowUnsigned should bypass: %v", err)
	}
	if !res.Verified {
		t.Errorf("expected verified=true")
	}
}

func TestStub_RegistryAllowlist(t *testing.T) {
	pol := DefaultPolicy()
	pol.AllowUnsignedFor = []string{"local"}
	v := StubVerifier{}
	res, err := v.VerifyBlob(context.Background(), "/any", Request{
		Policy:   pol,
		Source:   "github.com/random/plugin",
		Registry: "local",
	})
	if err != nil || !res.Verified {
		t.Fatalf("registry allowlist failed: ok=%v err=%v", res.Verified, err)
	}
}

func TestMatchesIdentity(t *testing.T) {
	pol := DefaultPolicy()
	cases := map[string]bool{
		"github.com/samuelpkg/samuel-go-guide":      true,
		"https://github.com/samuelpkg/samuel-anything": true,
		"github.com/anthropics/skills/mcp-builder":     true,
		"github.com/random/plugin":                     false,
	}
	for src, want := range cases {
		if got := MatchesIdentity(pol, src); got != want {
			t.Errorf("MatchesIdentity(%q) = %v, want %v", src, got, want)
		}
	}
}

func TestCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	stubInner := stubVerifier{onBlob: func(string, Request) (Result, error) {
		calls++
		return Result{Verified: true, Identity: "test"}, nil
	}}
	cache := NewCache(dir, "v2.0.0", stubInner)
	path := filepath.Join(dir, "art.tar")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := Request{Policy: DefaultPolicy(), AllowUnsigned: true}
	if _, err := cache.VerifyBlob(context.Background(), path, req); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.VerifyBlob(context.Background(), path, req); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("inner verifier should be called once, got %d", calls)
	}
}

func TestCache_InvalidatesOnVersionBump(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	stubInner := stubVerifier{onBlob: func(string, Request) (Result, error) {
		calls++
		return Result{Verified: true}, nil
	}}
	cacheV1 := NewCache(dir, "v2.0.0", stubInner)
	path := filepath.Join(dir, "art.tar")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := Request{Policy: DefaultPolicy(), AllowUnsigned: true}
	if _, err := cacheV1.VerifyBlob(context.Background(), path, req); err != nil {
		t.Fatal(err)
	}
	cacheV2 := NewCache(dir, "v2.0.1", stubInner)
	if _, err := cacheV2.VerifyBlob(context.Background(), path, req); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("version bump should invalidate cache, got %d calls", calls)
	}
}

type stubVerifier struct {
	onBlob func(path string, req Request) (Result, error)
}

func (s stubVerifier) VerifyBlob(_ context.Context, path string, req Request) (Result, error) {
	return s.onBlob(path, req)
}
func (s stubVerifier) VerifyImage(_ context.Context, _ string, req Request) (Result, error) {
	return s.onBlob("", req)
}
