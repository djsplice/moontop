package webcam

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// kittyImageID is the one image slot the camera pane reuses. Sending
// it again replaces the previous frame.
const kittyImageID = 7

// GraphicsEnabled reports whether this terminal can show a Kitty image.
// Ghostty, Kitty, and WezTerm can. Detection is the environment the
// process was started with; tmux leaves TERM_PROGRAM in place and
// rewrites TERM, so the program name is what we trust.
func GraphicsEnabled() bool {
	return graphicsFrom(os.Getenv)
}

func graphicsFrom(getenv func(string) string) bool {
	if getenv("KITTY_WINDOW_ID") != "" || getenv("WEZTERM_PANE") != "" {
		return true
	}
	switch strings.ToLower(getenv("TERM_PROGRAM")) {
	case "ghostty", "kitty", "wezterm":
		return true
	}
	term := strings.ToLower(getenv("TERM"))
	return strings.Contains(term, "kitty") || strings.Contains(term, "ghostty")
}

// SharpGraphics is GraphicsEnabled and, when running inside tmux, a
// server that will let the graphics sequence through. tmux strips it
// unless allow-passthrough is on (or all).
func SharpGraphics() bool {
	if !GraphicsEnabled() {
		return false
	}
	return !tmuxBlocksGraphics()
}

func tmuxBlocksGraphics() bool {
	if os.Getenv("TMUX") == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "show-option", "-gv", "allow-passthrough").Output()
	if err != nil {
		return true
	}
	switch strings.TrimSpace(string(out)) {
	case "on", "all":
		return false
	default:
		return true
	}
}

// PlaceKitty is the zero-width sequence that draws png in a cols by
// rows cell rectangle at the cursor, above the text that follows.
// Unicode placeholders are not used: U+10EEEE is two columns wide in
// the width tables the layout uses, so a placeholder grid wraps the pane.
// A direct placement is ignored by terminals that lack the protocol,
// and the text underneath (the half-block picture) still shows.
func PlaceKitty(png []byte, cols, rows int) string {
	if len(png) == 0 || cols < 1 || rows < 1 {
		return ""
	}
	return kittyTransmit(png, kittyImageID, cols, rows)
}

// DeleteKitty removes the camera image so a placement cannot sit on
// top of the console after the pane is swapped back.
func DeleteKitty() string {
	return fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", kittyImageID)
}

// kittyTransmit sends PNG bytes (f=100) and displays them. z=1 keeps
// the picture above the half-block text. C=1 leaves the cursor where
// it is, so the text that follows occupies the same cells. Chunks stay
// on one line so a panel splitter that cuts on newlines cannot divide
// a command.
func kittyTransmit(png []byte, id, cols, rows int) string {
	payload := base64.StdEncoding.EncodeToString(png)
	const chunk = 4096
	var b strings.Builder
	for off := 0; ; {
		end := off + chunk
		last := end >= len(payload)
		if last {
			end = len(payload)
		}
		more := "1"
		if last {
			more = "0"
		}
		b.WriteString("\x1b_G")
		if off == 0 {
			fmt.Fprintf(&b, "a=T,f=100,q=2,i=%d,c=%d,r=%d,C=1,z=1,m=%s", id, cols, rows, more)
		} else {
			b.WriteString("m=" + more)
		}
		b.WriteByte(';')
		b.WriteString(payload[off:end])
		b.WriteString("\x1b\\")
		if last {
			return b.String()
		}
		off = end
	}
}
