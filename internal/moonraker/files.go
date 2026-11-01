package moonraker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Moonraker serves file contents over plain HTTP (/server/files/…),
// not the websocket.

var httpClient = &http.Client{Timeout: 30 * time.Second}

func (c *Client) fileURL(root, path string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return fmt.Sprintf("%s/server/files/%s/%s", httpBase(c.host), root, strings.Join(segs, "/"))
}

// ListRoot lists the files in a root (gcodes, config, logs), recursively.
func (c *Client) ListRoot(root string) ([]FileInfo, error) {
	res, err := c.call("server.files.list", map[string]any{"root": root})
	if err != nil {
		return nil, err
	}
	var files []FileInfo
	err = json.Unmarshal(res, &files)
	return files, err
}

// FetchFile downloads a file; with tail > 0, only (about) its last tail
// bytes, by a Range request (falling back to cutting the whole file).
// It reports whether the start was cut off.
func (c *Client) FetchFile(root, path string, tail int) ([]byte, bool, error) {
	req, err := http.NewRequest(http.MethodGet, c.fileURL(root, path), nil)
	if err != nil {
		return nil, false, err
	}
	if tail > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=-%d", tail))
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, false, fmt.Errorf("%s: %s", path, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	cut := resp.StatusCode == http.StatusPartialContent
	if tail > 0 && len(data) > tail {
		data, cut = data[len(data)-tail:], true
	}
	return data, cut, nil
}

// UploadFile writes a file into a root (path may include folders).
func (c *Client) UploadFile(root, path string, data []byte) error {
	_, err := c.Upload(root, path, int64(len(data)), func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}, false)
	return err
}

// UploadLocal uploads a file from this computer, streaming it.
func (c *Client) UploadLocal(root, path, local string, print bool) (UploadResult, error) {
	fi, err := os.Stat(local)
	if err != nil {
		return UploadResult{}, err
	}
	if fi.IsDir() {
		return UploadResult{}, fmt.Errorf("%s is a folder", local)
	}
	return c.Upload(root, path, fi.Size(), func() (io.ReadCloser, error) { return os.Open(local) }, print)
}

// UploadResult is what Moonraker did with an upload.
type UploadResult struct {
	PrintStarted bool `json:"print_started"`
	PrintQueued  bool `json:"print_queued"`
}

// uploadClient has no overall timeout: a large file over slow Wi-Fi
// can take minutes. The dial and response-header waits still time out.
var uploadClient = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ResponseHeaderTimeout: 10 * time.Minute,
	TLSHandshakeTimeout:   15 * time.Second,
}}

// Upload streams size bytes from open() into root/path as a multipart
// form, with an exact Content-Length (some proxies refuse chunked
// uploads). print asks Moonraker to start printing it (gcodes only).
func (c *Client) Upload(root, path string, size int64, open func() (io.ReadCloser, error), print bool) (UploadResult, error) {
	var head, tail bytes.Buffer
	w := multipart.NewWriter(&head)
	dir, name := "", path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir, name = path[:i], path[i+1:]
	}
	_ = w.WriteField("root", root)
	if dir != "" {
		_ = w.WriteField("path", dir)
	}
	if print {
		_ = w.WriteField("print", "true")
	}
	if _, err := w.CreateFormFile("file", name); err != nil {
		return UploadResult{}, err
	}
	ct := w.FormDataContentType()
	headBytes := append([]byte(nil), head.Bytes()...)
	tw := multipart.NewWriter(&tail)
	_ = tw.SetBoundary(w.Boundary())
	_ = tw.Close() // "\r\n--boundary--\r\n"
	tailBytes := tail.Bytes()

	body := func() (io.ReadCloser, error) {
		f, err := open()
		if err != nil {
			return nil, err
		}
		r := io.MultiReader(bytes.NewReader(headBytes), io.LimitReader(f, size), bytes.NewReader(tailBytes))
		return struct {
			io.Reader
			io.Closer
		}{r, f}, nil
	}
	rc, err := body()
	if err != nil {
		return UploadResult{}, err
	}
	req, err := http.NewRequest(http.MethodPost, httpBase(c.host)+"/server/files/upload", rc)
	if err != nil {
		rc.Close()
		return UploadResult{}, err
	}
	req.ContentLength = int64(len(headBytes)) + size + int64(len(tailBytes))
	req.GetBody = body
	req.Header.Set("Content-Type", ct)
	resp, err := c.doWith(uploadClient, req)
	if err != nil {
		return UploadResult{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return UploadResult{}, fmt.Errorf("upload %s: %s", path, errorMessage(data, resp.Status))
	}
	var res UploadResult
	var env struct {
		Result *UploadResult `json:"result"`
	}
	if json.Unmarshal(data, &env) == nil && env.Result != nil {
		res = *env.Result
	} else {
		_ = json.Unmarshal(data, &res)
	}
	return res, nil
}

// LogRollover starts new log files (application "" = all).
func (c *Client) LogRollover(application string) error {
	params := map[string]any{}
	if application != "" {
		params["application"] = application
	}
	_, err := c.call("server.logs.rollover", params)
	return err
}

// QueryEndstops returns each endstop's state ("open" / "TRIGGERED").
func (c *Client) QueryEndstops() (map[string]string, error) {
	res, err := c.call("printer.query_endstops.status", nil)
	if err != nil {
		return nil, err
	}
	var out map[string]string
	err = json.Unmarshal(res, &out)
	return out, err
}

// ---------------------------------------------------------------------------
// File manager: directories, metadata, create / move / copy / delete.
// Paths include the root ("gcodes/sub/part.gcode").
// ---------------------------------------------------------------------------

