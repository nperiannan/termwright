package app

import (
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/testrec"
)

func TestDefectText(t *testing.T) {
	f := testrec.Folder{Root: t.TempDir()}
	f.Init()
	r := testrec.Result{Case: "VLAN", File: "cases/v.yaml", Start: time.Now(), Versions: map[string]string{"Lab/sw1": "32.6"},
		Steps: []testrec.StepResult{{Name: "show vlan", Target: "Lab/sw1", Output: "v1", Checks: []testrec.CheckResult{{What: "contains \"v200\"", Got: "not in the output"}}}}}
	d := defectFor(f, testrec.RunReport{Dir: "reports/x", Testbed: "testbeds/a.yaml"}, r)
	if !strings.HasPrefix(d.Summary, "Test failed: VLAN - show vlan: contains") || !strings.Contains(d.Description, "32.6") || len(d.Attach) != 1 || d.Attach[0].Name != "failed-output.txt" {
		t.Fatal(d.Summary, d.Description, d.Attach)
	}
}
