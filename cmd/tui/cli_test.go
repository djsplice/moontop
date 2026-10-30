package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
	"github.com/justinh-rahb/moonraker-tui/internal/moonraker/moonrakertest"
)

func gcodeServer(t *testing.T) *moonrakertest.Server {
	srv := moonrakertest.New(t)
	srv.Handle("printer.gcode.script", func(raw json.RawMessage) (any, error) {
		var p struct {
			Script string `json:"script"`
		}
		_ = json.Unmarshal(raw, &p)
		if p.Script == "BAD" {
			return nil, errors.New(`Unknown command:"BAD"`)
		}
		srv.Notify(moonraker.MethodGcodeResponse, "// ran "+p.Script)
		return "ok", nil
	})
	return srv
}

func TestRunGcode(t *testing.T) {
	srv := gcodeServer(t)
	t.Setenv("MOONRAKER_HOST", srv.Host())

	// stdin that isn't a terminal and is empty — only read when no args.
	empty, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()

	cases := []struct {
		name     string
		args     []string
		stdin    string // non-empty: piped stdin instead of /dev/null
		code     int
		stdout   string
		stderrIn string
	}{
		{"args", []string{"G28", "M115"}, "", 0, "// ran G28\n// ran M115\n", ""},
		{"echo", []string{"-echo", "G28"}, "", 0, ">>> G28\n// ran G28\n", ""},
		{"stops at error", []string{"BAD", "G28"}, "", 1, "", `!! Unknown command:"BAD"`},
		{"keep going", []string{"-k", "BAD", "G28"}, "", 1, "// ran G28\n", "BAD"},
		{"stdin", nil, "; hi\nG28\n\nM115\n", 0, "// ran G28\n// ran M115\n", ""},
		{"explicit host wins", []string{"-host", srv.Host(), "G28"}, "", 0, "// ran G28\n", ""},
		{"bad flag", []string{"-nope"}, "", 2, "", "flag provided but not defined"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := empty
			if tc.stdin != "" {
				p := filepath.Join(t.TempDir(), "in")
				if err := os.WriteFile(p, []byte(tc.stdin), 0o644); err != nil {
					t.Fatal(err)
				}
				f, err := os.Open(p)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				in = f
			}
			var out, errb bytes.Buffer
			code := runGcode(tc.args, in, &out, &errb)
			if code != tc.code {
				t.Fatalf("exit %d, want %d (stderr %q)", code, tc.code, errb.String())
			}
			if out.String() != tc.stdout {
				t.Fatalf("stdout %q, want %q", out.String(), tc.stdout)
			}
			if !strings.Contains(errb.String(), tc.stderrIn) {
				t.Fatalf("stderr %q missing %q", errb.String(), tc.stderrIn)
			}
		})
	}
}

func TestRunGcodeNoHost(t *testing.T) {
	t.Setenv("MOONRAKER_HOST", "")
	var out, errb bytes.Buffer
	if code := runGcode([]string{"G28"}, os.Stdin, &out, &errb); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
