// Package clickup is a thin HTTP wrapper around ClickUp's v2 REST API.
// It exposes only the operations the dashboard needs: list tasks in a list,
// create a task, and update a task's status. Auth is a single personal token
// sent in the Authorization header.
package clickup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const baseURL = "https://api.clickup.com/api/v2"

// ProjectStatuses is the canonical kanban pipeline the dashboard writes
// into every connected ClickUp list. Order matters: first entry is the
// "open" state, last is the "closed" state; everything in between is a
// custom in-flight state in ClickUp's status-type taxonomy.
var ProjectStatuses = []string{
	"to do",
	"in progress",
	"sit",
	"qa",
	"ppd",
	"qa fail",
	"blocker",
	"sit pass",
	"ppd pass",
	"completed",
}

// NormaliseStatus returns the lowercased canonical form of an arbitrary
// status string from ClickUp or the local DB, so comparisons are symmetric.
func NormaliseStatus(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Client talks to the ClickUp REST API on behalf of a single user.
type Client struct {
	token string
	http  *http.Client
}

// Task is a trimmed view of the ClickUp task schema — only the fields the UI
// actually renders. Full response is re-exposed as raw JSON via RawTasks when
// needed.
type Task struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	StatusColor string   `json:"status_color"`
	URL         string   `json:"url"`
	DueDate     string   `json:"due_date,omitempty"`
	Assignees   []string `json:"assignees,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	// DateUpdated is ClickUp's last-edit timestamp (unix millis as string).
	// Used by the sync engine to resolve push/pull conflicts: whichever side
	// has the newer timestamp wins.
	DateUpdated string `json:"date_updated,omitempty"`
}

// New returns a Client bound to a ClickUp personal API token. The Client is
// safe for concurrent use. If token is empty, methods return ErrNotConfigured.
func New(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 15 * time.Second},
	}
}

// ErrNotConfigured signals that no API token was supplied. Callers can map
// this to an HTTP 503 or a "configure ClickUp" empty-state in the UI.
var ErrNotConfigured = errors.New("clickup: api token not configured")

// ListTasks fetches non-archived tasks in the given list ID.
func (c *Client) ListTasks(listID string) ([]Task, error) {
	if c.token == "" {
		return nil, ErrNotConfigured
	}
	if listID == "" {
		return nil, errors.New("clickup: list id required")
	}
	url := fmt.Sprintf("%s/list/%s/task?archived=false&subtasks=true", baseURL, listID)
	body, err := c.do(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	var env struct {
		Tasks []struct {
			ID          string          `json:"id"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Status      json.RawMessage `json:"status"`
			URL         string          `json:"url"`
			DueDate     string          `json:"due_date"`
			Priority    json.RawMessage `json:"priority"`
			Tags        []struct {
				Name string `json:"name"`
			} `json:"tags"`
			Assignees []struct {
				Username string `json:"username"`
			} `json:"assignees"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("clickup: parse list response: %w", err)
	}

	out := make([]Task, 0, len(env.Tasks))
	for _, t := range env.Tasks {
		task := Task{
			ID:          t.ID,
			Name:        t.Name,
			Description: t.Description,
			URL:         t.URL,
			DueDate:     t.DueDate,
		}
		// `status` is usually `{status:"to do", color:"#d3d3d3"}` but may be a
		// bare string — handle both gracefully.
		var statusObj struct {
			Status string `json:"status"`
			Color  string `json:"color"`
		}
		if err := json.Unmarshal(t.Status, &statusObj); err == nil && statusObj.Status != "" {
			task.Status = statusObj.Status
			task.StatusColor = statusObj.Color
		} else {
			var s string
			if err := json.Unmarshal(t.Status, &s); err == nil {
				task.Status = s
			}
		}
		var prioObj struct {
			Priority string `json:"priority"`
		}
		if err := json.Unmarshal(t.Priority, &prioObj); err == nil && prioObj.Priority != "" {
			task.Priority = prioObj.Priority
		}
		for _, tag := range t.Tags {
			task.Tags = append(task.Tags, tag.Name)
		}
		for _, a := range t.Assignees {
			task.Assignees = append(task.Assignees, a.Username)
		}
		// date_updated is at top level of each task in the raw response, not
		// inside the struct we decoded above. Re-parse the tasks element as a
		// map just to pick this one field out — cheap and robust.
		task.DateUpdated = extractDateUpdated(body, task.ID)
		out = append(out, task)
	}
	return out, nil
}

// extractDateUpdated pulls `date_updated` for a specific task ID out of the
// raw ListTasks response. Saves us having to redefine the full schema.
func extractDateUpdated(body []byte, id string) string {
	var raw struct {
		Tasks []struct {
			ID          string `json:"id"`
			DateUpdated string `json:"date_updated"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	for _, t := range raw.Tasks {
		if t.ID == id {
			return t.DateUpdated
		}
	}
	return ""
}

