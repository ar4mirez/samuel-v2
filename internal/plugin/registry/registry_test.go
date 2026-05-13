package registry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixtureIndex = `
schema_version = 1

[plugin.go-guide]
repo = "github.com/samuelpkg/samuel-go-guide"
latest = "1.4.2"
versions = ["1.0.0", "1.4.0", "1.4.2", "2.0.0-rc.1"]
description = "Go language guardrails and patterns"
categories = ["language"]
tags = ["go", "golang"]
kind = "skill"

[plugin.codex-translator]
repo = "github.com/samuelpkg/samuel-codex-translator"
latest = "0.2.0"
versions = ["0.1.0", "0.2.0"]
description = "Codex tool translator"
tags = ["translator"]
kind = "wasm"

[plugin.claude-runner]
repo = "github.com/samuelpkg/samuel-claude-runner"
latest = "1.0.0"
description = "Claude OCI runner"
tags = ["runner"]
kind = "oci"

[plugin.react-helper]
repo = "github.com/samuelpkg/samuel-react-helper"
latest = "0.1.0"
description = "React component scaffolding"
tags = ["react", "frontend"]
`

func writeFixtureFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "index.toml")
	if err := os.WriteFile(path, []byte(fixtureIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadIndex_FromFile(t *testing.T) {
	path := writeFixtureFile(t)
	c := NewClient(t.TempDir())
	idx, err := c.LoadIndex(context.Background(), Source{Name: "test", URL: "file://" + path}, true)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if _, ok := idx.Plugins["go-guide"]; !ok {
		t.Errorf("go-guide missing from index")
	}
}

func TestLoadIndex_CachesAndReturnsFresh(t *testing.T) {
	path := writeFixtureFile(t)
	c := NewClient(t.TempDir())
	src := Source{Name: "test", URL: "file://" + path}
	if _, err := c.LoadIndex(context.Background(), src, true); err != nil {
		t.Fatal(err)
	}
	// Force fakeFetch to fail on next call — cached index should serve.
	c.WithFakeFetch(func(context.Context, string) ([]byte, error) {
		return nil, os.ErrNotExist
	})
	idx, err := c.LoadIndex(context.Background(), src, false)
	if err != nil {
		t.Fatalf("expected cached read, got %v", err)
	}
	if _, ok := idx.Plugins["go-guide"]; !ok {
		t.Errorf("expected cache hit to surface go-guide")
	}
}

func TestLoadIndex_ExpiredCacheFallsBackToStaleOnFetchFail(t *testing.T) {
	path := writeFixtureFile(t)
	cacheDir := t.TempDir()
	c := NewClient(cacheDir).WithTTL(1 * time.Nanosecond)
	src := Source{Name: "test", URL: "file://" + path}
	if _, err := c.LoadIndex(context.Background(), src, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	c.WithFakeFetch(func(context.Context, string) ([]byte, error) {
		return nil, os.ErrPermission
	})
	idx, err := c.LoadIndex(context.Background(), src, false)
	if err != nil {
		t.Fatalf("stale cache fallback failed: %v", err)
	}
	if idx == nil {
		t.Fatalf("expected stale cache to load")
	}
}

func TestFindFirst_FirstMatchWins(t *testing.T) {
	path := writeFixtureFile(t)
	c := NewClient(t.TempDir())
	srcs := []Source{
		{Name: "official", URL: "file://" + path},
	}
	_, src, p, err := c.FindFirst(context.Background(), srcs, "codex-translator")
	if err != nil {
		t.Fatalf("FindFirst: %v", err)
	}
	if src.Name != "official" || p.Latest != "0.2.0" {
		t.Errorf("got src=%+v plugin=%+v", src, p)
	}
}

func TestFindFirst_NotFound(t *testing.T) {
	path := writeFixtureFile(t)
	c := NewClient(t.TempDir())
	_, _, _, err := c.FindFirst(context.Background(), []Source{{Name: "x", URL: "file://" + path}}, "nope")
	if err == nil {
		t.Fatalf("expected NotFoundError")
	}
}

func TestResolveVersion_RangeMatchesHighest(t *testing.T) {
	p := Plugin{
		Latest:   "1.4.2",
		Versions: []string{"1.0.0", "1.4.0", "1.4.2", "2.0.0-rc.1"},
	}
	v, err := ResolveVersion(p, "^1.0.0", false)
	if err != nil {
		t.Fatalf("ResolveVersion: %v", err)
	}
	if v != "1.4.2" {
		t.Errorf("got %s, want 1.4.2", v)
	}
}

func TestResolveVersion_StarReturnsLatest(t *testing.T) {
	p := Plugin{Latest: "1.4.2", Versions: []string{"1.0.0", "1.4.2"}}
	v, _ := ResolveVersion(p, "*", false)
	if v != "1.4.2" {
		t.Errorf("got %s, want 1.4.2", v)
	}
}

func TestSearch_RanksByRelevance(t *testing.T) {
	path := writeFixtureFile(t)
	c := NewClient(t.TempDir())
	idx, _ := c.LoadIndex(context.Background(), Source{Name: "test", URL: "file://" + path}, true)

	hits := Search(idx, "react")
	if len(hits) == 0 {
		t.Fatalf("expected react hit")
	}
	if hits[0].Name != "react-helper" {
		t.Errorf("react-helper should rank first, got %+v", hits)
	}
}

func TestResolveURL_GitHubShorthand(t *testing.T) {
	c := NewClient(t.TempDir())
	got := c.resolveURL("github.com/samuelpkg/samuel-registry")
	if !strings.Contains(got, "raw.githubusercontent.com/samuelpkg/samuel-registry/main/index.toml") {
		t.Errorf("resolveURL got %s", got)
	}
}
