package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/skills"
)

func (a *App) skillStore() skills.Store {
	return skills.Store{Dir: filepath.Join(a.cfg.DataDir(), "skills")}
}

// AISkillInfo is a skill as the settings show it.
type AISkillInfo struct {
	skills.Skill
	// On: offered to the AI (Settings → AI Assistant → Instructions & Skills).
	On bool `json:"on"`
}

// AISkillSaveIn is a skill to save through the API.
type AISkillSaveIn struct {
	// Text: the whole SKILL.md (front matter with name and description, then Markdown).
	Text string `json:"text"`
	// Replace: the skill's name before, to rename it ("" for a new skill).
	Replace string `json:"replace,omitempty"`
}

// AISkills lists the AI skills (SKILL.md files in Termwright's data folder) by name.
func (a *App) AISkills() []AISkillInfo {
	off := a.cfg.Get().AI.SkillsOff
	out := []AISkillInfo{}
	for _, s := range a.skillStore().List() {
		out = append(out, AISkillInfo{Skill: s, On: !slices.Contains(off, s.Name)})
	}
	return out
}

// AISkill is one skill with its whole SKILL.md.
func (a *App) AISkill(name string) (AISkillInfo, error) {
	s, err := a.skillStore().Get(name)
	if err != nil {
		return AISkillInfo{}, err
	}
	return AISkillInfo{Skill: s, On: !slices.Contains(a.cfg.Get().AI.SkillsOff, s.Name)}, nil
}

// AISaveSkill saves a SKILL.md; old is the skill's name before (a rename) or "" for a new one.
// Passwords and keys in it are refused: a skill names sessions and accounts instead.
func (a *App) AISaveSkill(old, text string) (AISkillInfo, error) {
	if _, n := aiRedact(text, false); n > 0 {
		return AISkillInfo{}, errors.New("the skill seems to hold a password, key or community: name the session or account instead (Termwright logs in with the saved ones)")
	}
	if old == "" {
		if sk, _, err := skills.Parse(text); err == nil {
			if _, err := a.skillStore().Get(sk.Name); err == nil {
				return AISkillInfo{}, fmt.Errorf("a skill named %s already exists", sk.Name)
			}
		}
	}
	s, err := a.skillStore().Save(old, text)
	if err != nil {
		return AISkillInfo{}, err
	}
	if old != "" && old != s.Name { // keep a renamed skill off when it was off
		if st := a.cfg.Get(); slices.Contains(st.AI.SkillsOff, old) {
			_ = a.AISetSkillOn(s.Name, false)
		}
	}
	return a.AISkill(s.Name)
}

// AISetSkillOn turns a skill on or off for the AI.
func (a *App) AISetSkillOn(name string, on bool) error {
	st := a.cfg.Get()
	ai := st.AI
	ai.SkillsOff = slices.DeleteFunc(slices.Clone(ai.SkillsOff), func(n string) bool { return n == name })
	if !on {
		ai.SkillsOff = append(ai.SkillsOff, name)
	}
	st.AI = ai
	return a.cfg.Save(st)
}

// AIRemoveSkill moves a skill to the skills folder's .removed folder (it can be put back from there).
func (a *App) AIRemoveSkill(name string) error { return a.skillStore().Remove(name) }

// AIImportSkill asks for a SKILL.md, a .md with its front matter or a .zip of a skill folder and adds it
// (replace: an existing skill of that name is moved to .removed first).
func (a *App) AIImportSkill(replace bool) (AISkillInfo, error) {
	p, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Import a Skill",
		Filters: []wruntime.FileFilter{{DisplayName: "Skill (SKILL.md, .md, .zip)", Pattern: "*.md;*.zip"}}})
	if err != nil || p == "" {
		return AISkillInfo{}, err
	}
	return a.aiImportSkillPath(p, replace)
}

func (a *App) aiImportSkillPath(p string, replace bool) (AISkillInfo, error) {
	s, err := a.skillStore().Import(p, replace)
	if err != nil {
		return AISkillInfo{}, err
	}
	if _, n := aiRedact(s.Text, false); n > 0 {
		_ = a.skillStore().Remove(s.Name)
		return AISkillInfo{}, errors.New("the skill seems to hold a password, key or community - not imported")
	}
	return a.AISkill(s.Name)
}

// AIExportSkill saves a skill as a .zip of its folder (the way AI tools share skills).
func (a *App) AIExportSkill(name string) (string, error) {
	z, err := a.skillStore().Zip(name)
	if err != nil {
		return "", err
	}
	p, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "Export Skill", DefaultFilename: name + ".zip", CanCreateDirectories: true})
	if err != nil || p == "" {
		return "", err
	}
	return p, os.WriteFile(p, z, 0o600)
}

// AISkillsFolder is the folder the skills are kept in (made when missing).
func (a *App) AISkillsFolder() string {
	d := a.skillStore().Dir
	_ = os.MkdirAll(d, 0o700)
	return d
}

