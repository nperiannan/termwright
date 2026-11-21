package integrations

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// QTestClient sends test results to qTest Manager (API v3, the API token as bearer token).
type QTestClient struct {
	Cfg   QTest
	Token string
}

func (c QTestClient) hdr() map[string]string {
	return map[string]string{"Authorization": "bearer " + c.Token}
}

func (c QTestClient) url(path string) (string, error) {
	b, err := base(c.Cfg.BaseURL)
	if err != nil {
		return "", err
	}
	if c.Token == "" {
		return "", errors.New("qTest: no token saved (Global Options → Integrations)")
	}
	return b + "/api/v3" + path, nil
}

// Check reads the project and returns its name.
func (c QTestClient) Check(ctx context.Context) (string, error) {
	if c.Cfg.Project == 0 {
		var ps []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		u, err := c.url("/projects")
		if err != nil {
			return "", err
		}
		if err := do(ctx, "GET", u, c.hdr(), nil, &ps); err != nil {
			return "", err
		}
		l := []string{}
		for i, p := range ps {
			if i == 15 {
				break
			}
			l = append(l, fmt.Sprintf("%d %s", p.ID, p.Name))
		}
		return "connected; set the project id - projects: " + strings.Join(l, ", "), nil
	}
	var p struct {
		Name string `json:"name"`
	}
	u, err := c.url(fmt.Sprintf("/projects/%d", c.Cfg.Project))
	if err != nil {
		return "", err
	}
	if err := do(ctx, "GET", u, c.hdr(), nil, &p); err != nil {
		return "", err
	}
	return "connected to project " + p.Name, nil
}

// QTestResult is one test's result for qTest.
type QTestResult struct {
	TestCase string // the test case id (123) or its PID (TC-123)
	Name     string
	Passed   bool
	Start    time.Time
	End      time.Time
	Note     string
	Defect   string // a Jira key, put in the note
	Attach   []Attachment
}

// TestCaseID finds a test case's id from "123" or "TC-123".
func (c QTestClient) TestCaseID(ctx context.Context, tc string) (int, error) {
	tc = strings.TrimSpace(tc)
	if n, err := strconv.Atoi(tc); err == nil {
		return n, nil
	}
	u, err := c.url(fmt.Sprintf("/projects/%d/search", c.Cfg.Project))
	if err != nil {
		return 0, err
	}
	var r struct {
		Items []struct {
			ID  int    `json:"id"`
			PID string `json:"pid"`
		} `json:"items"`
	}
	q := map[string]any{"object_type": "test-cases", "fields": []string{"id", "pid"}, "query": fmt.Sprintf("'id' = '%s'", strings.ReplaceAll(tc, "'", ""))}
	if err := do(ctx, "POST", u, c.hdr(), q, &r); err != nil {
		return 0, err
	}
	for _, it := range r.Items {
		if strings.EqualFold(it.PID, tc) {
			return it.ID, nil
		}
	}
	return 0, fmt.Errorf("qTest: no test case %s in the project", tc)
}

// Submit makes a test run of the test case (in the configured cycle) and logs the result with its
// attachments. It returns the test run's PID.
func (c QTestClient) Submit(ctx context.Context, r QTestResult) (string, error) {
	if c.Cfg.Project == 0 {
		return "", errors.New("qTest: set the project id (Global Options → Integrations)")
	}
	id, err := c.TestCaseID(ctx, r.TestCase)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	if c.Cfg.Cycle != 0 {
		q.Set("parentId", strconv.Itoa(c.Cfg.Cycle))
		q.Set("parentType", "test-cycle")
	} else {
		q.Set("parentId", "0")
		q.Set("parentType", "root")
	}
	u, err := c.url(fmt.Sprintf("/projects/%d/test-runs?%s", c.Cfg.Project, q.Encode()))
	if err != nil {
		return "", err
	}
	var run struct {
		ID  int    `json:"id"`
		PID string `json:"pid"`
	}
	if err := do(ctx, "POST", u, c.hdr(), map[string]any{"name": r.Name, "test_case": map[string]int{"id": id}}, &run); err != nil {
		return "", err
	}
	status := "PASSED"
	if !r.Passed {
		status = "FAILED"
	}
	note := r.Note
	if r.Defect != "" {
		note = "Defect: " + r.Defect + "\n" + note
	}
	atts := []map[string]string{}
	for _, a := range r.Attach {
		atts = append(atts, map[string]string{"name": a.Name, "content_type": a.ContentType, "data": b64(a.Data)})
	}
	log := map[string]any{"name": r.Name, "status": status, "exe_start_date": r.Start.UTC().Format(time.RFC3339), "exe_end_date": r.End.UTC().Format(time.RFC3339), "note": note, "attachments": atts}
	u, _ = c.url(fmt.Sprintf("/projects/%d/test-runs/%d/auto-test-logs", c.Cfg.Project, run.ID))
	if err := do(ctx, "POST", u, c.hdr(), log, nil); err != nil {
		return run.PID, err
	}
	return run.PID, nil
}
