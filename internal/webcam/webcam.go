// Package webcam turns a Moonraker webcam entry into a picture for the
// dashboard. The live picture is the camera's H.264 stream; a JPEG
// snapshot is the fallback. Block cells draw it anywhere. A Kitty
// graphics placement draws the same frame sharp where the terminal allows.
package webcam

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Cam is one webcam from server.webcams.list, reduced to the fields
// the dashboard uses to choose and draw it.
type Cam struct {
	Name        string
	Service     string
	StreamURL   string
	SnapshotURL string
	Enabled     bool
}

// Pick chooses the camera to show. A WebRTC camera wins, because that
// is the stream the printer's player page uses. Anything else enabled
// and with a snapshot is the fallback.
func Pick(cams []Cam) (Cam, bool) {
	var fallback Cam
	have := false
	for _, c := range cams {
		if !c.Enabled || strings.TrimSpace(c.SnapshotURL) == "" {
			continue
		}
		if isWebRTC(c) {
			return c, true
		}
		if !have {
			fallback, have = c, true
		}
	}
	return fallback, have
}

func isWebRTC(c Cam) bool {
	s := strings.ToLower(c.Service + " " + c.StreamURL)
	return strings.Contains(s, "webrtc")
}

// Resolve makes a snapshot URL absolute. Relative paths on a plain
// http Moonraker at port 7125 are served by the printer's port-80
// front end (nginx), not by Moonraker itself. An absolute snapshot
// URL is kept as given. https keeps its host and port.
func Resolve(moonrakerHost, snapshot string) string {
	snapshot = strings.TrimSpace(snapshot)
	if snapshot == "" {
		return ""
	}
	if strings.Contains(snapshot, "://") {
		return snapshot
	}
	if !strings.HasPrefix(snapshot, "/") {
		snapshot = "/" + snapshot
	}
	addr := moonrakerHost
	scheme := "http"
	switch {
	case strings.HasPrefix(addr, "https://"):
		scheme = "https"
		addr = strings.TrimPrefix(addr, "https://")
	case strings.HasPrefix(addr, "http://"):
		addr = strings.TrimPrefix(addr, "http://")
	}
	host := addr
	if h, port, err := net.SplitHostPort(addr); err == nil && scheme == "http" && port == "7125" {
		host = h
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	}
	return scheme + "://" + host + snapshot
}

// Fetch downloads one JPEG snapshot.
func Fetch(ctx context.Context, url string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	img, err := jpeg.Decode(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return img, nil
}

// Draw scales img into cols by rows terminal cells. Each cell is the
// upper-half block, so the picture is cols by rows*2 pixels.
func Draw(img image.Image, cols, rows int) string {
	if img == nil || cols < 1 || rows < 1 {
		return ""
	}
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)
	pw, ph := cols, rows*2
	var bld strings.Builder
	for y := 0; y < rows; y++ {
		if y > 0 {
			bld.WriteByte('\n')
		}
		for x := 0; x < cols; x++ {
			top := average(rgba, b, x, y*2, pw, ph)
			bot := average(rgba, b, x, y*2+1, pw, ph)
			fmt.Fprintf(&bld, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", top.R, top.G, top.B, bot.R, bot.G, bot.B)
		}
		bld.WriteString("\x1b[0m")
	}
	return bld.String()
}

// average is the mean colour of the source rectangle that maps to
// pixel (x, y) of a pw by ph picture.
func average(img *image.RGBA, b image.Rectangle, x, y, pw, ph int) color.RGBA {
	sw, sh := b.Dx(), b.Dy()
	x0 := b.Min.X + x*sw/pw
	x1 := b.Min.X + (x+1)*sw/pw
	y0 := b.Min.Y + y*sh/ph
	y1 := b.Min.Y + (y+1)*sh/ph
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	var r, g, bl, n int
	for py := y0; py < y1; py++ {
		row := img.Pix[img.PixOffset(x0, py):]
		for px := x0; px < x1; px++ {
			r += int(row[0])
			g += int(row[1])
			bl += int(row[2])
			n++
			row = row[4:]
		}
	}
	if n == 0 {
		return color.RGBA{}
	}
	return color.RGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(bl / n), A: 255}
}

// quadGlyph is the 2×2 block for a bit mask. Bit 0 is the top left,
// then top right, bottom left, bottom right. A set bit is foreground.
var quadGlyph = [16]rune{
	' ', '▘', '▝', '▀',
	'▖', '▌', '▞', '▛',
	'▗', '▚', '▐', '▜',
	'▄', '▙', '▟', '█',
}

// DrawQuad scales img into cols by rows cells with four pixels per
// cell. Each cell still has only two colors; the glyph picks which
// quadrants use the foreground. It walks the same pixels Draw does.
func DrawQuad(img image.Image, cols, rows int) string {
	if img == nil || cols < 1 || rows < 1 {
		return ""
	}
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)
	pw, ph := cols*2, rows*2
	var bld strings.Builder
	var sample [4]color.RGBA
	for y := 0; y < rows; y++ {
		if y > 0 {
			bld.WriteByte('\n')
		}
		for x := 0; x < cols; x++ {
			sample[0] = average(rgba, b, x*2, y*2, pw, ph)
			sample[1] = average(rgba, b, x*2+1, y*2, pw, ph)
			sample[2] = average(rgba, b, x*2, y*2+1, pw, ph)
			sample[3] = average(rgba, b, x*2+1, y*2+1, pw, ph)
			glyph, fg, bg := quadCell(sample)
			fmt.Fprintf(&bld, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm%c", fg.R, fg.G, fg.B, bg.R, bg.G, bg.B, glyph)
		}
		bld.WriteString("\x1b[0m")
	}
	return bld.String()
}

// quadCell picks the two colors and the glyph for four samples.
// A nearly flat cell is a space in the average color, so a smooth
// surface does not flicker between quadrant shapes.
func quadCell(s [4]color.RGBA) (glyph rune, fg, bg color.RGBA) {
	spread := 0
	for i := 1; i < 4; i++ {
		if d := colorDist(s[0], s[i]); d > spread {
			spread = d
		}
	}
	if spread < 18*18 {
		return ' ', s[0], s[0]
	}
	sum := 0
	var lum [4]int
	for i, p := range s {
		lum[i] = int(p.R)*3 + int(p.G)*6 + int(p.B)
		sum += lum[i]
	}
	mean := sum / 4
	var hiR, hiG, hiB, hiN, loR, loG, loB, loN int
	for i, p := range s {
		if lum[i] >= mean {
			hiR += int(p.R)
			hiG += int(p.G)
			hiB += int(p.B)
			hiN++
			continue
		}
		loR += int(p.R)
		loG += int(p.G)
		loB += int(p.B)
		loN++
	}
	if hiN == 0 || loN == 0 {
		return ' ', s[0], s[0]
	}
	fg = color.RGBA{R: uint8(hiR / hiN), G: uint8(hiG / hiN), B: uint8(hiB / hiN), A: 255}
	bg = color.RGBA{R: uint8(loR / loN), G: uint8(loG / loN), B: uint8(loB / loN), A: 255}
	mask := 0
	for i, p := range s {
		if colorDist(p, fg) <= colorDist(p, bg) {
			mask |= 1 << i
		}
	}
	return quadGlyph[mask], fg, bg
}

func colorDist(a, b color.RGBA) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	return dr*dr + dg*dg + db*db
}
