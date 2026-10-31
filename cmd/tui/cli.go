package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moonraker-tui/internal/config"
	"github.com/justinh-rahb/moonraker-tui/internal/console"
	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches on the first argument. Anything that isn't a known
// subcommand (including flags like -host) goes to the dashboard, so
// `moontop -host …` keeps working as before.
func run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "console":
			return runConsole(args[1:])
		case "gcode":
			return runGcode(args[1:], os.Stdin, os.Stdout, os.Stderr)
		case "help", "-h", "-help", "--help":
			usage()
			return 0
		}
	}
	return runDashboard(args)
}

func usage() {
	fmt.Fprint(os.Stderr, `Moontop — a terminal UI for Moonraker / Klipper

Usage:
  moontop [-host ip:port]                    full-screen dashboard
  moontop console [-host ip:port]            interactive gcode console in this terminal
  moontop gcode [-host ip:port] [flags] CMD… run gcode, print the output, exit
  … | moontop gcode [-host ip:port] [flags]  run gcode read from stdin, one per line

-host defaults to $MOONRAKER_HOST (Moonraker's port is usually 7125).
-config picks the settings file (default ~/.config/moontop/config.toml);
press F2 in the dashboard to change settings.

gcode flags:
  -echo    print each command before its output
  -k       keep going after a failed command (default: stop)
  -filter  hide lines matched by your console filters (heat-wait
           temperatures and TMC "Run Current" by default; raw output
           unless given; filters are on in console and dashboard)

gcode exits 0 when every command succeeded, 1 when any failed, 2 on
usage or connection errors.
`)
}

func hostFlag(fs *flag.FlagSet) *string {
	return fs.String("host", "", "Moonraker host:port (default $MOONRAKER_HOST)")
}

func configFlag(fs *flag.FlagSet) *string {
	return fs.String("config", "", "settings file (default ~/.config/moontop/config.toml)")
}

// loadConfig reads the settings file. On a parse error it warns and
// returns defaults with an empty path, so a hand-edited file with a typo
// is never overwritten by the setup screen.
func loadConfig(flagPath string) (config.Config, string) {
	path := flagPath
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "moontop: settings: %v (using defaults)\n", err)
			return config.Config{}, ""
		}
		path = p
	}
	c, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "moontop: settings: %v\n  using defaults; fix the file to save settings again\n", err)
		time.Sleep(1500 * time.Millisecond) // let it be read before the screen clears
		return config.Config{}, ""
	}
	return *c, path
}

// resolveHost falls back to $MOONRAKER_HOST and explains when neither
// is set.
func resolveHost(flagVal string) (string, bool) {
	if flagVal != "" {
		return flagVal, true
	}
	if env := os.Getenv("MOONRAKER_HOST"); env != "" {
		return env, true
	}
	fmt.Fprintln(os.Stderr, "moontop: no printer host — pass -host ip:port or set MOONRAKER_HOST")
	return "", false
}

// isTerminal reports whether f is a character device (a TTY) rather
// than a pipe or file.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// runGcode is `moontop gcode`: commands from args, or from stdin when
// no args are given and stdin isn't a terminal.
func runGcode(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gcode", flag.ContinueOnError)
	fs.SetOutput(stderr)
	host := hostFlag(fs)
	echo := fs.Bool("echo", false, "print each command before its output")
	keepGoing := fs.Bool("k", false, "keep going after a failed command")
	filter := fs.Bool("filter", false, "hide lines matched by the console filters in your settings")
	cfgFlag := configFlag(fs)
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cmds := fs.Args()
	if len(cmds) == 0 && isTerminal(stdin) {
		fmt.Fprintln(stderr, "moontop gcode: no commands — pass them as arguments or pipe them on stdin")
		fmt.Fprintln(stderr, "  (for an interactive prompt, use: moontop console)")
		return 2
	}
	h, ok := resolveHost(*host)
	if !ok {
		return 2
	}

	// The client logs connection drops via the log package; in a
	// script that's noise on stderr next to real errors.
	log.SetOutput(io.Discard)
	client, err := moonraker.New(h)
	if err != nil {
		fmt.Fprintf(stderr, "moontop gcode: connect to %s: %v\n", h, err)
		return 2
	}
	defer client.Close()

	r := &console.Runner{
		Client:    client,
		Out:       stdout,
		Err:       stderr,
		Echo:      *echo,
		KeepGoing: *keepGoing,
		Style:     lineStyler(stdout),
	}
	if *filter {
		cfg, _ := loadConfig(*cfgFlag)
		r.Filter = buildFilters(cfg.Console)
	}
	var failed int
	if len(cmds) > 0 {
		// Each argument is one command, so `moontop gcode G28 "G1 X10"`
		// works without quoting the whole list.
		failed = r.RunAll(cmds)
	} else {
		failed, err = r.RunReader(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "moontop gcode: reading stdin: %v\n", err)
			return 2
		}
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// lineStyler colours console lines for w — only when w is a terminal
// that supports colour (lipgloss detects this, and honours NO_COLOR),
// so piped output stays plain text.
func lineStyler(w io.Writer) func(console.Kind, string) string {
	r := lipgloss.NewRenderer(w)
	styles := map[console.Kind]lipgloss.Style{
		console.KindError:   r.NewStyle().Foreground(colorErr),
		console.KindInfo:    r.NewStyle().Foreground(colorTextMuted),
		console.KindCommand: r.NewStyle().Foreground(colorAccentWarm),
		console.KindNotice:  r.NewStyle().Foreground(colorOk),
	}
	return func(k console.Kind, l string) string {
		if s, ok := styles[k]; ok {
			return s.Render(l)
		}
		return l
	}
}
