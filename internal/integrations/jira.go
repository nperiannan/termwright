package integrations

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"strings"
)

// JiraClient opens defects in Jira (REST API v2: Cloud with e-mail + API token, Data Center with a
// personal access token).
type JiraClient struct {
	Cfg   Jira
	Token string
}

func (c JiraClient) auth() string {
	if c.Cfg.User != "" {
		return "Basic " + b64([]byte(c.Cfg.User+":"+c.Token))
	}
	return "Bearer " + c.Token
}

func (c JiraClient) url(path string) (string, error) {
	b, err := base(c.Cfg.BaseURL)
	if err != nil {
		return "", err
	}
	if c.Token == "" {
		return "", errors.New("Jira: no token saved (Global Options → Integrations)")
	}
	return b + "/rest/api/2" + path, nil
}

// Check returns who the token belongs to.
func (c JiraClient) Check(ctx context.Context) (string, error) {
	u, err := c.url("/myself")
	if err != nil {
		return "", err
	}
	var me struct {
		DisplayName string `json:"displayName"`
	}
	if err := do(ctx, "GET", u, map[string]string{"Authorization": c.auth()}, nil, &me); err != nil {
		return "", err
	}
	msg := "connected as " + me.DisplayName
	if c.Cfg.Project != "" {
		u, _ = c.url("/project/" + c.Cfg.Project)
		var p struct {
			Name string `json:"name"`
		}
		if err := do(ctx, "GET", u, map[string]string{"Authorization": c.auth()}, nil, &p); err != nil {
			return msg, err
		}
		msg += ", project " + p.Name
	}
	return msg, nil
}

// Defect is a new Jira issue.
type Defect struct {
	Summary     string
	Description string
	Labels      []string
	Attach      []Attachment
}

// Create opens the defect, attaches the files and returns the issue key and its address.
func (c JiraClient) Create(ctx context.Context, d Defect) (key, link string, err error) {
	if c.Cfg.Project == "" {
		return "", "", errors.New("Jira: set the project key (Global Options → Integrations)")
	}
	it := c.Cfg.IssueType
	if it == "" {
		it = "Bug"
	}
	sum := strings.TrimSpace(d.Summary)
	if len(sum) > 250 {
		sum = sum[:250]
	}
	f := map[string]any{"project": map[string]string{"key": c.Cfg.Project}, "issuetype": map[string]string{"name": it}, "summary": sum, "description": d.Description}
	if len(d.Labels) > 0 {
		f["labels"] = d.Labels
	}
	u, err := c.url("/issue")
	if err != nil {
		return "", "", err
	}
	var r struct {
		Key string `json:"key"`
	}
	if err := do(ctx, "POST", u, map[string]string{"Authorization": c.auth()}, map[string]any{"fields": f}, &r); err != nil {
		return "", "", err
	}
	b, _ := base(c.Cfg.BaseURL)
	link = b + "/browse/" + r.Key
	for _, a := range d.Attach {
		if err := c.attach(ctx, r.Key, a); err != nil {
			return r.Key, link, err
		}
	}
	return r.Key, link, nil
}

func (c JiraClient) attach(ctx context.Context, key string, a Attachment) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	p, _ := w.CreateFormFile("file", a.Name)
	p.Write(a.Data)
	w.Close()
	u, _ := c.url("/issue/" + key + "/attachments")
	req, err := http.NewRequestWithContext(ctx, "POST", u, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", c.auth())
	req.Header.Set("X-Atlassian-Token", "no-check")
	return send(req, nil)
}
