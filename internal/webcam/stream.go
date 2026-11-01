package webcam

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// H264Path is the raw Annex-B stream the printer's /webcam/player page
// reads. A webrtc-camerastreamer page and its snapshot.jpg both sit
// beside stream.h264. Other cameras (an MJPEG action=stream, for one)
// return an empty path so the caller keeps using snapshots.
func H264Path(streamURL, snapshotURL string) string {
	if p := h264Sibling(streamURL); p != "" {
		return p
	}
	return h264Sibling(snapshotURL)
}

func h264Sibling(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	path := raw
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	low := strings.ToLower(path)
	switch {
	case strings.HasSuffix(low, "/stream.h264"):
		return raw
	case strings.HasSuffix(low, "/webrtc"), strings.HasSuffix(low, "/snapshot.jpg"):
		i := strings.LastIndex(path, "/")
		return path[:i+1] + "stream.h264"
	default:
		return ""
	}
}

// Frame widths for the JPEG pipe. Wide is the sharp picture. Narrow
// and small cut the PNG encode and the work the terminal does to draw it.
const (
	FrameWide   = 960
	FrameNarrow = 640
	FrameSmall  = 480
)

// ffmpegArgs is the low-latency decode of a live elementary stream.
// Probesize is large on purpose: the feed is Annex-B and a client that
// joins mid-GOP does not see SPS/PPS until the next keyframe. A tiny
// probe makes ffmpeg drop those first pictures and sometimes exit.
// width is FrameWide, FrameNarrow, or FrameSmall; anything else uses FrameWide.
func ffmpegArgs(rawURL string, width int) ([]string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || strings.HasPrefix(rawURL, "-") {
		return nil, fmt.Errorf("stream url must be http or https")
	}
	if width != FrameWide && width != FrameNarrow && width != FrameSmall {
		width = FrameWide
	}
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-fflags", "nobuffer+discardcorrupt",
		"-flags", "low_delay",
		"-probesize", "500000",
		"-analyzeduration", "500000",
		"-i", rawURL,
		"-an",
		"-vf", fmt.Sprintf("fps=12,scale=%d:-2", width),
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"-q:v", "8",
		"pipe:1",
	}, nil
}

// Play runs ffmpeg on the H.264 URL and calls yield with each decoded
// frame. yield runs on the Play goroutine; a non-nil error from it
// stops the decode. A cancelled ctx kills ffmpeg and returns ctx.Err.
// Frames that are not valid JPEG are skipped, because the first
// packets after joining the stream often are.
func Play(ctx context.Context, rawURL string, width int, yield func(image.Image) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	args, err := ffmpegArgs(rawURL, width)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return errors.New("ffmpeg not found")
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.WaitDelay = 250 * time.Millisecond
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var tail errTail
	cmd.Stderr = &tail
	if err := cmd.Start(); err != nil {
		return err
	}
	readErr := splitJPEG(stdout, func(frame []byte) error {
		img, decErr := jpeg.Decode(bytes.NewReader(frame))
		if decErr != nil {
			return nil
		}
		return yield(img)
	})
	if ctx.Err() != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if readErr != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		msg := strings.TrimSpace(tail.String())
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		if msg == "" {
			return fmt.Errorf("ffmpeg: %w", waitErr)
		}
		return fmt.Errorf("ffmpeg: %s", msg)
	}
	return nil
}

const maxJPEG = 12 << 20

// splitJPEG reads a concatenated JPEG byte stream (ffmpeg image2pipe)
// and calls yield with each SOI..EOI frame. The slice is only valid
// for the duration of the call.
func splitJPEG(r io.Reader, yield func([]byte) error) error {
	buf := make([]byte, 0, 64<<10)
	tmp := make([]byte, 32<<10)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			var yerr error
			buf, yerr = takeJPEGs(buf, yield)
			if yerr != nil {
				return yerr
			}
			if len(buf) > maxJPEG {
				if buf[len(buf)-1] == 0xFF {
					buf = append([]byte(nil), buf[len(buf)-1])
				} else {
					buf = buf[:0]
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func takeJPEGs(buf []byte, yield func([]byte) error) ([]byte, error) {
	for {
		start := findMarker(buf, 0xD8)
		if start < 0 {
			if len(buf) > 0 && buf[len(buf)-1] == 0xFF {
				return buf[len(buf)-1:], nil
			}
			return nil, nil
		}
		buf = buf[start:]
		if len(buf) < 4 {
			return buf, nil
		}
		end := findMarker(buf[2:], 0xD9)
		if end < 0 {
			return buf, nil
		}
		end += 2
		frame := buf[:end+2]
		if err := yield(frame); err != nil {
			return nil, err
		}
		buf = buf[end+2:]
	}
}

func findMarker(b []byte, second byte) int {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == 0xFF && b[i+1] == second {
			return i
		}
	}
	return -1
}

// errTail keeps the tail of ffmpeg's stderr for the error we show
// when the process gives up. The process copier writes it while Play
// reads it only after Wait returns.
type errTail struct {
	mu sync.Mutex
	b  []byte
}

func (e *errTail) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.b = append(e.b, p...)
	if len(e.b) > 2048 {
		e.b = append([]byte(nil), e.b[len(e.b)-2048:]...)
	}
	return len(p), nil
}

func (e *errTail) String() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return string(e.b)
}
