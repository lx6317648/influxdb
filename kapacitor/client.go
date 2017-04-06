package kapacitor

import (
	"context"
	"fmt"

	"github.com/influxdata/chronograf"
	client "github.com/influxdata/kapacitor/client/v1"
)

// Client communicates to kapacitor
type Client struct {
	URL      string
	Username string
	Password string
	ID       chronograf.ID
	Ticker   chronograf.Ticker
}

const (
	// Prefix is prepended to the ID of all alerts
	Prefix = "chronograf-v1-"
)

// Task represents a running kapacitor task
type Task struct {
	ID         string                // Kapacitor ID
	Href       string                // Kapacitor relative URI
	HrefOutput string                // Kapacitor relative URI to HTTPOutNode
	Rule       chronograf.AlertRule  // Rule is the rule that represents this Task
	TICKScript chronograf.TICKScript // TICKScript is the running script
}

// Href returns the link to a kapacitor task given an id
func (c *Client) Href(ID string) string {
	return fmt.Sprintf("/kapacitor/v1/tasks/%s", ID)
}

// HrefOutput returns the link to a kapacitor task httpOut Node given an id
func (c *Client) HrefOutput(ID string) string {
	return fmt.Sprintf("/kapacitor/v1/tasks/%s/%s", ID, HTTPEndpoint)
}

// Create builds and POSTs a tickscript to kapacitor
func (c *Client) Create(ctx context.Context, rule chronograf.AlertRule) (*Task, error) {
	kapa, err := c.kapaClient(ctx)
	if err != nil {
		return nil, err
	}

	id, err := c.ID.Generate()
	if err != nil {
		return nil, err
	}

	script, err := c.Ticker.Generate(rule)
	if err != nil {
		return nil, err
	}

	kapaID := Prefix + id
	rule.ID = kapaID
	task, err := kapa.CreateTask(client.CreateTaskOptions{
		ID:         kapaID,
		Type:       toTask(rule.Query),
		DBRPs:      []client.DBRP{{Database: rule.Query.Database, RetentionPolicy: rule.Query.RetentionPolicy}},
		TICKscript: string(script),
		Status:     client.Enabled,
	})
	if err != nil {
		return nil, err
	}

	return &Task{
		ID:         kapaID,
		Href:       task.Link.Href,
		HrefOutput: c.HrefOutput(kapaID),
		TICKScript: script,
		Rule:       rule,
	}, nil
}

// Delete removes tickscript task from kapacitor
func (c *Client) Delete(ctx context.Context, href string) error {
	kapa, err := c.kapaClient(ctx)
	if err != nil {
		return err
	}
	return kapa.DeleteTask(client.Link{Href: href})
}

func (c *Client) updateStatus(ctx context.Context, href string, status client.TaskStatus) (*Task, error) {
	kapa, err := c.kapaClient(ctx)
	if err != nil {
		return nil, err
	}

	opts := client.UpdateTaskOptions{
		Status: status,
	}

	task, err := kapa.UpdateTask(client.Link{Href: href}, opts)
	if err != nil {
		return nil, err
	}

	return &Task{
		ID:         task.ID,
		Href:       task.Link.Href,
		HrefOutput: c.HrefOutput(task.ID),
		TICKScript: chronograf.TICKScript(task.TICKscript),
	}, nil
}

// Disable changes the tickscript status to disabled for a given href.
func (c *Client) Disable(ctx context.Context, href string) (*Task, error) {
	return c.updateStatus(ctx, href, client.Disabled)
}

// Enable changes the tickscript status to disabled for a given href.
func (c *Client) Enable(ctx context.Context, href string) (*Task, error) {
	return c.updateStatus(ctx, href, client.Enabled)
}

// AllStatus returns the status of all tasks in kapacitor
func (c *Client) AllStatus(ctx context.Context) (map[string]string, error) {
	kapa, err := c.kapaClient(ctx)
	if err != nil {
		return nil, err
	}

	// Only get the status, id and link section back
	opts := &client.ListTasksOptions{
		Fields: []string{"status"},
	}
	tasks, err := kapa.ListTasks(opts)
	if err != nil {
		return nil, err
	}

	taskStatuses := map[string]string{}
	for _, task := range tasks {
		taskStatuses[task.ID] = task.Status.String()
	}

	return taskStatuses, nil
}