// FileMetadata is what Moonraker parsed from a gcode file's slicer
// comments. Fields the slicer didn't write are zero. Slicers disagree on
// types — some write "0.2" for a layer height, or a list for filament
// type — so every field decodes leniently.
type FileMetadata struct {
	Slicer              Text `json:"slicer"`
	SlicerVersion       Text `json:"slicer_version"`
	EstimatedTime       Num  `json:"estimated_time"`
	LayerHeight         Num  `json:"layer_height"`
	FirstLayerHeight    Num  `json:"first_layer_height"`
	ObjectHeight        Num  `json:"object_height"`
	NozzleDiameter      Num  `json:"nozzle_diameter"`
	FilamentTotal       Num  `json:"filament_total"` // mm
	FilamentWeightTotal Num  `json:"filament_weight_total"`
	FilamentType        Text `json:"filament_type"`
	FilamentName        Text `json:"filament_name"`
	FirstLayerExtrTemp  Num  `json:"first_layer_extr_temp"`
	FirstLayerBedTemp   Num  `json:"first_layer_bed_temp"`
	PrintStartTime      Num  `json:"print_start_time"`
}

// Num is a number that may arrive as a JSON number, a numeric string,
// a list (the first number counts) or null; anything else is 0.
type Num float64

func (n *Num) UnmarshalJSON(b []byte) error {
	*n = 0
	var f float64
	if json.Unmarshal(b, &f) == nil {
		*n = Num(f)
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		s = strings.TrimSpace(strings.SplitN(strings.NewReplacer(",", ";").Replace(s), ";", 2)[0])
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			*n = Num(v)
		}
		return nil
	}
	var list []json.RawMessage
	if json.Unmarshal(b, &list) == nil && len(list) > 0 {
		return n.UnmarshalJSON(list[0])
	}
	return nil
}

// Text is a string that may arrive as a string, number, list (joined
// with ", ") or null.
type Text string

func (t *Text) UnmarshalJSON(b []byte) error {
	*t = ""
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = Text(s)
		return nil
	}
	var list []json.RawMessage
	if json.Unmarshal(b, &list) == nil {
		var parts []string
		for _, r := range list {
			var x Text
			_ = x.UnmarshalJSON(r)
			if x != "" {
				parts = append(parts, string(x))
			}
		}
		*t = Text(strings.Join(parts, ", "))
		return nil
	}
	if string(b) != "null" {
		*t = Text(strings.Trim(string(b), `"`))
	}
	return nil
}

// DirFile is a file in a directory listing, with its metadata when the
// listing was extended and Moonraker has parsed it.
type DirFile struct {
	Name     string  `json:"filename"`
	Modified float64 `json:"modified"`
	Size     int64   `json:"size"`
	FileMetadata
}

// DirEntry is a subdirectory in a listing.
type DirEntry struct {
	Name     string  `json:"dirname"`
	Modified float64 `json:"modified"`
	Size     int64   `json:"size"`
}

// DiskUsage is the root's filesystem, in bytes (floats: some
// Moonraker versions send them that way).
type DiskUsage struct {
	Total float64 `json:"total"`
	Used  float64 `json:"used"`
	Free  float64 `json:"free"`
}

// Directory is one level of a root.
type Directory struct {
	Dirs  []DirEntry `json:"dirs"`
	Files []DirFile  `json:"files"`
	Disk  DiskUsage  `json:"disk_usage"`
}

// GetDirectory lists one directory, with gcode metadata.
func (c *Client) GetDirectory(path string) (Directory, error) {
	var d Directory
	res, err := c.call("server.files.get_directory", map[string]any{"path": path, "extended": true})
	if err != nil {
		return d, err
	}
	err = json.Unmarshal(res, &d)
	return d, err
}

// CreateDirectory makes a directory; its parent must exist.
func (c *Client) CreateDirectory(path string) error {
	_, err := c.call("server.files.post_directory", map[string]any{"path": path})
	return err
}

// DeleteDirectory removes a directory; force removes its contents too.
func (c *Client) DeleteDirectory(path string, force bool) error {
	_, err := c.call("server.files.delete_directory", map[string]any{"path": path, "force": force})
	return err
}

// DeleteFile removes a file.
func (c *Client) DeleteFile(path string) error {
	_, err := c.call("server.files.delete_file", map[string]any{"path": path})
	return err
}

// MoveFile moves or renames a file or directory.
func (c *Client) MoveFile(source, dest string) error {
	_, err := c.call("server.files.move", map[string]any{"source": source, "dest": dest})
	return err
}

// CopyFile copies a file or directory.
func (c *Client) CopyFile(source, dest string) error {
	_, err := c.call("server.files.copy", map[string]any{"source": source, "dest": dest})
	return err
}

// FileListChange is one notify_filelist_changed.
type FileListChange struct {
	Action string `json:"action"` // create_file, delete_file, move_file, modify_file, create_dir, delete_dir, move_dir, root_update
	Item   struct {
		Root string `json:"root"`
		Path string `json:"path"`
	} `json:"item"`
	SourceItem struct {
		Root string `json:"root"`
		Path string `json:"path"`
	} `json:"source_item"`
}

// ParseFileListChange decodes notify_filelist_changed params.
func ParseFileListChange(params []json.RawMessage) (FileListChange, bool) {
	var ch FileListChange
	if len(params) == 0 || json.Unmarshal(params[0], &ch) != nil {
		return ch, false
	}
	return ch, true
}
