package moonraker

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Typed API methods
// ---------------------------------------------------------------------------

// ListObjects returns the names of all printer objects registered in
// Klipper. Used to discover available heaters/sensors dynamically.
func (c *Client) ListObjects() ([]string, error) {
	result, err := c.call("printer.objects.list", nil)
	if err != nil {
		return nil, err
	}

	var body struct {
		Objects []string `json:"objects"`
	}
	if err := json.Unmarshal(result, &body); err != nil {
		return nil, err
	}

	return body.Objects, nil
}

// Subscribe subscribes to Moonraker printer object status updates.
//
// objects maps printer-object names to the list of fields to subscribe
// to. Use nil (or an empty slice) as the value to subscribe to all
// fields for that object.
//
// Moonraker treats each subscribe request as a replacement for the
// previous one on the same connection, so callers must always send
// the complete object map. Subscriptions are lost on reconnect and on
// Klippy restart; the caller re-subscribes (see Client doc comment).
//
// Example:
//
//	err := client.Subscribe(map[string][]string{
//	    "extruder":   {"temperature", "target"},
//	    "heater_bed": nil,  // all fields
//	})
//
// The initial state snapshot is returned as a StatusUpdate.
func (c *Client) Subscribe(objects map[string][]string) (*StatusUpdate, error) {
	// Moonraker expects null (not an empty array) to mean "all fields",
	// so we convert []string{} / nil → JSON null.
	mapped := make(map[string]any, len(objects))
	for k, v := range objects {
		if len(v) == 0 {
			mapped[k] = nil
		} else {
			mapped[k] = v
		}
	}

	params := map[string]any{
		"objects": mapped,
	}

	result, err := c.call("printer.objects.subscribe", params)
	if err != nil {
		return nil, err
	}

	// The response contains {"eventtime": float, "status": {...}}.
	var body struct {
		EventTime float64                   `json:"eventtime"`
		Status    map[string]map[string]any `json:"status"`
	}
	if err := json.Unmarshal(result, &body); err != nil {
		return nil, err
	}

	return &StatusUpdate{
		Objects:   body.Status,
		Timestamp: body.EventTime,
	}, nil
}

// QueryObjects queries the current state of the specified printer
// objects without subscribing to updates.
//
// See Subscribe for the objects parameter format.
func (c *Client) QueryObjects(objects map[string][]string) (*StatusUpdate, error) {
	mapped := make(map[string]any, len(objects))
	for k, v := range objects {
		if len(v) == 0 {
			mapped[k] = nil
		} else {
			mapped[k] = v
		}
	}

	params := map[string]any{
		"objects": mapped,
	}

	result, err := c.call("printer.objects.query", params)
	if err != nil {
		return nil, err
	}

	var body struct {
		EventTime float64                   `json:"eventtime"`
		Status    map[string]map[string]any `json:"status"`
	}
	if err := json.Unmarshal(result, &body); err != nil {
		return nil, err
	}

	return &StatusUpdate{
		Objects:   body.Status,
		Timestamp: body.EventTime,
	}, nil
}

// GcodeScript sends a G-Code script to Klipper for execution.
//
// Example:
//
//	err := client.GcodeScript("G28")           // home all axes
//	err := client.GcodeScript("M104 S200")     // set extruder temp
func (c *Client) GcodeScript(script string) error {
	params := map[string]any{
		"script": script,
	}

	_, err := c.call("printer.gcode.script", params)
	return err
}

// EmergencyStop sends an emergency stop command to Klipper (M112).
// This transitions the printer to a "shutdown" state immediately.
func (c *Client) EmergencyStop() error {
	_, err := c.call("printer.emergency_stop", nil)
	return err
}

// FileInfo describes a single gcode file from server.files.list.
type FileInfo struct {
	Path         string  `json:"path"`
	Size         int64   `json:"size"`
	ModifiedTime float64 `json:"modified"`
}

// ListFiles returns all gcode files in the "gcodes" root, recursively.
func (c *Client) ListFiles() ([]FileInfo, error) {
	params := map[string]any{"root": "gcodes"}
	result, err := c.call("server.files.list", params)
	if err != nil {
		return nil, err
	}
	var files []FileInfo
	if err := json.Unmarshal(result, &files); err != nil {
		return nil, err
	}
	return files, nil
}

// StartPrint queues and begins a print of the named gcode file
// (path relative to the gcodes root).
func (c *Client) StartPrint(filename string) error {
	params := map[string]any{"filename": filename}
	_, err := c.call("printer.print.start", params)
	return err
}

// PausePrint pauses the currently running print.
func (c *Client) PausePrint() error {
	_, err := c.call("printer.print.pause", nil)
	return err
}

// ResumePrint resumes a paused print.
func (c *Client) ResumePrint() error {
	_, err := c.call("printer.print.resume", nil)
	return err
}

// CancelPrint cancels the currently running or paused print.
func (c *Client) CancelPrint() error {
	_, err := c.call("printer.print.cancel", nil)
	return err
}