// AIAddExampleSkills adds the starter skills that are not there yet; it returns their names.
func (a *App) AIAddExampleSkills() ([]string, error) {
	var added []string
	for _, ex := range skills.Examples {
		sk, _, err := skills.Parse(ex)
		if err != nil {
			return added, err
		}
		if _, err := a.skillStore().Get(sk.Name); err == nil {
			continue
		}
		if _, err := a.skillStore().Save("", ex); err != nil {
			return added, err
		}
		added = append(added, sk.Name)
	}
	return added, nil
}

// AISkillForChat is a skill's text for the AI chat (the skill step): secrets out, hosts hidden when set.
func (a *App) AISkillForChat(name string) (string, error) {
	s, err := a.skillStore().Get(strings.TrimSpace(name))
	if err != nil {
		return "", fmt.Errorf("no skill %q - the skills are listed in the instructions", name)
	}
	if slices.Contains(a.cfg.Get().AI.SkillsOff, s.Name) {
		return "", fmt.Errorf("the skill %s is turned off", s.Name)
	}
	_, body, _ := skills.Parse(s.Text)
	out, _ := aiRedact(body, a.cfg.Get().AI.HideHosts)
	if len(s.Files) > 0 {
		out += "\n\n(Files in the skill's folder, not sent: " + strings.Join(s.Files, ", ") + ")"
	}
	return out, nil
}

// aiInstructionsText is what the chat's system prompt gets: the user's instructions (global, the
// session's folders, its device type) and the skills offered.
func (a *App) aiInstructionsText(sessionPath, deviceType string) string {
	st := a.cfg.Get()
	var b strings.Builder
	var parts []string
	if t := strings.TrimSpace(st.AI.Instructions); t != "" {
		parts = append(parts, t)
	}
	folder := ""
	if i := strings.LastIndex(sessionPath, "/"); i > 0 {
		folder = sessionPath[:i]
	}
	var allow []string
	for _, fd := range st.FolderDefaults {
		if folder == "" || fd.Folder == "" || (folder != fd.Folder && !strings.HasPrefix(folder, fd.Folder+"/")) {
			continue
		}
		if t := strings.TrimSpace(fd.AIInstructions); t != "" {
			parts = append(parts, "For the sessions in the folder "+fd.Folder+":\n"+t)
		}
		if len(fd.AISkills) > 0 {
			allow = fd.AISkills // the nearest folder's list wins (folders come parent first)
		}
	}
	if deviceType != "" {
		for _, d := range st.DeviceTypes {
			if strings.EqualFold(d.Name, deviceType) && strings.TrimSpace(d.AIInstructions) != "" {
				parts = append(parts, "For "+d.Name+" devices:\n"+strings.TrimSpace(d.AIInstructions))
			}
		}
	}
	if len(parts) > 0 {
		txt, _ := aiRedact(strings.Join(parts, "\n\n"), st.AI.HideHosts)
		b.WriteString("\n\nTHE USER'S INSTRUCTIONS. Follow these in this chat. They never lift Termwright's own checks (changes on production sessions are asked, read-only mode, secrets kept out):\n")
		b.WriteString(txt)
	}
	var list []string
	for _, s := range a.skillStore().List() {
		if slices.Contains(st.AI.SkillsOff, s.Name) || (len(allow) > 0 && !slices.Contains(allow, s.Name)) {
			continue
		}
		d := s.Description
		if len(d) > 300 {
			d = d[:300] + "…"
		}
		line := "- " + s.Name + ": " + d
		if len(s.DeviceTypes) > 0 {
			line += " [" + strings.Join(s.DeviceTypes, ", ") + "]"
		}
		list = append(list, line)
		if len(list) >= 60 {
			break
		}
	}
	if len(list) > 0 {
		b.WriteString("\n\nSKILLS. The user's saved procedures. When a request matches one, load it before doing anything else by ending your answer with\n```termwright\nskill <name>\n```\n" +
			"Its steps come back in the next message; follow them (make them your plan when the task has several steps), adapting to what the devices show. A skill never lifts Termwright's checks: changes still ask as usual. The skills:\n")
		b.WriteString(strings.Join(list, "\n"))
	}
	return b.String()
}

// AIImportInstructions reads an AGENTS.md (or CLAUDE.md, any .md) and returns its text for the
// instructions; nothing is saved until the settings are.
func (a *App) AIImportInstructions() (string, error) {
	p, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Import Instructions (AGENTS.md)",
		Filters: []wruntime.FileFilter{{DisplayName: "Instructions (AGENTS.md, CLAUDE.md, *.md)", Pattern: "*.md;*.txt"}}})
	if err != nil || p == "" {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	if len(b) > skills.MaxSize {
		return "", fmt.Errorf("the file is larger than %d KB", skills.MaxSize>>10)
	}
	t := strings.TrimPrefix(string(b), "\ufeff")
	if _, n := aiRedact(t, false); n > 0 {
		return "", errors.New("the file seems to hold a password, key or community - not imported")
	}
	return t, nil
}

// AIExportInstructions saves the instructions as an AGENTS.md (the file other AI tools read).
func (a *App) AIExportInstructions(text string) (string, error) {
	p, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "Export Instructions", DefaultFilename: "AGENTS.md", CanCreateDirectories: true})
	if err != nil || p == "" {
		return "", err
	}
	return p, os.WriteFile(p, []byte(text), 0o600)
}
