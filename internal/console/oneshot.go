package console

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/justinh-rahb/moonraker-tui/internal/moonraker"
)

// Runner sends gcode one command at a time and prints Klipper's output
// for each command as it streams, returning once that command finishes.
//
// "Finished" is the printer.gcode.script reply: Moonraker answers only
// after Klipper has executed the script (G28 returns after homing, M190
// after the bed reaches temperature). The output that a command prints
// travels on the same websocket ahead of that reply, so once the reply
// arrives everything the command printed is already in the
// notification channel and can be drained without waiting.
//
// Output from other clients (Mainsail, a running print's macros) that
// arrives while a command runs is printed too — Moonraker broadcasts
// gcode responses to every connection.
type Runner struct {
	Client *moonraker.Client

	// Out receives Klipper output, one line per Println; Err receives
	// command failures. Both required.
	Out, Err io.Writer

	// Echo prints each command (">>> G28") to Out before sending it.
	Echo bool

	// KeepGoing continues after a failed command instead of stopping.
	KeepGoing bool

	// Style, if set, decorates a line before printing (colour by Kind).
	Style func(Kind, string) string

	// Filter hides matching Klipper output; Hidden counts what it hid.
	Filter FilterSet
	Hidden int

	last string // last line printed to Out, to avoid printing an error twice
}

// RunAll runs cmds in order. It returns the number of failed commands;
// unless KeepGoing is set it stops at the first failure.
func (r *Runner) RunAll(cmds []string) int {
	failed := 0
	for _, c := range cmds {
		if !r.Run(c) {
			failed++
			if !r.KeepGoing {
				break
			}
		}
	}
	return failed
}

// RunReader runs one command per input line as lines arrive, so a
// slow producer (a script, a FIFO) streams rather than batching. Blank
// lines and whole-line ";" comments are skipped; inline comments are
// left for Klipper, which strips them itself.
func (r *Runner) RunReader(in io.Reader) (failed int, err error) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if !r.Run(line) {
			failed++
			if !r.KeepGoing {
				return failed, nil
			}
		}
	}
	return failed, sc.Err()
}

// Run sends one command and reports whether it succeeded.
func (r *Runner) Run(cmd string) bool {
	if r.Echo {
		r.print(r.Out, PrefixCommand+cmd)
	}
	done := make(chan error, 1)
	go func() { done <- r.Client.GcodeScript(cmd) }()

	notes := r.Client.Notifications()
	for {
		select {
		case n := <-notes:
			r.handle(n)
		case err := <-done:
			r.drain()
			if err != nil {
				line := ErrorLine(err)
				// Klipper sometimes echoes the same error as a
				// gcode response; don't print it twice.
				if line != r.last {
					r.print(r.Err, line)
				}
				return false
			}
			return true
		}
	}
}

func (r *Runner) drain() {
	notes := r.Client.Notifications()
	for {
		select {
		case n := <-notes:
			r.handle(n)
		default:
			return
		}
	}
}

func (r *Runner) handle(n moonraker.Notification) {
	if n.Method != moonraker.MethodGcodeResponse {
		return
	}
	msg, err := moonraker.ParseGcodeResponse(n.Params)
	if err != nil {
		return
	}
	for _, l := range strings.Split(msg, "\n") {
		if r.Filter.Hides(l) {
			r.Hidden++
			continue
		}
		r.print(r.Out, l)
	}
}

func (r *Runner) print(w io.Writer, line string) {
	if w == r.Out {
		r.last = line
	}
	if r.Style != nil {
		line = r.Style(ClassifyLine(line), line)
	}
	fmt.Fprintln(w, line)
}
