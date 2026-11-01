package moonraker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
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
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	dir, name := "", path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir, name = path[:i], path[i+1:]
	}
	_ = w.WriteField("root", root)
	if dir != "" {
		_ = w.WriteField("path", dir)
	}
	fw, err := w.CreateFormFile("file", name)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, httpBase(c.host)+"/server/files/upload", bytes.NewReader(body.Bytes()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("upload %s: %s %s", path, resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
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
// comments. Fields the slicer didn't write are zero.
type FileMetadata struct {
	Slicer              string   `json:"slicer"`
	SlicerVersion       string   `json:"slicer_version"`
	EstimatedTime       float64  `json:"estimated_time"`
	LayerHeight         float64  `json:"layer_height"`
	FirstLayerHeight    float64  `json:"first_layer_height"`
	ObjectHeight        float64  `json:"object_height"`
	NozzleDiameter      float64  `json:"nozzle_diameter"`
	FilamentTotal       float64  `json:"filament_total"` // mm
	FilamentWeightTotal float64  `json:"filament_weight_total"`
	FilamentType        string   `json:"filament_type"`
	FilamentName        string   `json:"filament_name"`
	FirstLayerExtrTemp  float64  `json:"first_layer_extr_temp"`
	FirstLayerBedTemp   float64  `json:"first_layer_bed_temp"`
	PrintStartTime      *float64 `json:"print_start_time"`
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
