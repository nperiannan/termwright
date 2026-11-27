package testrec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type webRoleDriver struct{ cmds, logins []string }

func (d *webRoleDriver) Command(_ context.Context, session, cmd, _ string) (string, error) {
	d.cmds = append(d.cmds, session+": "+cmd)
	return "", nil
}
func (d *webRoleDriver) HTTP(context.Context, HTTPStep) (HTTPAnswer, error) { return HTTPAnswer{}, nil }
func (d *webRoleDriver) SNMP(context.Context, SNMPStep) ([]SNMPValue, error) {
	return nil, nil
}
func (d *webRoleDriver) Web(_ context.Context, w WebStep) (string, error) {
	d.logins = append(d.logins, w.Login)
	return "", nil
}

// A recorded web test names its web app as a session; loaded, the app is a role a testbed can fill.
func TestWebLoginIsARole(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "cases"), 0o755)
	os.WriteFile(filepath.Join(root, "cases", "onboard.yaml"), []byte(`name: onboard
vars:
  dut: Lab/x465-1
steps:
  - run: { session: "{{dut}}", command: "configure iqagent server ipaddress 10.1.1.5" }
  - web: { login: "Web Apps/ExtremeCloud IQ", actions: [ { do: click, target: "text=Onboard" } ] }
  - web: { login: "Web Apps/ExtremeCloud IQ", actions: [ { do: fill, target: "#serial", value: "{{serial}}" } ] }
  - web: { login: "{{other}}" }
`), 0o644)
	f := Folder{Root: root}
	c, err := f.Load("cases/onboard.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Vars["extremecloud_iq"] != "Web Apps/ExtremeCloud IQ" || c.Steps[1].Web.Login != "{{extremecloud_iq}}" || c.Steps[2].Web.Login != "{{extremecloud_iq}}" || c.Steps[3].Web.Login != "{{other}}" {
		t.Fatalf("%v %q %q", c.Vars, c.Steps[1].Web.Login, c.Steps[3].Web.Login)
	}
	if !caseRoles(c)["extremecloud_iq"] || !caseRoles(c)["dut"] {
		t.Fatal(caseRoles(c))
	}
	// its own sessions
	d := &webRoleDriver{}
	RunWith(context.Background(), c, d, map[string]string{"serial": "X1", "other": "Web Apps/Other"}, RunOptions{})
	if strings.Join(d.logins, ",") != "Web Apps/ExtremeCloud IQ,Web Apps/ExtremeCloud IQ,Web Apps/Other" || d.cmds[0] != "Lab/x465-1: configure iqagent server ipaddress 10.1.1.5" {
		t.Fatalf("%v %v", d.logins, d.cmds)
	}
	// a testbed with its own switch and its own web app
	d = &webRoleDriver{}
	RunWith(context.Background(), c, d, map[string]string{"dut": "Bed2/5520-1", "extremecloud_iq": "Bed2/XIQ staging", "serial": "X1", "other": "o"}, RunOptions{})
	if d.logins[0] != "Bed2/XIQ staging" || d.cmds[0] != "Bed2/5520-1: configure iqagent server ipaddress 10.1.1.5" {
		t.Fatalf("%v %v", d.logins, d.cmds)
	}
	// the Playwright export names the session itself
	if l := literalWebLogins(c); l.Steps[1].Web.Login != "Web Apps/ExtremeCloud IQ" || c.Steps[1].Web.Login != "{{extremecloud_iq}}" {
		t.Fatal(l.Steps[1].Web.Login, c.Steps[1].Web.Login)
	}
	// a variable that already names the session is reused; a clashing name gets a suffix
	c2 := Case{Vars: map[string]string{"xiq": "Web Apps/ExtremeCloud IQ", "crm": "something else"}, Steps: []Step{{Web: &WebStep{Login: "Web Apps/ExtremeCloud IQ"}}, {Web: &WebStep{Login: "Apps/CRM"}}}}
	webLoginRoles(&c2)
	if c2.Steps[0].Web.Login != "{{xiq}}" || c2.Steps[1].Web.Login != "{{crm_2}}" || c2.Vars["crm_2"] != "Apps/CRM" {
		t.Fatalf("%+v %q %q", c2.Vars, c2.Steps[0].Web.Login, c2.Steps[1].Web.Login)
	}
}
