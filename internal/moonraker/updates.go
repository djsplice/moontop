package moonraker

import (
	"encoding/json"
	"strings"
)

// UpdateStatus is machine.update.status: busy, and version_info by
// component name (system, moonraker, klipper, mainsail, …). The entries
// differ by type (git_repo, web, zip, system) and Moonraker version, so
// they stay maps.
func (c *Client) UpdateStatus() (map[string]any, error) {
	return c.callMap("machine.update.status", map[string]any{"refresh": false})
}

// UpdateRefresh checks for new versions (all components, or one).
func (c *Client) UpdateRefresh(name string) error {
	params := map[string]any{}
	if name != "" {
		params["name"] = name
	}
	_, err := c.call("machine.update.refresh", params)
	return err
}

// Upgrade updates one component. machine.update.upgrade is current
// Moonraker's endpoint; older ones had one per kind, used as a fallback.
func (c *Client) Upgrade(name string) error {
	_, err := c.call("machine.update.upgrade", map[string]any{"name": name})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
		return err
	}
	switch name {
	case "klipper", "moonraker", "system":
		_, err = c.call("machine.update."+name, nil)
	default:
		_, err = c.call("machine.update.client", map[string]any{"name": name})
	}
	return err
}

// Announcement is one entry of server.announcements.list.
type Announcement struct {
	EntryID     string  `json:"entry_id"`
	URL         string  `json:"url"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    string  `json:"priority"` // normal, high
	Date        float64 `json:"date"`
	Dismissed   bool    `json:"dismissed"`
	Source      string  `json:"source"`
	Feed        string  `json:"feed"`
}

// Announcements lists the undismissed announcements.
func (c *Client) Announcements() ([]Announcement, error) {
	res, err := c.call("server.announcements.list", map[string]any{"include_dismissed": false})
	if err != nil {
		return nil, err
	}
	var body struct {
		Entries []Announcement `json:"entries"`
	}
	err = json.Unmarshal(res, &body)
	return body.Entries, err
}

// DismissAnnouncement dismisses one entry.
func (c *Client) DismissAnnouncement(id string) error {
	_, err := c.call("server.announcements.dismiss", map[string]any{"entry_id": id})
	return err
}
