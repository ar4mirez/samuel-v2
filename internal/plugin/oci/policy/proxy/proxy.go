// Package proxy is the userspace HTTP/HTTPS proxy that enforces the
// OCI tier's network policy (PRD 0010 §Functional 5).
//
// The proxy binds a Unix-domain socket that the framework bind-mounts
// into the container at /samuel-proxy. The container's HTTP client
// libraries see HTTP_PROXY=unix:///samuel-proxy and route every
// outbound request through us. Each request is checked against
// policy.Engine.Decide(); allowed requests are forwarded to the real
// destination, denied requests get a 403 with a structured-error body.
//
// CONNECT requests (HTTPS) are inspected by host only — TLS bytes pass
// through untouched once the engine allows the tunnel. DNS lookups
// also go through the proxy: the Host header of the HTTP request (or
// the CONNECT target) is the host the engine sees, so raw-IP exfil
// is automatically blocked because IPs never match a hostname-based
// allowlist.
//
// The proxy is not a security boundary on its own; it works in concert
// with the launcher's --network=none flag. The container has no other
// egress path.
package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/samuelpkg/samuel/internal/plugin/oci/policy"
)

// Server is the proxy implementation. Zero value is unusable; build via
// New().
type Server struct {
	Engine     *policy.Engine
	SocketPath string

	// Dialer is the upstream-dial hook. Tests pin to a stub net.Dialer.
	Dialer net.Dialer

	listener net.Listener
	mu       sync.Mutex
	stopped  bool
	wg       sync.WaitGroup
}

// New builds a Server bound to socketPath.
func New(engine *policy.Engine, socketPath string) *Server {
	return &Server{
		Engine:     engine,
		SocketPath: socketPath,
		Dialer:     net.Dialer{Timeout: 10 * time.Second},
	}
}

// Listen binds the Unix socket and returns the resolved path. Useful
// for tests that need the socket before Serve runs.
func (s *Server) Listen() (string, error) {
	_ = removeIfExists(s.SocketPath)
	l, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return "", err
	}
	s.listener = l
	return s.SocketPath, nil
}

// Serve accepts connections until Stop is called. Each connection is
// handled in its own goroutine.
func (s *Server) Serve() error {
	if s.listener == nil {
		if _, err := s.Listen(); err != nil {
			return err
		}
	}
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			stopped := s.stopped
			s.mu.Unlock()
			if stopped {
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			s.handle(c)
		}(conn)
	}
}

// Stop closes the listener and waits for in-flight handlers.
func (s *Server) Stop() error {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.wg.Wait()
	_ = removeIfExists(s.SocketPath)
	return nil
}

func (s *Server) handle(client net.Conn) {
	defer client.Close()
	reader := bufio.NewReader(client)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	host := strings.SplitN(req.Host, ":", 2)[0]
	if req.Method == http.MethodConnect {
		s.handleConnect(client, req, host)
		return
	}
	s.handleHTTP(client, req, host)
}

func (s *Server) handleConnect(client net.Conn, req *http.Request, host string) {
	if s.Engine.IsBlocked(host) {
		writeError(client, req.ProtoMajor, req.ProtoMinor, 403, "blocked by samuel policy: "+host)
		return
	}
	target := req.URL.Host
	if target == "" {
		target = req.Host
	}
	upstream, err := s.Dialer.DialContext(context.Background(), "tcp", target)
	if err != nil {
		writeError(client, req.ProtoMajor, req.ProtoMinor, 502, "upstream dial failed: "+err.Error())
		return
	}
	defer upstream.Close()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	pipe(client, upstream)
}

func (s *Server) handleHTTP(client net.Conn, req *http.Request, host string) {
	if s.Engine.IsBlocked(host) {
		writeError(client, req.ProtoMajor, req.ProtoMinor, 403, "blocked by samuel policy: "+host)
		return
	}
	// Strip hop-by-hop headers before forwarding.
	for _, h := range []string{"Proxy-Connection", "Proxy-Authorization"} {
		req.Header.Del(h)
	}
	// Build a fresh request to the upstream URL. The client sent us
	// the absolute URL (per the HTTP proxy protocol); we forward as-is.
	upstream, err := s.Dialer.DialContext(context.Background(), "tcp", joinHostPort(req.URL.Host, req.URL.Scheme))
	if err != nil {
		writeError(client, req.ProtoMajor, req.ProtoMinor, 502, "upstream dial failed: "+err.Error())
		return
	}
	defer upstream.Close()
	if err := req.Write(upstream); err != nil {
		return
	}
	pipe(client, upstream)
}

func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(b, a)
		done <- struct{}{}
	}()
	<-done
}

// joinHostPort fills in the default port for the URL scheme when the
// host carries no explicit :port.
func joinHostPort(host, scheme string) string {
	if strings.Contains(host, ":") {
		return host
	}
	switch strings.ToLower(scheme) {
	case "https":
		return host + ":443"
	default:
		return host + ":80"
	}
}

// writeError serializes a structured-error JSON body and an appropriate
// HTTP status line back to the client.
func writeError(client net.Conn, major, minor, status int, msg string) {
	body, _ := json.Marshal(map[string]string{"error": msg})
	header := "HTTP/" + itoa(major) + "." + itoa(minor) + " " + itoa(status) + " " + http.StatusText(status) + "\r\n" +
		"Content-Type: application/json\r\n" +
		"Content-Length: " + itoa(len(body)) + "\r\n" +
		"Connection: close\r\n\r\n"
	_, _ = io.WriteString(client, header)
	_, _ = client.Write(body)
}

// itoa avoids strconv import for a single-purpose helper.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = '0' + byte(i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func removeIfExists(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return os.Remove(path)
	}
	return nil
}
