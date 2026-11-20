package app

import (
	"context"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

// Skills save (not with a password in them), the chat gets the instructions of the session's folder
// and device type and the skills offered there, a skill loads for the chat and the MCP tool lists them.
func TestAISkillsAndInstructions(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	ss, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: ss}
	if added, err := a.AIAddExampleSkills(); err != nil || len(added) != 3 {
		t.Fatal(err, added)
	}
	if _, err := a.AISaveSkill("", "---\nname: bad\ndescription: x\n---\nconfigure snmp community s3cretcomm rw\nenable password Passw0rd!123\n"); err == nil {
		t.Fatal("saved a password")
	}
	if _, err := a.AISaveSkill("", "---\nname: add-vlan-fabric\ndescription: Add a VLAN with its I-SID on every fabric switch.\ndevice-types: VOSS\n---\n1. vlan create <id> type port-mstprstp 0\n"); err != nil {
		t.Fatal(err)
	}
	st := cfg.Get()
	st.AI.Instructions = "Save the configuration after every change."
	st.FolderDefaults = append(st.FolderDefaults, config.FolderDefault{Folder: "Datacenter", AIInstructions: "Changes only 22:00-02:00.", AISkills: []string{"add-vlan-fabric", "troubleshoot-port"}})
	for i, d := range st.DeviceTypes {
		if d.Name == "VOSS" {
			st.DeviceTypes[i].AIInstructions = "Use I-SIDs 10000-19999."
		}
	}
	if err := cfg.Save(st); err != nil {
		t.Fatal(err)
	}
	txt := a.aiInstructionsText("Datacenter/Core/core-sw1", "VOSS")
	for _, want := range []string{"Save the configuration after every change.", "folder Datacenter:\nChanges only 22:00-02:00.", "For VOSS devices:\nUse I-SIDs 10000-19999.", "- add-vlan-fabric: Add a VLAN", "[VOSS]", "- troubleshoot-port:", "skill <name>"} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in\n%s", want, txt)
		}
	}
	if strings.Contains(txt, "upgrade-exos-firmware") {
		t.Error("the folder's list was not used")
	}
	// elsewhere: every skill that is on
	if err := a.AISetSkillOn("troubleshoot-port", false); err != nil {
		t.Fatal(err)
	}
	other := a.aiInstructionsText("Lab/x", "")
	if !strings.Contains(other, "upgrade-exos-firmware") || strings.Contains(other, "troubleshoot-port") || strings.Contains(other, "Changes only") {
		t.Fatal(other)
	}
	if out, err := a.AISkillForChat("upgrade-exos-firmware"); err != nil || !strings.Contains(out, "download url") || strings.Contains(out, "description:") {
		t.Fatal(err, out)
	}
	if _, err := a.AISkillForChat("troubleshoot-port"); err == nil {
		t.Fatal("a skill turned off was given")
	}
	if out, err := a.mcpCall(context.Background(), "termwright_skills", map[string]any{"action": "get", "name": "add-vlan-fabric"}); err != nil || !strings.Contains(out, "vlan create") {
		t.Fatal(err, out)
	}
	var on int
	for _, s := range a.AISkills() {
		if s.On {
			on++
		}
	}
	if on != 3 {
		t.Fatal(a.AISkills())
	}
}
