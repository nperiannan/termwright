package testrec

import (
	"os"
	"strings"
	"testing"
)

func writeFile(p string) error { return os.WriteFile(p, []byte("x"), 0o644) }

func TestWebBlocks(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	if _, err := f.SaveWebBlock("Add contact", "", []WebAction{{Do: "click", Target: "text=New"}, {Do: "fill", Target: "label=Name", Value: "{{name}}"}, {Do: "fill", Target: "label=Password", Value: "{{password}}", Secret: true}}); err != nil {
		t.Fatal(err)
	}
	p, err := f.LoadProc("add-contact")
	if err != nil || len(p.Params) != 1 || p.Params[0] != "name" {
		t.Fatalf("%v %+v", err, p)
	}
	c := Case{Name: "x", Steps: []Step{{Web: &WebStep{URL: "/", Actions: []WebAction{{Do: "block", Target: "add-contact", With: map[string]string{"name": "Ann"}}, {Do: "expect", Value: "Ann"}}}}}}
	pc, err := f.Prepare(c)
	if err != nil {
		t.Fatal(err)
	}
	a := pc.Steps[0].Web.Actions
	if len(a) != 4 || a[1].Value != "Ann" || a[2].Value != "{{password}}" || a[3].Do != "expect" || c.Steps[0].Web.Actions[0].Do != "block" {
		t.Fatalf("%+v", a)
	}
	if _, err := f.Prepare(Case{Steps: []Step{{Web: &WebStep{Actions: []WebAction{{Do: "block", Target: "add-contact"}}}}}}); err == nil {
		t.Fatal("missing name not reported")
	}
}

func TestMoveWebFiles(t *testing.T) {
	d := t.TempDir()
	pic := d + "/web-1-01.jpg"
	if err := writeFile(pic); err != nil {
		t.Fatal(err)
	}
	rep := RunReport{Results: []Result{{Steps: []StepResult{{Output: "✓ click #a\nhealed 1: #a → #b\npicture: " + pic + "\npage: x"}}}}}
	run := t.TempDir()
	moveWebFiles(&rep, run)
	out := rep.Results[0].Steps[0].Output
	h := string(HTML(rep))
	if !strings.Contains(out, "picture: web/web-1-01.jpg") || !strings.Contains(h, `<img src="web/web-1-01.jpg"`) || !strings.Contains(h, "healed 1") {
		t.Fatalf("%s\n%s", out, h)
	}
}
