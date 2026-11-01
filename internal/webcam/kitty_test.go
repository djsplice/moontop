package webcam

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestGraphicsFrom(t *testing.T) {
	lookup := func(env map[string]string) func(string) string {
		return func(k string) string { return env[k] }
	}
	if !graphicsFrom(lookup(map[string]string{"TERM_PROGRAM": "ghostty"})) {
		t.Fatal("ghostty")
	}
	if !graphicsFrom(lookup(map[string]string{"TERM_PROGRAM": "WezTerm"})) {
		t.Fatal("wezterm")
	}
	if !graphicsFrom(lookup(map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "tmux-256color"})) {
		t.Fatal("kitty inside tmux")
	}
	if graphicsFrom(lookup(map[string]string{"TERM": "dumb"})) {
		t.Fatal("dumb")
	}
	if graphicsFrom(lookup(map[string]string{})) {
		t.Fatal("empty")
	}
}

func TestPlaceKittyIsZeroWidth(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 4))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	const cols, rows = 20, 6
	place := PlaceKitty(buf.Bytes(), cols, rows)
	if place == "" {
		t.Fatal("empty placement")
	}
	if ansi.StringWidth(place) != 0 || strings.Contains(place, "\n") {
		t.Fatalf("placement width %d newline %v", ansi.StringWidth(place), strings.Contains(place, "\n"))
	}
	if !strings.Contains(place, "a=T,f=100,q=2,i=7,c=20,r=6,C=1,z=1,m=") {
		t.Fatalf("placement missing: %.160q", place)
	}
	body := place + Draw(img, cols, rows)
	if lipgloss.Width(body) != cols || lipgloss.Height(body) != rows {
		t.Fatalf("size %dx%d, want %dx%d", lipgloss.Width(body), lipgloss.Height(body), cols, rows)
	}
	line := strings.Split(body, "\n")[0]
	if got := ansi.Truncate(line, 200, ""); got != line {
		t.Fatal("truncate dropped a line that fits")
	}
	if ansi.StringWidth(DeleteKitty()) != 0 {
		t.Fatal("delete sequence has width")
	}
	if PlaceKitty(nil, cols, rows) != "" {
		t.Fatal("empty png produced a placement")
	}
}

func TestKittyTransmitChunks(t *testing.T) {
	payload := bytes.Repeat([]byte{'A'}, 4000)
	s := kittyTransmit(payload, 7, 10, 4)
	if strings.Count(s, "\x1b_G") < 2 {
		t.Fatalf("chunks = %d", strings.Count(s, "\x1b_G"))
	}
	if !strings.Contains(s, "m=1") || !strings.Contains(s, "m=0") {
		t.Fatal("chunk flags")
	}
	if strings.Contains(s, "\n") {
		t.Fatal("transmit contains a newline")
	}
	if ansi.StringWidth(s) != 0 {
		t.Fatalf("transmit width %d", ansi.StringWidth(s))
	}
}
