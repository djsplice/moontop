package webcam

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"strings"
	"testing"
)

func TestH264Path(t *testing.T) {
	cases := []struct{ stream, snap, want string }{
		{"/webcam/webrtc", "/webcam/snapshot.jpg", "/webcam/stream.h264"},
		{"http://10.0.0.5/webcam/webrtc", "/webcam/snapshot.jpg", "http://10.0.0.5/webcam/stream.h264"},
		{"https://printer.local:7130/webcam/webrtc", "", "https://printer.local:7130/webcam/stream.h264"},
		{"", "/webcam/snapshot.jpg", "/webcam/stream.h264"},
		{"/webcam/stream.h264", "", "/webcam/stream.h264"},
		{"/webcam/?action=stream", "/webcam/?action=snapshot", ""},
		{"/webcam/webrtc?x=1", "", "/webcam/stream.h264"},
	}
	for _, c := range cases {
		if got := H264Path(c.stream, c.snap); got != c.want {
			t.Errorf("H264Path(%q, %q) = %q, want %q", c.stream, c.snap, got, c.want)
		}
	}
}

func TestFFmpegArgs(t *testing.T) {
	url := "http://192.168.1.217/webcam/stream.h264"
	args, err := ffmpegArgs(url, FrameWide)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for i := 0; i+1 < len(args); i++ {
		if strings.HasPrefix(args[i], "-") && !strings.HasPrefix(args[i+1], "-") {
			got[args[i]] = args[i+1]
		}
	}
	if got["-i"] != url {
		t.Fatalf("-i = %q", got["-i"])
	}
	if got["-vf"] != "fps=12,scale=960:-2" {
		t.Fatalf("-vf = %q", got["-vf"])
	}
	narrow, err := ffmpegArgs(url, FrameNarrow)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(narrow, " "), "scale=640:-2") {
		t.Fatalf("narrow args = %q", narrow)
	}
	small, err := ffmpegArgs(url, FrameSmall)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(small, " "), "scale=480:-2") {
		t.Fatalf("small args = %q", small)
	}
	fallback, err := ffmpegArgs(url, 320)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fallback, " "), "scale=960:-2") {
		t.Fatalf("fallback args = %q", fallback)
	}
	for i, a := range args {
		if a == "-i" && i > 0 && args[i-1] == "-vf" {
			t.Fatal("scale filter is an input option")
		}
	}
	for _, bad := range []string{"file:///etc/passwd", "-hide_banner", "http:///nohost", "not a url"} {
		if _, err := ffmpegArgs(bad, FrameWide); err == nil {
			t.Errorf("ffmpegArgs(%q) accepted", bad)
		}
	}
}

func TestSplitJPEG(t *testing.T) {
	var one, two bytes.Buffer
	enc := func(w *bytes.Buffer, c color.RGBA) {
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.Set(0, 0, c)
		if err := jpeg.Encode(w, img, &jpeg.Options{Quality: 50}); err != nil {
			t.Fatal(err)
		}
	}
	enc(&one, color.RGBA{R: 255, A: 255})
	enc(&two, color.RGBA{G: 255, A: 255})

	var stream bytes.Buffer
	stream.Write([]byte{0x00, 0xFF}) // junk, including a split marker
	stream.Write(one.Bytes())
	stream.Write([]byte("nope"))
	stream.Write(two.Bytes())

	// Deliver one byte at a time so a marker can straddle a read.
	var got int
	err := splitJPEG(&byteReader{b: stream.Bytes()}, func(frame []byte) error {
		img, err := jpeg.Decode(bytes.NewReader(frame))
		if err != nil {
			t.Fatalf("frame %d: %v", got, err)
		}
		if img.Bounds().Dx() != 2 {
			t.Fatalf("frame %d bounds %v", got, img.Bounds())
		}
		got++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("frames = %d", got)
	}
}

type byteReader struct{ b []byte }

func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	p[0] = r.b[0]
	r.b = r.b[1:]
	return 1, nil
}
