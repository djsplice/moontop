package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Set at release build time by GoReleaser (-ldflags "-X main.version=…").
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func init() {
	// `go install …@v1.2.3` builds carry the module version instead.
	if version == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
	}
}

func versionString() string {
	s := "moontop " + version
	if commit != "" {
		short := commit
		if len(short) > 7 {
			short = short[:7]
		}
		s += " (" + short
		if date != "" {
			s += ", " + date
		}
		s += ")"
	}
	return fmt.Sprintf("%s %s/%s", s, runtime.GOOS, runtime.GOARCH)
}
