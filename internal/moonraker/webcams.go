package moonraker

import "encoding/json"

// Webcam is one entry from server.webcams.list.
type Webcam struct {
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Service     string `json:"service"`
	StreamURL   string `json:"stream_url"`
	SnapshotURL string `json:"snapshot_url"`
}

// Webcams lists the cameras Moonraker knows about.
func (c *Client) Webcams() ([]Webcam, error) {
	res, err := c.call("server.webcams.list", nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		Webcams []Webcam `json:"webcams"`
	}
	if err := json.Unmarshal(res, &body); err != nil {
		return nil, err
	}
	return body.Webcams, nil
}