// GetTask fetches a single task by ID. Used by the sync engine when we
// only have a task id and need current status + date_updated.
func (c *Client) GetTask(taskID string) (*Task, error) {
	if c.token == "" {
		return nil, ErrNotConfigured
	}
	url := fmt.Sprintf("%s/task/%s", baseURL, taskID)
	body, err := c.do(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		URL         string `json:"url"`
		DateUpdated string `json:"date_updated"`
		Status      struct {
			Status string `json:"status"`
			Color  string `json:"color"`
		} `json:"status"`
		Priority *struct {
			Priority string `json:"priority"`
		} `json:"priority"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("clickup: parse get task response: %w", err)
	}
	t := &Task{
		ID:          raw.ID,
		Name:        raw.Name,
		Description: raw.Description,
		URL:         raw.URL,
		DateUpdated: raw.DateUpdated,
		Status:      raw.Status.Status,
		StatusColor: raw.Status.Color,
	}
	if raw.Priority != nil {
		t.Priority = raw.Priority.Priority
	}
	return t, nil
}

// UpdateTaskInput covers the fields the sync engine ever needs to push back.
// Empty fields are dropped from the JSON so partial updates work.
type UpdateTaskInput struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Priority    int    `json:"priority,omitempty"` // 1=urgent … 4=low
}

// UpdateTask patches one or more fields on an existing task.
func (c *Client) UpdateTask(taskID string, in UpdateTaskInput) (*Task, error) {
	if c.token == "" {
		return nil, ErrNotConfigured
	}
	url := fmt.Sprintf("%s/task/%s", baseURL, taskID)
	buf, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	body, err := c.do(http.MethodPut, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID          string `json:"id"`
		DateUpdated string `json:"date_updated"`
		Status      struct {
			Status string `json:"status"`
		} `json:"status"`
	}
	_ = json.Unmarshal(body, &raw)
	return &Task{
		ID:          raw.ID,
		Status:      raw.Status.Status,
		DateUpdated: raw.DateUpdated,
	}, nil
}

// EnsureListStatuses makes sure the target ClickUp list exposes every status
// in `wanted` (in order). If statuses are missing or in a different order,
// it PUTs the full list config with `wanted` substituted in. Idempotent.
//
// ClickUp's /list/{id} endpoint takes the whole list config; this function
// fetches the current list first, patches only the `statuses` array, then
// writes it back.
func (c *Client) EnsureListStatuses(listID string, wanted []string) error {
	if c.token == "" {
		return ErrNotConfigured
	}
	if listID == "" {
		return errors.New("clickup: list id required")
	}

	// Fetch current list config — statuses object is nested.
	body, err := c.do(http.MethodGet, fmt.Sprintf("%s/list/%s", baseURL, listID), nil)
	if err != nil {
		return fmt.Errorf("clickup: fetch list config: %w", err)
	}
	var listRaw struct {
		Statuses []struct {
			Status string `json:"status"`
		} `json:"statuses"`
	}
	if err := json.Unmarshal(body, &listRaw); err != nil {
		return fmt.Errorf("clickup: parse list config: %w", err)
	}

	have := make(map[string]struct{}, len(listRaw.Statuses))
	for _, s := range listRaw.Statuses {
		have[strings.ToLower(s.Status)] = struct{}{}
	}
	missing := false
	for _, w := range wanted {
		if _, ok := have[strings.ToLower(w)]; !ok {
			missing = true
			break
		}
	}
	if !missing && len(have) >= len(wanted) {
		return nil // already good
	}

	// Build the statuses payload. ClickUp accepts:
	//   {"statuses": [{"status": "to do", "type": "open"}, …]}
	// `type` rules: first must be "open", last "closed", rest "custom".
	type statusObj struct {
		Status string `json:"status"`
		Type   string `json:"type"`
	}
	payload := make([]statusObj, 0, len(wanted))
	for i, w := range wanted {
		t := "custom"
		if i == 0 {
			t = "open"
		} else if i == len(wanted)-1 {
			t = "closed"
		}
		payload = append(payload, statusObj{Status: w, Type: t})
	}
	buf, _ := json.Marshal(map[string]any{"statuses": payload})
	if _, err := c.do(http.MethodPut, fmt.Sprintf("%s/list/%s", baseURL, listID), bytes.NewReader(buf)); err != nil {
		return fmt.Errorf("clickup: update list statuses: %w", err)
	}
	return nil
}

// CreateTaskInput is the minimal payload for a new ClickUp task.
type CreateTaskInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Priority    int    `json:"priority,omitempty"` // 1=urgent … 4=low
}

// CreateTask posts a new task to the given list.
func (c *Client) CreateTask(listID string, in CreateTaskInput) (*Task, error) {
	if c.token == "" {
		return nil, ErrNotConfigured
	}
	url := fmt.Sprintf("%s/list/%s/task", baseURL, listID)
	buf, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	body, err := c.do(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		URL    string `json:"url"`
		Status struct {
			Status string `json:"status"`
			Color  string `json:"color"`
		} `json:"status"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("clickup: parse create response: %w", err)
	}
	return &Task{
		ID:          raw.ID,
		Name:        raw.Name,
		URL:         raw.URL,
		Status:      raw.Status.Status,
		StatusColor: raw.Status.Color,
	}, nil
}

// UpdateTaskStatus patches a task's status. Pass the ClickUp status string
// (e.g. "in progress"). Returns a minimal refreshed task.
func (c *Client) UpdateTaskStatus(taskID, status string) error {
	if c.token == "" {
		return ErrNotConfigured
	}
	url := fmt.Sprintf("%s/task/%s", baseURL, taskID)
	buf, _ := json.Marshal(map[string]string{"status": status})
	_, err := c.do(http.MethodPut, url, bytes.NewReader(buf))
	return err
}

// do executes an HTTP request and returns the response body. Non-2xx
// responses are surfaced as errors with the body appended for debugging.
func (c *Client) do(method, url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clickup: %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("clickup: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("clickup: %s %s: status %d: %s", method, url, resp.StatusCode, string(data))
	}
	return data, nil
}
