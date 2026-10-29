package config

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/termwright/termwright/internal/session"
)

// Settings travel with the sessions in Termwright's own exports (zip and
// folder) as SettingsFileName. Three parts can be imported separately:
//
//	logins  - device types, accounts (names and usernames) and Folder Defaults
//	library - command library, checklists, Run and Collect, backup jobs,
//	          alerts, buttons, keyboard shortcuts, workspaces, SFTP bookmarks,
//	          favourites, keyword sets and colour schemes
//	options - the remaining Global Options (appearance, terminal, behaviour)
//
// Passwords are never in it (Passwords & Accounts → Export moves them), and
// neither is what belongs to one computer: folders on disk, the master
// password, open tabs, recent sessions, window position.

// SettingsFileName is the settings file inside an export.
const SettingsFileName = "termwright-settings.json"

const settingsFormat = "termwright-settings"

// Settings parts.
const (
	PartLogins  = "logins"
	PartLibrary = "library"
	PartOptions = "options"
)

// never exported: they belong to this computer, are secrets, or are history
var localKeys = map[string]bool{
	"sessionsDir": true, "logDir": true, "downloadDir": true, "backupDir": true, "collectDir": true,
	"checklistDir": true, "recordDir": true, "pythonPath": true, "smartCardLibraries": true,
	"masterHash": true, "keyPassphrases": true, "openTabs": true, "recentSessions": true,
	"lastConnected": true, "deviceFacts": true, "window": true, "welcomeSeen": true,
	"themeFollowsSystem": true, "highlightOptIn": true, "modernLayout": true, "factRulesAdded": true,
}

var loginKeys = map[string]bool{"deviceTypes": true, "credentials": true, "folderDefaults": true}

var libraryKeys = map[string]bool{
	"snippets": true, "checklists": true, "collections": true, "backupJobs": true, "alerts": true,
	"buttons": true, "keyMap": true, "workspaces": true, "bookmarks": true, "favorites": true,
	"keywordSets": true, "customSchemes": true,
}

type settingsFile struct {
	Format   string                     `json:"format"`
	Version  int                        `json:"version"`
	App      string                     `json:"app"`
	Settings map[string]json.RawMessage `json:"settings"`
}

// ExportSettings returns the settings file written into an export.
func ExportSettings(s Settings, appVersion string) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for k := range localKeys {
		delete(m, k)
	}
	return json.MarshalIndent(settingsFile{Format: settingsFormat, Version: 1, App: appVersion, Settings: m}, "", "  ")
}

// SettingsSummary says what an export's settings file holds.
type SettingsSummary struct {
	App            string `json:"app"`
	DeviceTypes    int    `json:"deviceTypes"`
	Accounts       int    `json:"accounts"`
	FolderDefaults int    `json:"folderDefaults"`
	Library        int    `json:"library"` // commands, checklists, jobs, alerts, buttons, shortcuts, workspaces …
	Options        int    `json:"options"` // global options in the file
}

func parseSettingsFile(data []byte) (*settingsFile, Settings, error) {
	var f settingsFile
	if err := json.Unmarshal(data, &f); err != nil || f.Format != settingsFormat || f.Settings == nil {
		return nil, Settings{}, errors.New("not a Termwright settings file")
	}
	for k := range localKeys {
		delete(f.Settings, k)
	}
	raw, _ := json.Marshal(f.Settings)
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, Settings{}, err
	}
	return &f, s, nil
}

// SummarizeSettings describes a settings file for the Import dialog.
func SummarizeSettings(data []byte) (SettingsSummary, error) {
	f, s, err := parseSettingsFile(data)
	if err != nil {
		return SettingsSummary{}, err
	}
	sum := SettingsSummary{App: f.App, DeviceTypes: len(s.DeviceTypes), Accounts: len(s.Credentials), FolderDefaults: len(s.FolderDefaults)}
	sum.Library = len(s.Snippets) + len(s.Checklists) + len(s.Collections) + len(s.BackupJobs) + len(s.Alerts) +
		len(s.Buttons) + len(s.KeyMap) + len(s.Workspaces) + len(s.Bookmarks) + len(s.Favorites) +
		len(s.KeywordSets) + len(s.CustomSchemes)
	for k := range f.Settings {
		if !loginKeys[k] && !libraryKeys[k] {
			sum.Options++
		}
	}
	return sum, nil
}

