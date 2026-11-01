package webcam

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPickPrefersWebRTC(t *testing.T) {
	cams := []Cam{
		{Name: "SpaghettiSpy", Service: "mjpegstreamer", StreamURL: "/webcam/?action=stream", SnapshotURL: "/webcam/?action=snapshot", Enabled: true},
		{Name: "case", Service: "webrtc-camerastreamer", StreamURL: "/webcam/webrtc", SnapshotURL: "/webcam/snapshot.jpg", Enabled: true},
	}
	got, ok := Pick(cams)
	if !ok || got.Name != "case" {
		t.Fatalf("pick = %+v ok=%v", got, ok)
	}
}

func TestPickSkipsDisabledAndEmpty(t *testing.T) {
	if _, ok := Pick([]Cam{{Name: "off", Enabled: false, SnapshotURL: "/a.jpg"}}); ok {
		t.Fatal("disabled camera was picked")
	}
	got, ok := Pick([]Cam{{Name: "plain", Service: "mjpegstreamer", SnapshotURL: "/a.jpg", Enabled: true}})
	if !ok || got.Name != "plain" {
		t.Fatalf("fallback = %+v ok=%v", got, ok)
	}
}

func TestResolveSnapshotURL(t *testing.T) {
	cases := []struct{ host, snap, want string }{
		{"192.168.1.217:7125", "/webcam/snapshot.jpg", "http://192.168.1.217/webcam/snapshot.jpg"},
		{"https://printer.local", "/webcam/snapshot.jpg", "https://printer.local/webcam/snapshot.jpg"},
		{"https://printer.local:7130", "webcam/snapshot.jpg", "https://printer.local:7130/webcam/snapshot.jpg"},
		{"printer.local:8080", "/snap.jpg", "http://printer.local:8080/snap.jpg"},
		{"192.168.1.217:7125", "http://10.0.0.5/snap.jpg", "http://10.0.0.5/snap.jpg"},
		{"[2001:db8::1]:7125", "/webcam/snapshot.jpg", "http://[2001:db8::1]/webcam/snapshot.jpg"},
	}
	for _, c := range cases {
		if got := Resolve(c.host, c.snap); got != c.want {
			t.Errorf("Resolve(%q, %q) = %q, want %q", c.host, c.snap, got, c.want)
		}
	}
}

func TestDrawHalfBlock(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	red := color.RGBA{R: 255, A: 255}
	green := color.RGBA{G: 255, A: 255}
	img.Set(0, 0, red)
	img.Set(1, 0, red)
	img.Set(0, 1, green)
	img.Set(1, 1, green)
	got := Draw(img, 1, 1)
	if !strings.Contains(got, "\x1b[38;2;255;0;0m") || !strings.Contains(got, "\x1b[48;2;0;255;0m") || !strings.Contains(got, "▀") {
		t.Fatalf("draw = %q", got)
	}
}

func TestDrawQuad(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	red := color.RGBA{R: 255, A: 255}
	black := color.RGBA{A: 255}
	img.Set(0, 0, red)
	img.Set(1, 0, black)
	img.Set(0, 1, black)
	img.Set(1, 1, black)
	got := DrawQuad(img, 1, 1)
	if !strings.Contains(got, "▘") || !strings.Contains(got, "\x1b[38;2;255;0;0m") {
		t.Fatalf("quad = %q", got)
	}
	if lipgloss.Width(got) != 1 {
		t.Fatalf("width %d", lipgloss.Width(got))
	}

	flat := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			flat.Set(x, y, color.RGBA{G: 40, A: 255})
		}
	}
	if strings.ContainsAny(DrawQuad(flat, 1, 1), "▘▝▀▖▌▞▛▗▚▐▜▄▙▟█") {
		t.Fatal("flat cell used a quadrant glyph")
	}
}
