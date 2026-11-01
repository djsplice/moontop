package moonraker

import "encoding/json"

// HistoryJob is one entry of server.history.list.
type HistoryJob struct {
	JobID         string  `json:"job_id"`
	Filename      string  `json:"filename"`
	Status        string  `json:"status"` // completed, cancelled, error, klippy_shutdown, in_progress…
	StartTime     float64 `json:"start_time"`
	EndTime       float64 `json:"end_time"`
	PrintDuration float64 `json:"print_duration"`
	TotalDuration float64 `json:"total_duration"`
	FilamentUsed  float64 `json:"filament_used"` // mm
	Exists        bool    `json:"exists"`        // the file is still there
}

// HistoryTotals is server.history.totals' job_totals.
type HistoryTotals struct {
	TotalJobs         int     `json:"total_jobs"`
	TotalTime         float64 `json:"total_time"`
	TotalPrintTime    float64 `json:"total_print_time"`
	TotalFilamentUsed float64 `json:"total_filament_used"`
	LongestJob        float64 `json:"longest_job"`
	LongestPrint      float64 `json:"longest_print"`
}

// History returns the most recent jobs, newest first.
func (c *Client) History(limit int) ([]HistoryJob, error) {
	res, err := c.call("server.history.list", map[string]any{"limit": limit, "order": "desc"})
	if err != nil {
		return nil, err
	}
	var body struct {
		Jobs []HistoryJob `json:"jobs"`
	}
	err = json.Unmarshal(res, &body)
	return body.Jobs, err
}

// HistoryTotals returns the lifetime totals.
func (c *Client) HistoryTotals() (HistoryTotals, error) {
	res, err := c.call("server.history.totals", nil)
	if err != nil {
		return HistoryTotals{}, err
	}
	var body struct {
		Totals HistoryTotals `json:"job_totals"`
	}
	err = json.Unmarshal(res, &body)
	return body.Totals, err
}

// HistoryDelete removes one job record (not its file).
func (c *Client) HistoryDelete(jobID string) error {
	_, err := c.call("server.history.delete_job", map[string]any{"uid": jobID})
	return err
}

// QueuedJob is one entry of the job queue.
type QueuedJob struct {
	JobID       string  `json:"job_id"`
	Filename    string  `json:"filename"`
	TimeAdded   float64 `json:"time_added"`
	TimeInQueue float64 `json:"time_in_queue"`
}

// JobQueue is server.job_queue.status.
type JobQueue struct {
	State string      `json:"queue_state"` // ready, loading, starting, paused
	Jobs  []QueuedJob `json:"queued_jobs"`
}

// JobQueueStatus returns the queue.
func (c *Client) JobQueueStatus() (JobQueue, error) {
	res, err := c.call("server.job_queue.status", nil)
	if err != nil {
		return JobQueue{}, err
	}
	var q JobQueue
	err = json.Unmarshal(res, &q)
	return q, err
}

// JobQueueAdd appends files to the queue.
func (c *Client) JobQueueAdd(filenames ...string) error {
	_, err := c.call("server.job_queue.post_job", map[string]any{"filenames": filenames})
	return err
}

// JobQueueRemove removes queued jobs.
func (c *Client) JobQueueRemove(jobIDs ...string) error {
	_, err := c.call("server.job_queue.delete_job", map[string]any{"job_ids": jobIDs})
	return err
}

// JobQueueStart starts (or resumes) the queue; JobQueuePause pauses it.
func (c *Client) JobQueueStart() error { _, err := c.call("server.job_queue.start", nil); return err }
func (c *Client) JobQueuePause() error { _, err := c.call("server.job_queue.pause", nil); return err }