// ImportSettings merges the chosen parts of a settings file into cur and
// says what changed. Named items (device types, accounts, commands …) with
// the same name are replaced, new ones are added and the others are kept.
// mapPath turns a session path of the export into the path it was imported
// at; folder is the folder the sessions were imported into.
func ImportSettings(cur Settings, data []byte, parts []string, folder string, mapPath func(string) string) (Settings, []string, error) {
	f, in, err := parseSettingsFile(data)
	if err != nil {
		return cur, nil, err
	}
	want := map[string]bool{}
	for _, p := range parts {
		want[p] = true
	}
	if mapPath == nil {
		mapPath = func(p string) string { return session.JoinPath(folder, p) }
	}
	mapTarget := func(t string) string { // folders end with "/"
		if strings.HasSuffix(t, "/") {
			return session.JoinPath(folder, strings.TrimSuffix(t, "/")) + "/"
		}
		return mapPath(t)
	}
	mapTargets := func(l []string) []string {
		out := make([]string, 0, len(l))
		for _, t := range l {
			out = append(out, mapTarget(t))
		}
		return out
	}
	var notes []string
	note := func(what string, added, updated int) {
		if added+updated == 0 {
			return
		}
		var b []string
		if added > 0 {
			b = append(b, strconv.Itoa(added)+" added")
		}
		if updated > 0 {
			b = append(b, strconv.Itoa(updated)+" updated")
		}
		notes = append(notes, what+": "+strings.Join(b, ", "))
	}

	if want[PartLogins] {
		var a, u int
		cur.DeviceTypes, a, u = mergeBy(cur.DeviceTypes, in.DeviceTypes, func(d session.DeviceType) string { return strings.ToLower(d.Name) })
		note("Device types", a, u)
		cur.Credentials, a, u = mergeBy(cur.Credentials, in.Credentials, func(c Credential) string { return c.Name })
		note("Accounts", a, u)
		fds := make([]FolderDefault, 0, len(in.FolderDefaults))
		for _, d := range in.FolderDefaults {
			d.Folder = session.JoinPath(folder, d.Folder)
			if d.JumpSession != "" {
				d.JumpSession = mapPath(d.JumpSession)
			}
			fds = append(fds, d)
		}
		cur.FolderDefaults, a, u = mergeBy(cur.FolderDefaults, fds, func(d FolderDefault) string { return d.Folder })
		note("Folder Defaults", a, u)
	}

	if want[PartLibrary] {
		var a, u int
		cur.Snippets, a, u = mergeBy(cur.Snippets, in.Snippets, func(s Snippet) string { return s.Folder + "\x00" + s.Name })
		note("Command library", a, u)
		for i := range in.Checklists {
			in.Checklists[i].Targets = mapTargets(in.Checklists[i].Targets)
		}
		cur.Checklists, a, u = mergeBy(cur.Checklists, in.Checklists, func(c Checklist) string { return c.Name })
		note("Checklists", a, u)
		for i := range in.Collections {
			in.Collections[i].Targets = mapTargets(in.Collections[i].Targets)
		}
		cur.Collections, a, u = mergeBy(cur.Collections, in.Collections, func(c Collection) string { return c.Name })
		note("Run and Collect", a, u)
		for i := range in.BackupJobs {
			in.BackupJobs[i].Targets = mapTargets(in.BackupJobs[i].Targets)
		}
		cur.BackupJobs, a, u = mergeBy(cur.BackupJobs, in.BackupJobs, func(j BackupJob) string { return j.Name })
		note("Backup jobs", a, u)
		cur.Alerts, a, u = mergeBy(cur.Alerts, in.Alerts, func(x Alert) string { return x.Pattern })
		note("Alerts", a, u)
		cur.Buttons, a, u = mergeBy(cur.Buttons, in.Buttons, func(b Button) string { return b.Label })
		note("Buttons", a, u)
		cur.KeyMap, a, u = mergeBy(cur.KeyMap, in.KeyMap, func(k KeyBinding) string { return strings.ToLower(k.Keys) })
		note("Keyboard shortcuts", a, u)
		for i := range in.Workspaces {
			for j := range in.Workspaces[i].Tabs {
				in.Workspaces[i].Tabs[j].Session = mapPath(in.Workspaces[i].Tabs[j].Session)
			}
		}
		cur.Workspaces, a, u = mergeBy(cur.Workspaces, in.Workspaces, func(w Workspace) string { return w.Name })
		note("Workspaces", a, u)
		for i := range in.Bookmarks {
			if in.Bookmarks[i].Session != "" {
				in.Bookmarks[i].Session = mapPath(in.Bookmarks[i].Session)
			}
		}
		cur.Bookmarks, a, u = mergeBy(cur.Bookmarks, in.Bookmarks, func(b Bookmark) string { return b.Side + "\x00" + b.Session + "\x00" + b.Name })
		note("SFTP bookmarks", a, u)
		cur.Favorites, a, u = mergeBy(cur.Favorites, mapTargets(in.Favorites), func(p string) string { return p })
		note("Favourites", a, 0)
		cur.KeywordSets, a, u = mergeBy(cur.KeywordSets, in.KeywordSets, func(k KeywordSet) string { return k.Name })
		note("Keyword sets", a, u)
		cur.CustomSchemes, a, u = mergeBy(cur.CustomSchemes, in.CustomSchemes, func(c ColorScheme) string { return c.Name })
		note("Colour schemes", a, u)
	}

	if want[PartOptions] {
		var keys []string
		for k := range f.Settings {
			if !loginKeys[k] && !libraryKeys[k] {
				keys = append(keys, k)
			}
		}
		if len(keys) > 0 {
			sort.Strings(keys)
			raw, err := json.Marshal(cur)
			if err != nil {
				return cur, notes, err
			}
			m := map[string]json.RawMessage{}
			if err := json.Unmarshal(raw, &m); err != nil {
				return cur, notes, err
			}
			changed := 0
			for _, k := range keys {
				if string(m[k]) != string(f.Settings[k]) {
					changed++
				}
				m[k] = f.Settings[k]
			}
			raw, _ = json.Marshal(m)
			var next Settings
			if err := json.Unmarshal(raw, &next); err != nil {
				return cur, notes, err
			}
			cur = next
			notes = append(notes, "Global Options: "+strconv.Itoa(changed)+" changed")
		}
	}
	return cur, notes, nil
}

// mergeBy replaces items of cur that have the key of an item of in, appends
// the new ones, and counts both (an identical item is not counted).
func mergeBy[T any](cur, in []T, key func(T) string) (out []T, added, updated int) {
	out = append([]T{}, cur...)
	at := map[string]int{}
	for i, c := range out {
		at[key(c)] = i
	}
	for _, x := range in {
		k := key(x)
		if i, ok := at[k]; ok {
			a, _ := json.Marshal(out[i])
			b, _ := json.Marshal(x)
			if string(a) != string(b) {
				out[i] = x
				updated++
			}
			continue
		}
		at[k] = len(out)
		out = append(out, x)
		added++
	}
	return out, added, updated
}
