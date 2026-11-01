package moonrakertest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// fakeAuth is a minimal Moonraker authorization component: an API key,
// one user, access tokens that can be expired, and a refresh token.
type fakeAuth struct {
	apiKey, user, password string
	refresh                string
	valid                  map[string]bool // access tokens accepted
	issued                 int
	logins, refreshes      int
}

// RequireAuth makes the server refuse untrusted clients: the websocket
// and HTTP endpoints need X-Api-Key: apiKey, or a Bearer token from
// /access/login with user / password. /access/info, /access/login and
// /access/refresh_jwt stay open, as in Moonraker. Empty apiKey or user
// disables that method.
func (s *Server) RequireAuth(apiKey, user, password string) {
	s.mu.Lock()
	s.auth = &fakeAuth{apiKey: apiKey, user: user, password: password, refresh: "refresh-token", valid: map[string]bool{}}
	s.mu.Unlock()
}

// ExpireTokens invalidates every access token issued so far, as if an
// hour had passed; the refresh token still works.
func (s *Server) ExpireTokens() {
	s.mu.Lock()
	if s.auth != nil {
		s.auth.valid = map[string]bool{}
	}
	s.mu.Unlock()
}

// AuthCounts reports how many logins and token refreshes were served.
func (s *Server) AuthCounts() (logins, refreshes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.auth == nil {
		return 0, 0
	}
	return s.auth.logins, s.auth.refreshes
}

func writeResult(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"result": v})
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": msg}})
}

// serveAuth answers the /access endpoints and rejects unauthorized
// requests. It reports whether it wrote a response.
func (s *Server) serveAuth(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.auth
	switch r.URL.Path {
	case "/access/info":
		writeResult(w, map[string]any{
			"default_source": "moonraker", "available_sources": []string{"moonraker"},
			"login_required": a != nil, "trusted": a == nil,
		})
		return true
	case "/access/login":
		var body struct{ Username, Password, Source string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		if a == nil || a.user == "" || body.Username != a.user || body.Password != a.password {
			writeError(w, http.StatusUnauthorized, "Invalid Username or Password")
			return true
		}
		a.logins++
		writeResult(w, map[string]any{"username": a.user, "token": a.issue(), "refresh_token": a.refresh, "action": "user_logged_in", "source": "moonraker"})
		return true
	case "/access/refresh_jwt":
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if a == nil || body.RefreshToken != a.refresh {
			writeError(w, http.StatusUnauthorized, "Invalid refresh token")
			return true
		}
		a.refreshes++
		writeResult(w, map[string]any{"username": a.user, "token": a.issue(), "source": "moonraker", "action": "user_jwt_refresh"})
		return true
	}
	if a == nil {
		return false
	}
	if a.apiKey != "" && r.Header.Get("X-Api-Key") == a.apiKey {
		return false
	}
	if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && a.valid[tok] {
		return false
	}
	writeError(w, http.StatusUnauthorized, "Unauthorized")
	return true
}

func (a *fakeAuth) issue() string {
	a.issued++
	t := fmt.Sprintf("access-%d", a.issued)
	a.valid[t] = true
	return t
}
