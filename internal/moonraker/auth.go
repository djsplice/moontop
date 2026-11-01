package moonraker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Moonraker authorization: an untrusted client either sends an API key
// (X-Api-Key) or logs in as a user and sends the access JWT as a Bearer
// token. Access tokens last an hour; the refresh token (90 days) gets a
// new one. Moontop sends the credentials as headers on the websocket
// upgrade and on every HTTP request, and refreshes once on a 401.

// Credentials authenticate a client. All fields are optional; a trusted
// client needs none.
type Credentials struct {
	APIKey       string
	Username     string
	AccessToken  string
	RefreshToken string
}

// Empty reports whether there is nothing to send.
func (c Credentials) Empty() bool {
	return c.APIKey == "" && c.AccessToken == "" && c.RefreshToken == ""
}

func (c Credentials) apply(h http.Header) {
	if c.APIKey != "" {
		h.Set("X-Api-Key", c.APIKey)
	}
	if c.AccessToken != "" {
		h.Set("Authorization", "Bearer "+c.AccessToken)
	}
}

// ErrUnauthorized means Moonraker refused the connection: it needs a
// login or an API key, or the ones given are wrong.
var ErrUnauthorized = errors.New("moonraker: unauthorized")

// AccessInfo is /access/info: what kinds of login the server accepts.
type AccessInfo struct {
	DefaultSource    string   `json:"default_source"`
	AvailableSources []string `json:"available_sources"`
	LoginRequired    bool     `json:"login_required"`
	Trusted          bool     `json:"trusted"`
}

// NormalizeHost turns what a person types into what the client dials:
// "voron.local" → "voron.local:7125", "http://10.0.0.5/" → "10.0.0.5:7125",
// "https://printer.example.com" → "https://printer.example.com:443".
// A plain host keeps no scheme; https / wss keep "https://".
func NormalizeHost(s string) string {
	s = strings.TrimSpace(s)
	secure := false
	for _, p := range []string{"https://", "wss://"} {
		if strings.HasPrefix(strings.ToLower(s), p) {
			s, secure = s[len(p):], true
		}
	}
	for _, p := range []string{"http://", "ws://"} {
		if strings.HasPrefix(strings.ToLower(s), p) {
			s = s[len(p):]
		}
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		port := "7125"
		if secure {
			port = "443"
		}
		h := strings.Trim(s, "[]")
		s = net.JoinHostPort(h, port)
	}
	if secure {
		return "https://" + s
	}
	return s
}

// splitHost separates the "https://" marker from host:port.
func splitHost(host string) (addr string, secure bool) {
	if strings.HasPrefix(host, "https://") {
		return strings.TrimPrefix(host, "https://"), true
	}
	return host, false
}

func httpBase(host string) string {
	addr, secure := splitHost(host)
	if secure {
		return "https://" + addr
	}
	return "http://" + addr
}

func wsURL(host string) string {
	addr, secure := splitHost(host)
	if secure {
		return "wss://" + addr + "/websocket"
	}
	return "ws://" + addr + "/websocket"
}

// GetAccessInfo asks an (unauthenticated) server how it wants clients
// to log in.
func GetAccessInfo(host string) (AccessInfo, error) {
	var info AccessInfo
	err := postOrGet(http.MethodGet, httpBase(host)+"/access/info", nil, &info)
	return info, err
}

// Login logs in as a Moonraker user. source "" uses the server default.
func Login(host, username, password, source string) (Credentials, error) {
	body := map[string]string{"username": username, "password": password}
	if source != "" {
		body["source"] = source
	}
	var res struct {
		Username     string `json:"username"`
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := postOrGet(http.MethodPost, httpBase(host)+"/access/login", body, &res); err != nil {
		return Credentials{}, err
	}
	if res.Username == "" {
		res.Username = username
	}
	return Credentials{Username: res.Username, AccessToken: res.Token, RefreshToken: res.RefreshToken}, nil
}

// refreshAccessToken trades a refresh token for a new access token.
func refreshAccessToken(host, refreshToken string) (string, error) {
	var res struct {
		Token string `json:"token"`
	}
	err := postOrGet(http.MethodPost, httpBase(host)+"/access/refresh_jwt", map[string]string{"refresh_token": refreshToken}, &res)
	if err == nil && res.Token == "" {
		err = errors.New("refresh: no token in response")
	}
	return res.Token, err
}

// postOrGet does one JSON request to an auth endpoint, unwrapping
// Moonraker's {"result": …} envelope into out.
func postOrGet(method, u string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: %s", ErrUnauthorized, errorMessage(data, resp.Status))
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %s", path(u), errorMessage(data, resp.Status))
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &env); err != nil || len(env.Result) == 0 {
		return fmt.Errorf("%s: unexpected response", path(u))
	}
	return json.Unmarshal(env.Result, out)
}

// errorMessage pulls Moonraker's {"error": {"message": …}} out of a
// failed response, falling back to the HTTP status.
func errorMessage(data []byte, status string) string {
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &env) == nil && env.Error.Message != "" {
		return env.Error.Message
	}
	return status
}

func path(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Path
	}
	return u
}

// credentials returns the client's current credentials.
func (c *Client) credentials() Credentials {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	return c.creds
}

// refresh gets a new access token, if the client logged in. It reports
// whether there is a new token to retry with.
func (c *Client) refresh() bool {
	cr := c.credentials()
	if cr.RefreshToken == "" {
		return false
	}
	tok, err := refreshAccessToken(c.host, cr.RefreshToken)
	if err != nil {
		return false
	}
	c.authMu.Lock()
	c.creds.AccessToken = tok
	c.authMu.Unlock()
	return true
}

// do sends an HTTP request with the client's credentials, refreshing
// the access token and retrying once on a 401.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	c.credentials().apply(req.Header)
	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || req.GetBody == nil && req.Body != nil {
		return resp, err
	}
	if !c.refresh() {
		return resp, err
	}
	resp.Body.Close()
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		if retry.Body, err = req.GetBody(); err != nil {
			return nil, err
		}
	}
	c.credentials().apply(retry.Header)
	return httpClient.Do(retry)
}
