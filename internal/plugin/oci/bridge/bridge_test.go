package bridge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// shortSocket returns a Unix-socket path safe for the macOS 104-char
// limit. t.TempDir() lives under /var/folders/... which alone consumes
// most of the budget; we instead allocate under /tmp.
func shortSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sb-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, fmt.Sprintf("b%d.sock", time.Now().UnixNano()%1e6))
}

func TestServer_DispatchesDetect(t *testing.T) {
	socket := shortSocket(t)
	srv := NewServer(socket)
	srv.HandleDetect(func(_ context.Context, _ *DetectRequest) (*DetectResponse, error) {
		return &DetectResponse{Installed: true, Version: "1.0.0", Path: "/x"}, nil
	})
	if _, err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	t.Cleanup(func() { _ = srv.Stop() })

	time.Sleep(20 * time.Millisecond)
	c := NewClient(socket)
	var out DetectResponse
	if err := c.Call(context.Background(), MethodDetect, &DetectRequest{}, &out); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !out.Installed || out.Version != "1.0.0" {
		t.Errorf("unexpected response: %+v", out)
	}
}

func TestServer_HookByName(t *testing.T) {
	socket := shortSocket(t)
	srv := NewServer(socket)
	srv.HandleHook("pre-commit", func(_ context.Context, req *HookRequest) (*HookResponse, error) {
		if req.HookName != "pre-commit" {
			t.Errorf("hook name = %s", req.HookName)
		}
		return &HookResponse{OK: true, Message: "checked"}, nil
	})
	if _, err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Stop() })

	time.Sleep(20 * time.Millisecond)
	c := NewClient(socket)
	var out HookResponse
	if err := c.Call(context.Background(), MethodHook, &HookRequest{HookName: "pre-commit"}, &out); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !out.OK {
		t.Errorf("hook should return OK")
	}
}

func TestServer_UnimplementedMethod(t *testing.T) {
	socket := shortSocket(t)
	srv := NewServer(socket)
	if _, err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Stop() })
	time.Sleep(20 * time.Millisecond)
	c := NewClient(socket)
	err := c.Call(context.Background(), MethodCheck, &CheckRequest{}, nil)
	if err == nil {
		t.Errorf("expected unimplemented error")
	}
}
