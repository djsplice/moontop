package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinh-rahb/moontop/internal/webcam"
)

func TestCameraToggleSwapsConsolePane(t *testing.T) {
	srv := fakePrinter(t)
	m := readyModel(t, srv)
	m.setFocus(focusConsole)
	m.input.SetValue("G28")

	m, cmd := updateCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if cmd == nil || !m.showVideo || m.focus != focusConsole {
		t.Fatalf("toggle on: show=%v focus=%v cmd=%v", m.showVideo, m.focus, cmd != nil)
	}
	view := m.View()
	if !strings.Contains(view, "Camera") || !strings.Contains(view, "ctrl+t console") || !strings.Contains(view, "looking up the webcam") {
		t.Fatalf("camera view:\n%s", view)
	}
	if strings.Contains(view, "G28") {
		t.Fatal("console draft still drawn while the camera is up")
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.input.Value() != "G28" {
		t.Fatalf("camera ate a key into the console input: %q", m.input.Value())
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.showVideo || !strings.Contains(m.View(), "5 Console") || !strings.Contains(m.View(), "G28") {
		t.Fatalf("toggle off: show=%v view has console draft=%v", m.showVideo, strings.Contains(m.View(), "G28"))
	}
}

func TestRenderPanelKeepsCameraGraphics(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	const cols, rows = 12, 3
	body := webcam.PlaceKitty(buf.Bytes(), cols, rows) + webcam.Draw(img, cols, rows)
	if body == "" || !strings.Contains(body, "\x1b_Ga=T") {
		t.Fatal("placement missing")
	}
	outerW := cols + panelInnerXReserve()
	outerH := rows + 3 // title, body, two borders
	panel := renderPanel("5 case", body, true, outerW, outerH)
	if w := lipgloss.Width(panel); w != outerW {
		t.Fatalf("panel width %d, want %d", w, outerW)
	}
	if lipgloss.Height(panel) != outerH {
		t.Fatalf("panel height %d, want %d", lipgloss.Height(panel), outerH)
	}
	if strings.Count(panel, "▀") != cols*rows {
		t.Fatalf("half-blocks = %d, want %d", strings.Count(panel, "▀"), cols*rows)
	}
	if !strings.Contains(panel, "\x1b_Ga=T") {
		t.Fatal("graphics sequence was stripped")
	}
}

func TestCameraDetailCycle(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	red := color.RGBA{R: 255, A: 255}
	black := color.RGBA{A: 255}
	img.Set(0, 0, red)
	img.Set(1, 0, black)
	img.Set(0, 1, black)
	img.Set(1, 1, black)
	m := model{showVideo: true, camSharp: true, camImg: img}
	m.pngEncode().Store(true)
	m.paintCamera()
	if !strings.Contains(m.camPaint, "\x1b_Ga=T") {
		t.Fatal("auto should be the sharp picture")
	}
	if m.streamWidth() != 960 || m.detailLabel() != "" {
		t.Fatalf("auto width %d label %q", m.streamWidth(), m.detailLabel())
	}
	m.cycleCameraDetail()
	if m.camDetail != camDetail640 || m.streamWidth() != 640 || m.detailLabel() != "640" || !m.camEncode.Load() || !strings.Contains(m.camPaint, "\x1b_Ga=T") {
		t.Fatalf("640 detail=%d encode=%v label=%q", m.camDetail, m.camEncode.Load(), m.detailLabel())
	}
	m.cycleCameraDetail()
	if m.camDetail != camDetail480 || m.streamWidth() != 480 || m.detailLabel() != "480" || !m.camEncode.Load() || !strings.Contains(m.camPaint, "\x1b_Ga=T") {
		t.Fatalf("480 detail=%d encode=%v label=%q", m.camDetail, m.camEncode.Load(), m.detailLabel())
	}
	m.cycleCameraDetail()
	if m.streamWidth() != 480 || m.camEncode.Load() || strings.Contains(m.camPaint, "\x1b_Ga=T") || !strings.Contains(m.camPaint, "▘") {
		t.Fatalf("quad encode=%v paint=%q", m.camEncode.Load(), m.camPaint)
	}
	m.cycleCameraDetail()
	if m.streamWidth() != 480 || strings.Contains(m.camPaint, "▘") || !strings.Contains(m.camPaint, "▀") {
		t.Fatalf("half paint=%q", m.camPaint)
	}
	m.cycleCameraDetail()
	if m.camDetail != camDetailAuto || m.streamWidth() != 960 || !m.camEncode.Load() || !strings.Contains(m.camPaint, "\x1b_Ga=T") {
		t.Fatal("cycle did not return to sharp")
	}

	plain := model{showVideo: true, camImg: img}
	plain.cycleCameraDetail()
	if plain.camDetail != camDetailQuad || plain.streamWidth() != 960 {
		t.Fatalf("block terminal landed on %d width %d", plain.camDetail, plain.streamWidth())
	}
}
