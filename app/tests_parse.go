package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
