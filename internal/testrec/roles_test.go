package testrec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRolesApproval(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	r, _ := f.Roles()
	if r.May("anyone", "edit") != nil || r.NeedsApproval(true, "tb") {
		t.Fatal("empty roles must allow everything")
	}
	os.WriteFile(filepath.Join(f.Root, "roles.yaml"), []byte("editors: [ann]\napproval: production\n"), 0o644)
	r, _ = f.Roles()
	if r.May("bob", "edit") == nil || r.May("ann", "edit") != nil || r.May("bob", "run") != nil || !r.NeedsApproval(true, "") || r.NeedsApproval(false, "") {
		t.Fatalf("%+v", r)
	}
	files := []string{"cases/b.yaml", "cases/a.yaml"}
	a, err := f.RequestApproval(files, "lab1", "release check")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Approved(files, "lab1"); ok {
		t.Fatal("approved before a decision")
	}
	if _, err := f.Decide(a.ID, true, "ok"); err != nil { // no approvers listed: anyone, even the requester
		t.Fatal(err)
	}
	if _, ok := f.Approved([]string{"cases/a.yaml", "cases/b.yaml"}, "lab1"); !ok {
		t.Fatal("not approved")
	}
	if _, ok := f.Approved(files, "lab2"); ok {
		t.Fatal("other testbed")
	}
	os.WriteFile(filepath.Join(f.Root, "roles.yaml"), []byte("approvers: ["+Me()+"]\n"), 0o644)
	b, _ := f.RequestApproval(files, "", "")
	if _, err := f.Decide(b.ID, true, ""); err == nil {
		t.Fatal("approved own request")
	}
	if !errors.Is(ErrNeedsApproval, ErrNeedsApproval) {
		t.Fatal()
	}
}