// SetVelocityLimit issues a SET_VELOCITY_LIMIT gcode with whichever
// subset of parameters the caller passes as non-nil. Returns an error
// if every argument is nil — that would otherwise send a no-op gcode.
func (c *Client) SetVelocityLimit(velocity, accel, squareCornerVelocity, minCruiseRatio *float64) error {
	parts := []string{"SET_VELOCITY_LIMIT"}
	if velocity != nil {
		parts = append(parts, fmt.Sprintf("VELOCITY=%g", *velocity))
	}
	if accel != nil {
		parts = append(parts, fmt.Sprintf("ACCEL=%g", *accel))
	}
	if squareCornerVelocity != nil {
		parts = append(parts, fmt.Sprintf("SQUARE_CORNER_VELOCITY=%g", *squareCornerVelocity))
	}
	if minCruiseRatio != nil {
		parts = append(parts, fmt.Sprintf("MIN_CRUISE_RATIO=%g", *minCruiseRatio))
	}
	if len(parts) == 1 {
		return fmt.Errorf("set velocity limit: no parameters to set")
	}
	return c.GcodeScript(strings.Join(parts, " "))
}

// ---------------------------------------------------------------------------
// Connection identity and Klippy lifecycle
// ---------------------------------------------------------------------------

// Identify registers this websocket connection with Moonraker
// (server.connection.identify) so it shows up by name in Moonraker's
// connection list. clientType is one of Moonraker's accepted types:
// web, mobile, desktop, display, bot, agent, other.
func (c *Client) Identify(name, version, clientType, url string) error {
	params := map[string]any{
		"client_name": name,
		"version":     version,
		"type":        clientType,
		"url":         url,
	}
	_, err := c.call("server.connection.identify", params)
	return err
}

// ServerInfo is the subset of server.info used to decide whether Klippy
// can be subscribed to yet.
type ServerInfo struct {
	KlippyConnected bool   `json:"klippy_connected"`
	KlippyState     string `json:"klippy_state"` // ready | startup | shutdown | error | disconnected
}

// GetServerInfo wraps server.info. Always answerable by Moonraker, even
// while Klippy is down — that's what makes it the right first call.
func (c *Client) GetServerInfo() (ServerInfo, error) {
	var info ServerInfo
	result, err := c.call("server.info", nil)
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(result, &info)
	return info, err
}

// PrinterInfo is the subset of printer.info carrying Klippy's own state
// and the human-readable reason (config error text, shutdown cause).
type PrinterInfo struct {
	State        string `json:"state"`
	StateMessage string `json:"state_message"`
}

// GetPrinterInfo wraps printer.info. Fails while Klippy is disconnected.
func (c *Client) GetPrinterInfo() (PrinterInfo, error) {
	var info PrinterInfo
	result, err := c.call("printer.info", nil)
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(result, &info)
	return info, err
}

// RestartKlipper wraps printer.restart (Klipper's RESTART: reload
// config, host side only).
func (c *Client) RestartKlipper() error {
	_, err := c.call("printer.restart", nil)
	return err
}

// FirmwareRestart wraps printer.firmware_restart (FIRMWARE_RESTART:
// reset MCUs and reload config — the way out of a shutdown).
func (c *Client) FirmwareRestart() error {
	_, err := c.call("printer.firmware_restart", nil)
	return err
}

// GcodeStoreEntry is one cached console line from server.gcode_store.
type GcodeStoreEntry struct {
	Message string  `json:"message"`
	Time    float64 `json:"time"`
	Type    string  `json:"type"` // "command" | "response"
}

// GcodeStore returns up to count of Moonraker's most recent cached
// console lines, oldest first.
func (c *Client) GcodeStore(count int) ([]GcodeStoreEntry, error) {
	result, err := c.call("server.gcode_store", map[string]any{"count": count})
	if err != nil {
		return nil, err
	}
	var body struct {
		Store []GcodeStoreEntry `json:"gcode_store"`
	}
	if err := json.Unmarshal(result, &body); err != nil {
		return nil, err
	}
	return body.Store, nil
}

// GcodeHelp wraps printer.gcode.help: every registered command
// (built-ins, extended commands and macros) mapped to its help text.
func (c *Client) GcodeHelp() (map[string]string, error) {
	result, err := c.call("printer.gcode.help", nil)
	if err != nil {
		return nil, err
	}
	var help map[string]string
	if err := json.Unmarshal(result, &help); err != nil {
		return nil, err
	}
	return help, nil
}

// DatabaseGetItem wraps server.database.get_item and returns the raw
// value stored under namespace/key (key may be dotted, "a.b.c").
// Moonraker answers with an error when the namespace or key doesn't
// exist; callers treating "absent" as normal should check for it.
func (c *Client) DatabaseGetItem(namespace, key string) (json.RawMessage, error) {
	result, err := c.call("server.database.get_item", map[string]any{
		"namespace": namespace,
		"key":       key,
	})
	if err != nil {
		return nil, err
	}
	var body struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(result, &body); err != nil {
		return nil, err
	}
	return body.Value, nil
}