// Status returns the status of a task in kapacitor
func (c *Client) Status(ctx context.Context, href string) (string, error) {
	kapa, err := c.kapaClient(ctx)
	if err != nil {
		return "", err
	}
	task, err := kapa.Task(client.Link{Href: href}, nil)
	if err != nil {
		return "", err
	}

	return task.Status.String(), nil
}

// All returns all tasks in kapacitor
func (c *Client) All(ctx context.Context) (map[string]chronograf.AlertRule, error) {
	kapa, err := c.kapaClient(ctx)
	if err != nil {
		return nil, err
	}

	// Only get the status, id and link section back
	opts := &client.ListTasksOptions{}
	tasks, err := kapa.ListTasks(opts)
	if err != nil {
		return nil, err
	}

	alerts := map[string]chronograf.AlertRule{}
	for _, task := range tasks {
		script := chronograf.TICKScript(task.TICKscript)
		if rule, err := Reverse(script); err != nil {
			alerts[task.ID] = chronograf.AlertRule{
				ID:         task.ID,
				Name:       task.ID,
				TICKScript: script,
			}
		} else {
			rule.ID = task.ID
			rule.TICKScript = script
			alerts[task.ID] = rule
		}
	}
	return alerts, nil
}

// Get returns a single alert in kapacitor
func (c *Client) Get(ctx context.Context, id string) (chronograf.AlertRule, error) {
	kapa, err := c.kapaClient(ctx)
	if err != nil {
		return chronograf.AlertRule{}, err
	}
	href := c.Href(id)
	task, err := kapa.Task(client.Link{Href: href}, nil)
	if err != nil {
		return chronograf.AlertRule{}, chronograf.ErrAlertNotFound
	}

	script := chronograf.TICKScript(task.TICKscript)
	rule, err := Reverse(script)
	if err != nil {
		return chronograf.AlertRule{
			ID:         task.ID,
			Name:       task.ID,
			TICKScript: script,
		}, nil
	}
	rule.ID = task.ID
	rule.TICKScript = script
	return rule, nil
}

// Update changes the tickscript of a given id.
func (c *Client) Update(ctx context.Context, href string, rule chronograf.AlertRule) (*Task, error) {
	kapa, err := c.kapaClient(ctx)
	if err != nil {
		return nil, err
	}

	script, err := c.Ticker.Generate(rule)
	if err != nil {
		return nil, err
	}

	// We need to disable the kapacitor task followed by enabling it during update.
	opts := client.UpdateTaskOptions{
		TICKscript: string(script),
		Status:     client.Disabled,
		Type:       toTask(rule.Query),
		DBRPs: []client.DBRP{
			{
				Database:        rule.Query.Database,
				RetentionPolicy: rule.Query.RetentionPolicy,
			},
		},
	}

	task, err := kapa.UpdateTask(client.Link{Href: href}, opts)
	if err != nil {
		return nil, err
	}

	// Now enable the task.
	if _, err := c.Enable(ctx, href); err != nil {
		return nil, err
	}

	return &Task{
		ID:         task.ID,
		Href:       task.Link.Href,
		HrefOutput: c.HrefOutput(task.ID),
		TICKScript: script,
		Rule:       rule,
	}, nil
}

func (c *Client) kapaClient(ctx context.Context) (*client.Client, error) {
	var creds *client.Credentials
	if c.Username != "" {
		creds = &client.Credentials{
			Method:   client.UserAuthentication,
			Username: c.Username,
			Password: c.Password,
		}
	}

	return client.New(client.Config{
		URL:         c.URL,
		Credentials: creds,
	})
}

func toTask(q chronograf.QueryConfig) client.TaskType {
	if q.RawText == "" {
		return client.StreamTask
	}
	return client.BatchTask
}
