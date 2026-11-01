package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justinh-rahb/moontop/internal/moonraker"
	"github.com/justinh-rahb/moontop/internal/moonraker/moonrakertest"
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

func TestRunDump(t *testing.T) {
	srv := afcPrinter(t)
	srv.Handle("printer.objects.query", func(raw json.RawMessage) (any, error) {
		var p struct {
			Objects map[string]any `json:"objects"`
		}
		_ = json.Unmarshal(raw, &p)
		status := map[string]any{}
		for k := range p.Objects {
			status[k] = map[string]any{"queried": true}
		}
		return map[string]any{"eventtime": 1.0, "status": status}, nil
	})
	t.Setenv("MOONRAKER_HOST", srv.Host())
	var out, errb bytes.Buffer
	if code := runDump([]string{"-prefix", "AFC"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var got map[string]map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if len(got) != 6 || got["AFC_lane E2"] == nil || got["toolhead"] != nil {
		t.Fatalf("dumped %d objects: %v", len(got), out.String())
	}
	if code := runDump([]string{"-prefix", "nope"}, &out, &errb); code != 1 {
		t.Fatalf("no match: exit %d", code)
	}
}
