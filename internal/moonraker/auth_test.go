package moonraker_test

import (
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/justinh-rahb/moontop/internal/moonraker"
	"github.com/justinh-rahb/moontop/internal/moonraker/moonrakertest"
)

func TestNormalizeHost(t *testing.T) {
	for in, want := range map[string]string{
		"voron.local":                       "voron.local:7125",
		" 10.0.3.228:7125 ":                 "10.0.3.228:7125",
		"http://10.0.0.5/":                  "10.0.0.5:7125",
		"http://10.0.0.5:80/mainsail":       "10.0.0.5:80",
		"ws://printer:7125/websocket":       "printer:7125",
		"https://printer.example.com":       "https://printer.example.com:443",
		"HTTPS://printer.example.com:8443/": "https://printer.example.com:8443",
		"::1":                               "[::1]:7125",
		"[fe80::1]:7125":                    "[fe80::1]:7125",
		"":                                  "",
	} {
		if got := moonraker.NormalizeHost(in); got != want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnauthorizedDial(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.RequireAuth("key123", "", "")
	if _, err := moonraker.New(srv.Host()); !errors.Is(err, moonraker.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if _, err := moonraker.NewWithCredentials(srv.Host(), moonraker.Credentials{APIKey: "nope"}); !errors.Is(err, moonraker.ErrUnauthorized) {
		t.Fatalf("wrong key: err = %v", err)
	}
	c, err := moonraker.NewWithCredentials(srv.Host(), moonraker.Credentials{APIKey: "key123"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.GetServerInfo(); err != nil {
		t.Fatal(err)
	}
}

func TestAccessInfoAndLogin(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.RequireAuth("", "admin", "hunter2")
	info, err := moonraker.GetAccessInfo(srv.Host())
	if err != nil || !info.LoginRequired || info.Trusted || len(info.AvailableSources) != 1 {
		t.Fatalf("info = %+v, %v", info, err)
	}
	if _, err := moonraker.Login(srv.Host(), "admin", "wrong", ""); !errors.Is(err, moonraker.ErrUnauthorized) {
		t.Fatalf("bad password: %v", err)
	}
	cr, err := moonraker.Login(srv.Host(), "admin", "hunter2", "moonraker")
	if err != nil || cr.AccessToken == "" || cr.RefreshToken == "" || cr.Username != "admin" {
		t.Fatalf("login = %+v, %v", cr, err)
	}
	c, err := moonraker.NewWithCredentials(srv.Host(), cr)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

// A saved refresh token alone is enough to connect, and an expired
// access token is refreshed for HTTP requests.
func TestRefreshTokenFlow(t *testing.T) {
	srv := moonrakertest.New(t)
	srv.RequireAuth("", "admin", "pw")
	srv.HandleHTTP("/server/files/", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello") })
	cr, err := moonraker.Login(srv.Host(), "admin", "pw", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := moonraker.NewWithCredentials(srv.Host(), moonraker.Credentials{Username: "admin", RefreshToken: cr.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	srv.ExpireTokens()
	data, _, err := c.FetchFile("config", "printer.cfg", 0)
	if err != nil || string(data) != "hello" {
		t.Fatalf("fetch = %q, %v", data, err)
	}
	if err := c.UploadFile("config", "a.cfg", []byte("x")); err != nil {
		t.Fatalf("upload after expiry: %v", err)
	}
	if _, refreshes := srv.AuthCounts(); refreshes < 2 {
		t.Fatalf("refreshes = %d, want at least 2 (connect, expired fetch)", refreshes)
	}
	srv.ExpireTokens()
	srv.DropConnections()
	time.Sleep(50 * time.Millisecond)
	if err := c.Reconnect(t.Context()); err != nil {
		t.Fatalf("reconnect with an expired token: %v", err)
	}
}
