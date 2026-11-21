// Package integrations talks to the services a test run reports to: qTest (test management), Jira
// (defects) and a Git host (the tests folder's remote). Addresses and user names are settings; the
// tokens are kept in the OS credential store and never in the settings or the output.
package integrations

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config is the Integrations page of Global Options.
type Config struct {
	QTest QTest `json:"qtest"`
	Jira  Jira  `json:"jira"`
	Git   Git   `json:"git"`
}

// QTest: the qTest Manager site, the user and the project test runs go to.
type QTest struct {
	BaseURL string `json:"baseUrl,omitempty"` // https://yourcompany.qtestnet.com
	User    string `json:"user,omitempty"`
	Project int    `json:"project,omitempty"` // the project id
	// Cycle: the test cycle id new test runs are made in (empty: the project's root).
	Cycle int `json:"cycle,omitempty"`
	// Auto: send every run's results (manual runs and schedules) without asking.
	Auto bool `json:"auto,omitempty"`
}

// Jira: the Jira site (Cloud or Data Center) defects are opened in.
type Jira struct {
	BaseURL   string `json:"baseUrl,omitempty"` // https://yourcompany.atlassian.net
	User      string `json:"user,omitempty"`    // the e-mail address on Cloud; empty on Data Center (a personal access token)
	Project   string `json:"project,omitempty"` // project key, e.g. NET
	IssueType string `json:"issueType,omitempty"`
	// Auto: open a defect for every failed test of a scheduled run.
	Auto bool `json:"auto,omitempty"`
}

// Git: the host the tests folder is cloned from, pulled and pushed to over HTTPS.
type Git struct {
	Host string `json:"host,omitempty"` // github.com, git.example.com
	User string `json:"user,omitempty"`
}

// Kinds of tokens; the credential store keys are "integration:<kind>".
const (
	KindQTest = "qtest"
	KindJira  = "jira"
	KindGit   = "git"
)

// TokenKey is the credential store key of a kind's token.
func TokenKey(kind string) string { return "integration:" + kind }

var client = &http.Client{Timeout: 60 * time.Second}

// do sends a JSON request and decodes the JSON answer into out (when not nil).
func do(ctx context.Context, method, u string, hdr map[string]string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return send(req, out)
}

func send(req *http.Request, out any) error {
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(b))
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		switch res.StatusCode {
		case 401:
			return fmt.Errorf("%s: the user name or token was refused (401)", req.URL.Host)
		case 403:
			return fmt.Errorf("%s: no permission (403) %s", req.URL.Host, msg)
		}
		return fmt.Errorf("%s %s: %s %s", req.Method, req.URL.Path, res.Status, msg)
	}
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("%s: not a JSON answer", req.URL.Path)
		}
	}
	return nil
}

func base(u string) (string, error) {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" {
		return "", fmt.Errorf("%q is not an address like https://example.com", u)
	}
	if p.User != nil {
		return "", errors.New("leave the user and token out of the address")
	}
	return u, nil
}

// Attachment is a file sent with a result or defect.
type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
