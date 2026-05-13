package bridge

import (
	"context"
	"testing"
	"time"
)

// TestBridge_EndToEnd simulates an OCI plugin "container" connecting to
// the bridge socket, registering itself via Detect, and the framework
// invoking Install and Hook in turn.
func TestBridge_EndToEnd(t *testing.T) {
	socket := shortSocket(t)
	srv := NewServer(socket)

	srv.HandleDetect(func(_ context.Context, _ *DetectRequest) (*DetectResponse, error) {
		return &DetectResponse{Installed: false}, nil
	})
	srv.HandleInstall(func(_ context.Context, req *InstallRequest) (*InstallResponse, error) {
		return &InstallResponse{
			Component: "claude-runner",
			Mutations: []Mutation{{Kind: "oci_pulled", Path: "ghcr.io/x/y:1.0"}},
		}, nil
	})
	srv.HandleHook("post-install", func(_ context.Context, _ *HookRequest) (*HookResponse, error) {
		return &HookResponse{OK: true, Message: "post-install complete"}, nil
	})
	if _, err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Stop() })
	time.Sleep(20 * time.Millisecond)

	c := NewClient(socket)
	var det DetectResponse
	if err := c.Call(context.Background(), MethodDetect, &DetectRequest{}, &det); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if det.Installed {
		t.Errorf("Detect should report not installed initially")
	}
	var inst InstallResponse
	if err := c.Call(context.Background(), MethodInstall, &InstallRequest{}, &inst); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if inst.Component != "claude-runner" || len(inst.Mutations) == 0 {
		t.Errorf("Install: %+v", inst)
	}
	var hook HookResponse
	if err := c.Call(context.Background(), MethodHook, &HookRequest{HookName: "post-install"}, &hook); err != nil {
		t.Fatalf("Hook: %v", err)
	}
	if !hook.OK {
		t.Errorf("Hook: %+v", hook)
	}
}
