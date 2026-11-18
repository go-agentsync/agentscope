// Package serve is the HTTP face of agentscope: it serves the wasm UI and the
// gRPC-over-WebSocket endpoint on one loopback origin.
//
// What a page served here can reach is the whole point of the design, so it is
// narrow and stated:
//
//   - the listener is bound to a loopback address and refuses any other;
//   - every request, the WebSocket upgrade included, must carry a Host header
//     naming the loopback address and port actually bound -- a DNS-rebinding
//     page reaches this server with its own name in Host, and is answered 421;
//   - the WebSocket handshake additionally checks Origin against Host (the
//     transport's same-origin default), so a page on another origin cannot
//     open the stream even if it knew the port;
//   - the only methods are GET and HEAD, the only data is the read-only
//     snapshot, and there is no endpoint that writes to the machine;
//   - responses carry a CSP that allows the page's own scripts and the wasm
//     it instantiates, nothing from elsewhere.
package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/go-agentsync/agentscope/wire"
	wstransport "github.com/grpc-transports/websocket"
	"google.golang.org/grpc"
)

// GRPCPath is where the WebSocket carrying gRPC is mounted.
const GRPCPath = "/grpc"

// Config configures a Server.
type Config struct {
	Addr  string // host:port; the host must be a loopback IP; port 0 picks one
	Web   fs.FS  // the UI: index.html, app.wasm, wasm_exec.js, ...
	Scope wire.Scope
}

// Server is a bound, not yet serving, agentscope server.
type Server struct {
	ln   net.Listener
	http *http.Server
	grpc *grpc.Server
	wsl  net.Listener
	// Hosts are the Host header values accepted.
	Hosts []string
}

// loopbackHost splits addr and requires its host to be a loopback IP literal.
func loopbackHost(addr string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		return "", "", fmt.Errorf("serve: %q is not a loopback address; agentscope binds 127.0.0.1 or ::1 only", host)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 0 || p > 65535 {
		return "", "", fmt.Errorf("serve: bad port %q", port)
	}
	return host, port, nil
}

// New binds the listener and builds the handlers.
func New(c Config) (*Server, error) {
	host, _, err := loopbackHost(c.Addr)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", c.Addr)
	if err != nil {
		return nil, err
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	s := &Server{ln: ln}
	s.Hosts = []string{net.JoinHostPort(host, port), net.JoinHostPort("localhost", port)}
	if host == "127.0.0.1" || host == "::1" {
		// Both loopback families answer to "localhost".
		s.Hosts = append(s.Hosts, net.JoinHostPort("127.0.0.1", port), net.JoinHostPort("::1", port))
	}

	wsh, wsl := wstransport.HandlerListener(wstransport.ServerConfig{Path: GRPCPath})
	s.wsl = wsl
	s.grpc = grpc.NewServer(wire.ServerOptions()...)
	wire.Register(s.grpc, c.Scope)

	mux := http.NewServeMux()
	mux.Handle(GRPCPath, wsh)
	mux.Handle("/", http.FileServerFS(c.Web))
	s.http = &http.Server{
		Handler:           s.guard(mux),
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	return s, nil
}

// URL is the address to open.
func (s *Server) URL() string { return "http://" + s.ln.Addr().String() + "/" }

// guard applies the Host check, the method check and the headers.
func (s *Server) guard(next http.Handler) http.Handler {
	ok := map[string]bool{}
	for _, h := range s.Hosts {
		ok[strings.ToLower(h)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ok[strings.ToLower(r.Host)] {
			http.Error(w, "misdirected request: this server answers to its loopback address only", http.StatusMisdirectedRequest)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "read-only", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// Serve runs until ctx ends, then shuts down.
func (s *Server) Serve(ctx context.Context) error {
	errc := make(chan error, 2)
	go func() { errc <- s.grpc.Serve(s.wsl) }()
	go func() { errc <- s.http.Serve(s.ln) }()
	select {
	case <-ctx.Done():
	case err := <-errc:
		s.shutdown()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
	s.shutdown()
	return nil
}

func (s *Server) shutdown() {
	c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.http.Shutdown(c)
	s.grpc.Stop()
	_ = s.wsl.Close()
}
