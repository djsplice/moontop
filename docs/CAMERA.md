# Camera picture

Pane 5 (`Ctrl+T`) is either the gcode console or the printer's webcam. This is how a frame gets from Moonraker to a terminal cell. The dashboard does not open a WebRTC session, and the decoder is `ffmpeg` on `PATH` rather than cgo.

## Choosing a camera

`server.webcams.list` supplies the cameras. An enabled WebRTC camera wins. Anything else enabled, with a snapshot URL, is the fallback.

The WebRTC URL is the camera's HTML player, not the bytes we decode. A player page or a `snapshot.jpg` has a sibling `stream.h264` (raw Annex-B). That is the live picture. An MJPEG camera (`?action=stream`) has no such sibling, so the pane stays on snapshots.

Relative paths on a plain `http` Moonraker at port 7125 are rewritten to the printer's port 80 front end. `https` keeps its host and port. An absolute URL is kept as given.

## Decode

`ffmpeg` reads the H.264 URL and writes a JPEG pipe. The probe is large on purpose: a client that joins mid-GOP does not see SPS/PPS until the next keyframe, and a tiny probe makes ffmpeg drop those first pictures. The filter is `fps=12,scale=WIDTH:-2`. Width is 960, 640, or 480. The JPEG quality is `q:v 8`. Only `http` and `https` URLs are accepted.

Each JPEG is cut out of the pipe on its SOI/EOI markers. A chunk that is not a valid JPEG is skipped. The first moments after joining often log `non-existing PPS` and `no frame!` on ffmpeg's stderr. Those lines do not stop the stream.

If `ffmpeg` is missing, or the stream ends, the pane polls the snapshot JPEG every 250ms.

Changing width kills ffmpeg and starts another. A generation counter makes the old reader's close look stale, so it cannot be mistaken for the new stream dying. The last frame stays on screen until the next picture arrives, which waits on a keyframe.

## Quality ladder

`b` cycles the picture while the camera is showing. On the console, `b` is just a letter.

| Rung | What you see | Decode width |
| --- | --- | --- |
| sharp 960 | Kitty PNG, title unchanged | 960 |
| sharp 640 | Kitty PNG, title `640` | 640 |
| sharp 480 | Kitty PNG, title `480` | 480 |
| quadrant blocks | four samples per cell, title `quad` | 480 |
| half blocks | two samples per cell, title `half` | 480 |

960 is the default. Quadrant and half stay on the 480 pipe, so stepping from 480 into blocks does not start another ffmpeg. A terminal that cannot place a Kitty image skips 640 and 480. There the default is quadrant blocks, then half blocks.

Block rungs also stop the PNG encode. The flag lives on the heap: Bubble Tea copies the model on every update, and the ffmpeg goroutine has to see the same flag.

## How a frame is drawn

A sharp frame is encoded as a PNG (`BestSpeed`) and placed with the Kitty graphics protocol. One image id is reused, so each frame replaces the last. The sequence transmits and displays (`a=T`), says the payload is a PNG (`f=100`), leaves the cursor where it is (`C=1`), and paints above the text (`z=1`). The base64 is split into 4096-character chunks with no newlines, so a panel splitter that cuts on newlines cannot divide a command.

Unicode placeholders are not used. U+10EEEE is two columns wide in the width tables this layout uses, and a placeholder grid wraps the pane.

The placement is deleted when this view will not redraw it (the console, help, and the full-screen tools). Otherwise the photo stays painted on top of whatever comes next.

Ghostty, Kitty, and WezTerm are detected from `TERM_PROGRAM`, `KITTY_WINDOW_ID`, `WEZTERM_PANE`, or a `TERM` that contains `kitty` or `ghostty`. Outside tmux that is enough. Inside tmux (`TMUX` is set), the sequence is used only when `tmux show-option -gv allow-passthrough` reports `on` or `all`. A failed query is treated as blocked.

Under the PNG, the pane still draws half blocks. A terminal that ignores the placement shows that picture. Sharp mode pays for both.

Quadrant blocks sample a 2×2 patch per cell and pick two colors. The glyph is one of the sixteen block characters (` `, `▘`, `▝`, `▀`, and the rest). A cell whose samples are close is a space in one color, so a smooth area does not flicker between shapes.

Half blocks sample a 1×2 patch per cell. The top half is the foreground, the bottom half is the background, and the glyph is `▀`.

Both modes average the source rectangle that maps onto each sample.

## What the CPU is doing

These are casual readings from one Ghostty window, with moontop not running inside tmux:

| Rung | CPU | Pixels vs 960 |
| --- | --- | --- |
| 960 | 91% | 100% |
| 640 | 54% | 44% |
| 480 | 40% | 25% |
| quadrant blocks | 23% | cell grid |
| half blocks | 19% | cell grid |

The sharp rungs fit a floor near 20% plus work that follows the pixel count. That floor is ffmpeg decoding the camera's full frame, plus drawing the pane. Quadrant blocks sit a few points above half blocks because each cell samples four patches and picks a glyph. The block rungs do not encode a PNG. A still smaller sharp width would save less: more of the remaining time is that floor.

## Where the code is

| Piece | File |
| --- | --- |
| Camera list | `internal/moonraker/webcams.go` |
| Pick, URL rewrite, snapshot, block drawing | `internal/webcam/webcam.go` |
| H.264 path, ffmpeg, JPEG split | `internal/webcam/stream.go` |
| Kitty placement and terminal detection | `internal/webcam/kitty.go` |
| Pane toggle, stream, quality ladder, paint | `cmd/moontop/video.go` |
| `Ctrl+T` and `b` | `cmd/moontop/keys.go`, `cmd/moontop/help.go` |
