package testrec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPDFReport(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	f.Init()
	f.Save("v", Case{Name: "VLAN (42) check", Steps: []Step{{Run: &RunStep{Session: "s", Command: "show vlan 42"}, Expect: []Expect{{Contains: "v42"}, {Contains: "nope"}}}}})
	rep, err := f.RunFilesWith(context.Background(), nil, &fake{}, nil, RunOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(f.Root, rep.Dir, "report.pdf"))
	if err != nil || !strings.HasPrefix(string(b), "%PDF-1.4") || !strings.Contains(string(b), `VLAN \(42\) check`) || !strings.Contains(string(b), "FAIL") {
		t.Fatalf("%v %.300s", err, b)
	}
}
