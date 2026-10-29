package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/session"
)

func TestSettingsTransfer(t *testing.T) {
	src := Defaults(t.TempDir())
	src.MasterHash = "secret-verifier"
	src.SessionsDir = "/somewhere/Sessions"
	src.OpenTabs = []string{"Lab/a"}
	src.FontSize = 17
	src.NoMoveConfirm = true
	src.DeviceTypes = append(src.DeviceTypes, session.DeviceType{Name: "MyOS", Username: "ops", Credential: "lab"})
	src.Credentials = []Credential{{Name: "lab", Username: "admin", Note: "lab devices"}}
	src.FolderDefaults = []FolderDefault{{Folder: "Lab", Account: "lab", JumpSession: "Lab/jump"}}
	src.Checklists = []Checklist{{Name: "Pre", Targets: []string{"Lab/", "Lab/sw1"}}}
	src.Workspaces = []Workspace{{Name: "W", Tabs: []WorkspaceTab{{Session: "Lab/sw1", Kind: "terminal"}}}}

	data, err := ExportSettings(src, "0.21.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"secret-verifier", "/somewhere/Sessions", "openTabs", "masterHash"} {
		if strings.Contains(string(data), bad) {
			t.Fatalf("export contains %q", bad)
		}
	}
	sum, err := SummarizeSettings(data)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Accounts != 1 || sum.FolderDefaults != 1 || sum.DeviceTypes != len(src.DeviceTypes) || sum.Options == 0 || sum.Library == 0 {
		t.Fatalf("summary %+v", sum)
	}

	dst := Defaults(t.TempDir())
	dst.MasterHash = "mine"
	dst.SessionsDir = "/mine/Sessions"
	dst.Credentials = []Credential{{Name: "other", Username: "x"}}
	mapPath := func(p string) string {
		if p == "Lab/sw1" {
			return "Imported/Lab/sw1 (2)"
		}
		return session.JoinPath("Imported", p)
	}

	// only logins
	got, notes, err := ImportSettings(dst, data, []string{PartLogins}, "Imported", mapPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.FontSize != dst.FontSize || len(got.Checklists) != 0 {
		t.Fatal("options or library imported without being chosen")
	}
	if len(got.Credentials) != 2 || got.FolderDefaults[0].Folder != "Imported/Lab" || got.FolderDefaults[0].JumpSession != "Imported/Lab/jump" || got.FolderDefaults[0].Account != "lab" {
		t.Fatalf("logins %+v %+v", got.Credentials, got.FolderDefaults)
	}
	found := false
	for _, d := range got.DeviceTypes {
		if d.Name == "MyOS" && d.Credential == "lab" {
			found = true
		}
	}
	if !found || len(notes) == 0 {
		t.Fatalf("device type not imported; notes %v", notes)
	}

	// everything
	got, _, err = ImportSettings(dst, data, []string{PartLogins, PartLibrary, PartOptions}, "Imported", mapPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.FontSize != 17 || !got.NoMoveConfirm {
		t.Fatal("options not imported")
	}
	if got.MasterHash != "mine" || got.SessionsDir != "/mine/Sessions" {
		t.Fatal("this computer's settings were replaced")
	}
	if c := got.Checklists[0]; c.Targets[0] != "Imported/Lab/" || c.Targets[1] != "Imported/Lab/sw1 (2)" {
		t.Fatalf("targets %v", c.Targets)
	}
	if got.Workspaces[0].Tabs[0].Session != "Imported/Lab/sw1 (2)" {
		t.Fatal("workspace path not mapped")
	}

	// importing twice adds nothing
	again, notes, _ := ImportSettings(got, data, []string{PartLogins, PartLibrary}, "Imported", mapPath)
	a, _ := json.Marshal(again)
	b, _ := json.Marshal(got)
	if string(a) != string(b) || len(notes) != 0 {
		t.Fatalf("second import changed things: %v", notes)
	}

	if _, _, err := ImportSettings(dst, []byte(`{"x":1}`), []string{PartOptions}, "", nil); err == nil {
		t.Fatal("accepted a file that is not a settings file")
	}
}
