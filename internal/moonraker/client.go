// Package moonraker provides a WebSocket JSON-RPC 2.0 client for the
// Moonraker 3D-printer management API.
//
// It handles connection, request/response correlation, and exposes a
// single channel of server notifications (method + raw params). Typed
// parsing of individual notifications lives in notify.go; routing them
// is the consumer's job.
package moonraker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// JSON-RPC envelope types
// ---------------------------------------------------------------------------

// rpcRequest is the outgoing JSON-RPC 2.0 request envelope.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      int64  `json:"id"`
}

// rpcResponse is a JSON-RPC 2.0 response (matched to a request by ID).
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError represents a JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// rpcNotification is a server-pushed JSON-RPC notification (no id field).
type rpcNotification struct {
	JSONRPC string            `json:"jsonrpc"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

// StatusUpdate carries a single diff from notify_status_update.
// Objects maps printer-object name → changed fields (arbitrary JSON).
type StatusUpdate struct {
	Objects   map[string]map[string]any
	Timestamp float64
}

// Notification is one server-pushed JSON-RPC notification, untouched
// apart from envelope decoding. Method is e.g. "notify_status_update",
// "notify_klippy_ready"; Params is the raw params array (may be empty).
type Notification struct {
	Method string
	Params []json.RawMessage
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// Client manages a WebSocket connection to a Moonraker instance.
//
// Channel lifecycle across reconnect:
//
//   - notifications and disconnected are created once in New() and
//     survive Reconnect(). Long-lived consumer goroutines do NOT need
//     to be re-issued after a reconnect — they remain blocked on these
//     channels and naturally resume reading once a fresh readLoop
//     starts pushing data.
//
//   - Subscriptions do NOT survive. Moonraker drops a connection's
//     subscription when the socket closes, and also when Klippy
//     disconnects/restarts (the socket stays up in that case). The
//     client deliberately doesn't replay subscriptions itself: the
//     consumer owns one "initialize" path (identify → check Klippy
//     state → subscribe) and runs it after Reconnect and after
//     notify_klippy_ready alike.
//
//   - The underlying *websocket.Conn IS replaced on Reconnect. Each
//     readLoop goroutine captures its own conn at start, so an old
//     readLoop exits cleanly without racing against the new one.
//
//   - Pending RPC response channels are closed when the readLoop
//     exits on a connection error, so any blocked call() returns
//     promptly with an error rather than hanging forever.
type Client struct {
	host string // host:port, or "https://host:port" for TLS

	authMu sync.Mutex
	creds  Credentials

	// Auto-incrementing request ID.
	nextID atomic.Int64

	// Serializes WriteMessage calls AND swaps of c.conn — gorilla/
	// websocket panics on concurrent writes, so the same lock that
	// guards writes also protects the conn pointer being replaced
	// out from under an in-flight write.
	writeMu sync.Mutex
	conn    *websocket.Conn

	// Pending requests awaiting a response, keyed by request ID.
	mu      sync.Mutex
	pending map[int64]chan *rpcResponse

	// Channels exposed to consumers. Survive reconnect.
	notifications chan Notification
	disconnected  chan error // buffered=1: signals a connection loss once per drop

	// Set by Close() so the readLoop exit suppresses the disconnect
	// signal during normal shutdown.
	closing atomic.Bool

	// Closed by Close(); call() selects on it to abort blocked waits.
	done chan struct{}
}

// New creates a new Client connected to the given host (host:port).
// It dials ws://<host>/websocket and starts the background read loop.
func New(host string) (*Client, error) {
	return NewWithCredentials(host, Credentials{})
}

// NewWithCredentials connects like New, authenticating with cr. A
// refused connection returns an error wrapping ErrUnauthorized.
func NewWithCredentials(host string, cr Credentials) (*Client, error) {
	c := &Client{
		host:          host,
		creds:         cr,
		pending:       make(map[int64]chan *rpcResponse),
		notifications: make(chan Notification, notificationBuffer),
		disconnected:  make(chan error, 1),
		done:          make(chan struct{}),
	}
	if err := c.dial(); err != nil {
		return nil, err
	}
	go c.readLoop(c.conn)
	return c, nil
}

// dial establishes a fresh websocket connection and swaps it into
// c.conn under writeMu (so an in-flight write can't race with the swap).
func (c *Client) dial() error {
	cr := c.credentials()
	if cr.AccessToken == "" && cr.RefreshToken != "" {
		c.refresh()
	}
	conn, err := c.dialOnce()
	if errors.Is(err, ErrUnauthorized) && c.refresh() {
		conn, err = c.dialOnce()
	}
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	c.conn = conn
	c.writeMu.Unlock()
	return nil
}

func (c *Client) dialOnce() (*websocket.Conn, error) {
	u := wsURL(c.host)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: http.ProxyFromEnvironment}
	h := http.Header{}
	c.credentials().apply(h)
	conn, resp, err := dialer.Dial(u, h)
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return nil, fmt.Errorf("dial %s: %w", u, ErrUnauthorized)
		}
		return nil, fmt.Errorf("dial %s: %w", u, err)
	}
	return conn, nil
}

// Host is the host the client dials.
func (c *Client) Host() string { return c.host }

// Credentials returns what the client authenticates with (the access
// token may have been refreshed since it was created).
func (c *Client) Credentials() Credentials { return c.credentials() }

// notificationBuffer sizes the notification channel. Status diffs are
// dropped (with a log line) if the consumer falls this far behind, so
// it's generous: a burst of ~4 Hz status updates plus gcode output
// during a macro shouldn't come close.
const notificationBuffer = 256

// Reconnect re-establishes the websocket. One attempt — the caller
// drives backoff/retry. It does not resubscribe (see the Client doc
// comment): a successful return means the socket is up, nothing more.
func (c *Client) Reconnect(ctx context.Context) error {
	if c.closing.Load() {
		return fmt.Errorf("client is closing")
	}
	if err := c.dial(); err != nil {
		return err
	}
	go c.readLoop(c.conn)
	return nil
}

// Disconnected returns a channel that emits exactly once each time the
// underlying connection drops. The caller should re-read after each
// successful Reconnect to arm the channel for the next drop.
func (c *Client) Disconnected() <-chan error {
	return c.disconnected
}

// Notifications returns the single channel of every server-pushed
// notification. Use ParseStatusUpdate / ParseGcodeResponse (notify.go)
// to decode the ones you care about.
func (c *Client) Notifications() <-chan Notification {
	return c.notifications
}

// Close shuts down the WebSocket connection.
//
// Sets the closing flag first so the readLoop's exit doesn't fire a
// disconnect notification (it's an expected shutdown, not a drop),
// then closes done so any blocked call() returns promptly.
func (c *Client) Close() error {
	c.closing.Store(true)
	c.writeMu.Lock()
	err := c.conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)
	conn := c.conn
	c.writeMu.Unlock()
	if err != nil {
		_ = conn.Close()
	}
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Request / response plumbing
// ---------------------------------------------------------------------------

// call sends a JSON-RPC request and blocks until the matching response
// arrives (or the connection dies).
func (c *Client) call(method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	// Register a channel for the response before sending, so we
	// can't miss it.
	ch := make(chan *rpcResponse, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.writeMu.Lock()
	err = c.conn.WriteMessage(websocket.TextMessage, data)
	c.writeMu.Unlock()
	if err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("write: %w", err)
	}

	// Wait for the response, a connection drop (readLoop closes our
	// channel from failPending), or a full client close.
	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("connection lost while waiting for response to %s (id=%d)", method, id)
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-c.done:
		return nil, fmt.Errorf("client closed while waiting for response to %s (id=%d)", method, id)
	}
}

func (c *Client) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// failPending closes every pending response channel so any blocked
// call() returns with an error. Called on every readLoop exit.
func (c *Client) failPending() {
	c.mu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Read loop — runs in its own goroutine
// ---------------------------------------------------------------------------

// readLoop takes the conn it should read from as a parameter rather
// than reading c.conn, so a new generation started by Reconnect can run
// safely on the new conn while the previous goroutine (if still
// finishing its exit path on the old conn) doesn't race.
func (c *Client) readLoop(conn *websocket.Conn) {
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if !c.closing.Load() {
				log.Printf("moonraker: read error (disconnected): %v", err)
			}
			c.failPending()
			if !c.closing.Load() {
				// Non-blocking send — channel is buffered=1, so if the
				// previous disconnect notification hasn't been consumed
				// yet we just drop this one (consumer's already going
				// to react to the first).
				select {
				case c.disconnected <- err:
				default:
				}
			}
			return
		}

		c.dispatch(msg)
	}
}

// dispatch routes an incoming message to either a pending-request
// channel or a notification handler.
func (c *Client) dispatch(raw []byte) {
	// Peek at the message to decide if it's a response (has "id") or a
	// notification (has "method" but no "id").
	var probe struct {
		ID     *int64 `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		log.Printf("moonraker: unmarshal probe: %v", err)
		return
	}

	if probe.ID != nil {
		// It's a response to one of our requests.
		var resp rpcResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			log.Printf("moonraker: unmarshal response: %v", err)
			return
		}

		c.mu.Lock()
		ch, ok := c.pending[*resp.ID]
		if ok {
			delete(c.pending, *resp.ID)
		}
		c.mu.Unlock()

		if ok {
			ch <- &resp
		} else {
			log.Printf("moonraker: unexpected response id=%d", *resp.ID)
		}
		return
	}

	// It's a notification — forward it as-is. Routing is the
	// consumer's job; the client has no opinion on which ones matter.
	var notif rpcNotification
	if err := json.Unmarshal(raw, &notif); err != nil {
		log.Printf("moonraker: unmarshal notification: %v", err)
		return
	}

	select {
	case c.notifications <- Notification{Method: notif.Method, Params: notif.Params}:
	default:
		// Drop rather than block the read loop — a blocked read loop
		// would also stall RPC responses.
		log.Printf("moonraker: notification channel full, dropping %s", notif.Method)
	}
}
