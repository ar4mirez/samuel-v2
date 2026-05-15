package proxy

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samuelpkg/samuel/internal/plugin/oci/policy"
)

// startProxy boots a proxy on a per-test Unix socket and returns the
// socket path + cleanup hook. macOS caps Unix-socket paths at 104 bytes
// so we mint a short tempdir under /tmp instead of using t.TempDir()
// (whose darwin path is ~/Library/Caches/... and exceeds the limit).
func startProxy(t *testing.T, eng *policy.Engine) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sp")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "p.sock")
	p := New(eng, sock)
	if _, err := p.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	go func() {
		_ = p.Serve()
	}()
	t.Cleanup(func() { _ = p.Stop() })
	return sock
}

// dialProxy returns a connection to the proxy socket.
func dialProxy(t *testing.T, sock string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	return conn
}

func TestProxy_BlocksUnallowedHost(t *testing.T) {
	eng := &policy.Engine{
		Plugin:       "test",
		AllowedHosts: []string{"api.example.com"},
		Store:        policy.NewStore(t.TempDir()),
		Mode:         policy.ModeDenyAll,
	}
	sock := startProxy(t, eng)
	conn := dialProxy(t, sock)
	defer conn.Close()
	// Issue a CONNECT for an unallowed host.
	if _, err := fmt.Fprint(conn, "CONNECT evil.example.com:443 HTTP/1.1\r\nHost: evil.example.com:443\r\n\r\n"); err != nil {
		t.Fatalf("write CONNECT: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != 403 {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

// TestProxy_AllowsManifestHost wires a real upstream so we can confirm
// the proxy forwards bytes when the allowlist matches. This test does
// not exercise TLS — it uses plain HTTP with a CONNECT-style tunnel to
// keep the harness simple. The proxy logic for both is symmetric.
func TestProxy_AllowsManifestHost(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	hostOnly := strings.SplitN(host, ":", 2)[0]
	eng := &policy.Engine{
		Plugin:       "test",
		AllowedHosts: []string{hostOnly},
		Store:        policy.NewStore(t.TempDir()),
	}
	sock := startProxy(t, eng)
	conn := dialProxy(t, sock)
	defer conn.Close()

	// Plain HTTP via the proxy: client sends an absolute-URL request.
	req := "GET " + upstream.URL + "/ HTTP/1.1\r\nHost: " + host + "\r\n\r\n"
	if _, err := fmt.Fprint(conn, req); err != nil {
		t.Fatalf("write GET: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestProxy_RawIPBlockedByAllowlist confirms PRD 0010 §Functional 5.9 +
// 5.10: a raw-IP destination is treated as a separate host and never
// matches a hostname-only allowlist. The proxy refuses without ever
// dialing upstream.
func TestProxy_RawIPBlockedByAllowlist(t *testing.T) {
	eng := &policy.Engine{
		Plugin:       "test",
		AllowedHosts: []string{"api.example.com"},
		Store:        policy.NewStore(t.TempDir()),
		Mode:         policy.ModeDenyAll,
	}
	sock := startProxy(t, eng)
	conn := dialProxy(t, sock)
	defer conn.Close()
	if _, err := fmt.Fprint(conn, "CONNECT 203.0.113.5:443 HTTP/1.1\r\nHost: 203.0.113.5:443\r\n\r\n"); err != nil {
		t.Fatalf("write CONNECT: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != 403 {
		t.Errorf("expected 403 for raw IP, got %d", resp.StatusCode)
	}
}
