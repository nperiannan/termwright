package app

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/textfsm"
)

// ParseOutput turns a command's output into a table with a TextFSM template from the tests folder's
// templates/ ("auto": the index entry for the session's device type and the command).
func (d *testDriver) ParseOutput(session, command, template, output string) (*textfsm.Table, error) {
	f, err := d.a.testsFolder()
	if err != nil {
		return nil, err
	}
	return d.a.parseWith(f.TemplatesDir(), session, command, template, output)
}

func (a *App) parseWith(dir, session, command, template, output string) (*textfsm.Table, error) {
	var file string
	if strings.EqualFold(strings.TrimSpace(template), "auto") {
		platform := ""
		if s, err := a.store.Get(session); err == nil {
			platform = textfsm.Platform(s.DeviceType)
		}
		if platform == "" {
			return nil, fmt.Errorf("parse auto: %s has no device type - set it, or name the template", session)
		}
		ix, err := textfsm.LoadIndex(dir)
		if err != nil {
			return nil, err
		}
		files := ix.Find(platform, command)
		if len(files) == 0 {
			return nil, fmt.Errorf("parse auto: no template for %s %q in templates/index", platform, command)
		}
		file = files[0]
	} else {
		file = filepath.Join(dir, filepath.Clean("/" + template)[1:])
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("template %s: %v", filepath.Base(file), err)
	}
	t, err := textfsm.Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("template %s: %v", filepath.Base(file), err)
	}
	tab, err := t.Run(output)
	if err != nil {
		return &tab, fmt.Errorf("template %s: %v", filepath.Base(file), err)
	}
	return &tab, nil
}

// TestParse parses output with a template (for the recorder and the Tests tab: "Parse as table").
func (a *App) TestParse(session, command, template, output string) (*textfsm.Table, error) {
	f, err := a.testsFolder()
	if err != nil {
		return nil, err
	}
	if template == "" {
		template = "auto"
	}
	return a.parseWith(f.TemplatesDir(), session, command, template, output)
}

// TestsFetchTemplates downloads the community ntc-templates (TextFSM templates for many vendors and
// commands) into the tests folder's templates/ntc and adds their index lines after your own.
func (a *App) TestsFetchTemplates() (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://codeload.github.com/networktocode/ntc-templates/zip/refs/heads/master", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download ntc-templates: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("download ntc-templates: %s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return "", err
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return "", err
	}
	dst := filepath.Join(f.TemplatesDir(), "ntc")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", err
	}
	n := 0
	var index []byte
	for _, zf := range zr.File {
		i := strings.Index(zf.Name, "/ntc_templates/templates/")
		if i < 0 || zf.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(zf.Name)
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(rc, 4<<20))
		rc.Close()
		if name == "index" {
			index = data
			continue
		}
		if strings.HasSuffix(name, ".textfsm") && os.WriteFile(filepath.Join(dst, name), data, 0o644) == nil {
			n++
		}
	}
	if index == nil {
		return "", errors.New("the download has no index")
	}
	// add their index lines (paths under ntc/) after ours, once
	ip := filepath.Join(f.TemplatesDir(), "index")
	ours, _ := os.ReadFile(ip)
	if i := strings.Index(string(ours), "\n# --- ntc-templates"); i >= 0 {
		ours = ours[:i+1]
	}
	var add []string
	header := true
	for _, l := range strings.Split(string(index), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if header && strings.HasPrefix(t, "Template") {
			header = false
			continue
		}
		parts := strings.SplitN(l, ",", 2)
		if len(parts) < 2 {
			continue
		}
		var files []string
		for _, x := range strings.Split(parts[0], ":") {
			files = append(files, "ntc/"+strings.TrimSpace(x))
		}
		add = append(add, strings.Join(files, ":")+","+parts[1])
	}
	out := strings.TrimRight(string(ours), "\n") + "\n\n# --- ntc-templates (downloaded; Library → Get Community Templates again to update)\n" + strings.Join(add, "\n") + "\n"
	if err := os.WriteFile(ip, []byte(out), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d templates for %d commands in templates/ntc", n, len(add)), nil
}
