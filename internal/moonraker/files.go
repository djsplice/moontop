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
	return fmt.Sprintf("http://%s/server/files/%s/%s", c.host, root, strings.Join(segs, "/"))
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
	resp, err := httpClient.Do(req)
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
	resp, err := httpClient.Post(fmt.Sprintf("http://%s/server/files/upload", c.host), w.FormDataContentType(), &body)
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
