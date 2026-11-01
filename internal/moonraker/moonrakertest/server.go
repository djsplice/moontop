// Package moonrakertest provides an in-process fake Moonraker websocket
// server for tests. It speaks just enough JSON-RPC 2.0 to drive the
// moonraker.Client: canned per-method handlers, server-pushed
// notifications, a record of every call received, and a way to drop
// live connections to exercise reconnect.
package moonrakertest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Handler answers one RPC. Return a result (marshalled to JSON) or an
// error (sent as a JSON-RPC error object).
type Handler func(params json.RawMessage) (any, error)

// Call is one request the server received.
type Call struct {
	Method string
	Params json.RawMessage
}

// Server is a fake Moonraker. Zero-config it answers server.info as
// "ready", the printer.objects.* calls with empty but well-formed
// results, and every other method with "ok"; override with Handle.
type Server struct {
	t   testing.TB
	srv *httptest.Server

	mu       sync.Mutex
	handlers map[string]Handler
	calls    []Call
	conns    map[*websocket.Conn]*sync.Mutex
	connCh   chan struct{}
	http     map[string]http.HandlerFunc // plain HTTP, by path prefix

	auth *fakeAuth // nil: every client is trusted
}

// HandleHTTP serves plain HTTP requests whose path starts with prefix
// (file downloads and uploads, which Moonraker serves over HTTP).
func (s *Server) HandleHTTP(prefix string, h http.HandlerFunc) {
	s.mu.Lock()
	s.http[prefix] = h
	s.mu.Unlock()
}

// New starts a fake server and registers cleanup with t.
func New(t testing.TB) *Server {
	s := &Server{
		t:        t,
		handlers: map[string]Handler{},
		conns:    map[*websocket.Conn]*sync.Mutex{},
		connCh:   make(chan struct{}, 16),
		http:     map[string]http.HandlerFunc{},
	}
	s.Handle("server.info", Result(map[string]any{"klippy_connected": true, "klippy_state": "ready"}))
	empty := Result(map[string]any{"eventtime": 0.0, "status": map[string]any{}})
	s.Handle("printer.objects.subscribe", empty)
	s.Handle("printer.objects.query", empty)
	s.Handle("printer.objects.list", Result(map[string]any{"objects": []string{}}))
	s.srv = httptest.NewServer(http.HandlerFunc(s.serveWS))
	t.Cleanup(s.Close)
	return s
}

// Host returns host:port suitable for moonraker.New.
func (s *Server) Host() string {
	return strings.TrimPrefix(s.srv.URL, "http://")
}

// Handle sets the handler for method, replacing any previous one.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	s.handlers[method] = h
	s.mu.Unlock()
}

// Result is a convenience for a Handler that always returns v.
func Result(v any) Handler {
	return func(json.RawMessage) (any, error) { return v, nil }
}

// Calls returns a copy of every request received so far.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// CallsTo returns the received requests for one method.
func (s *Server) CallsTo(method string) []Call {
	var out []Call
	for _, c := range s.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// WaitForCall polls until method has been called at least n times.
func (s *Server) WaitForCall(method string, n int, timeout time.Duration) []Call {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		calls := s.CallsTo(method)
		if len(calls) >= n {
			return calls
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out waiting for %d call(s) to %s; got %d", n, method, len(calls))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// WaitForConnections blocks until n connections have been accepted in
// total since the server started.
func (s *Server) WaitForConnections(n int, timeout time.Duration) {
	s.t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-s.connCh:
		case <-time.After(timeout):
			s.t.Fatalf("timed out waiting for connection %d", i+1)
		}
	}
}

// Notify pushes a notification to every live connection.
func (s *Server) Notify(method string, params ...any) {
	s.t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	data, err := json.Marshal(msg)
	if err != nil {
		s.t.Fatalf("marshal notification: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for c, wmu := range s.conns {
		wmu.Lock()
		_ = c.WriteMessage(websocket.TextMessage, data)
		wmu.Unlock()
	}
}

// DropConnections closes every live connection abruptly, as a network
// blip or Moonraker restart would.
func (s *Server) DropConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		_ = c.Close()
		delete(s.conns, c)
	}
}

// Close stops the server.
func (s *Server) Close() {
	s.DropConnections()
	s.srv.Close()
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	if s.serveAuth(w, r) {
		return
	}
	if r.URL.Path != "/websocket" {
		s.mu.Lock()
		var h http.HandlerFunc
		best := ""
		for p, f := range s.http {
			if strings.HasPrefix(r.URL.Path, p) && len(p) > len(best) {
				best, h = p, f
			}
		}
		s.mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	wmu := &sync.Mutex{}
	s.mu.Lock()
	s.conns[conn] = wmu
	s.mu.Unlock()
	select {
	case s.connCh <- struct{}{}:
	default:
	}

	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			ID     int64           `json:"id"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			continue
		}
		s.mu.Lock()
		s.calls = append(s.calls, Call{Method: req.Method, Params: req.Params})
		h := s.handlers[req.Method]
		s.mu.Unlock()

		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if h == nil {
			resp["result"] = "ok"
		} else if result, err := h(req.Params); err != nil {
			resp["error"] = map[string]any{"code": 400, "message": err.Error()}
		} else {
			resp["result"] = result
		}
		data, err := json.Marshal(resp)
		if err != nil {
			panic(fmt.Sprintf("moonrakertest: marshal response: %v", err))
		}
		wmu.Lock()
		err = conn.WriteMessage(websocket.TextMessage, data)
		wmu.Unlock()
		if err != nil {
			return
		}
	}
}
