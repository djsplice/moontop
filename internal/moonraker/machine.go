package moonraker

import "encoding/json"

// The machine endpoints return deep, version-dependent JSON; they're
// decoded into maps and read field by field where they're shown.

func (c *Client) callMap(method string, params any) (map[string]any, error) {
	res, err := c.call(method, params)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.Unmarshal(res, &out)
	return out, err
}

// SystemInfo is machine.system_info's "system_info": cpu_info,
// distribution, available_services, service_state, …
func (c *Client) SystemInfo() (map[string]any, error) {
	res, err := c.callMap("machine.system_info", nil)
	if err != nil {
		return nil, err
	}
	si, _ := res["system_info"].(map[string]any)
	return si, nil
}

// ProcStats is machine.proc_stats: system_cpu_usage, system_memory,
// cpu_temp, throttled_state, moonraker_stats, network, system_uptime.
func (c *Client) ProcStats() (map[string]any, error) {
	return c.callMap("machine.proc_stats", nil)
}

// DiskUsage is the gcodes root's disk_usage: total, used, free (bytes).
func (c *Client) DiskUsage() (map[string]any, error) {
	res, err := c.callMap("server.files.get_directory", map[string]any{"path": "gcodes", "extended": false})
	if err != nil {
		return nil, err
	}
	du, _ := res["disk_usage"].(map[string]any)
	return du, nil
}

// ServiceAction runs machine.services.<action> (restart, stop, start).
func (c *Client) ServiceAction(action, service string) error {
	_, err := c.call("machine.services."+action, map[string]any{"service": service})
	return err
}

// Reboot and Shutdown act on the host.
func (c *Client) Reboot() error   { _, err := c.call("machine.reboot", nil); return err }
func (c *Client) Shutdown() error { _, err := c.call("machine.shutdown", nil); return err }

// PowerDevice is one [power] device.
type PowerDevice struct {
	Device              string `json:"device"`
	Status              string `json:"status"` // on, off, init, error
	Type                string `json:"type"`
	LockedWhilePrinting bool   `json:"locked_while_printing"`
}

// PowerDevices lists the power devices (an optional component).
func (c *Client) PowerDevices() ([]PowerDevice, error) {
	res, err := c.call("machine.device_power.devices", nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		Devices []PowerDevice `json:"devices"`
	}
	err = json.Unmarshal(res, &body)
	return body.Devices, err
}

// PowerSet switches a device: action is on, off or toggle.
func (c *Client) PowerSet(device, action string) error {
	_, err := c.call("machine.device_power.post_device", map[string]any{"device": device, "action": action})
	return err
}
