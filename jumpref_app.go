package main

import (
	"strings"

	"github.com/termwright/termwright/internal/session"
)

// JumpUse is something that uses a session as its jump host: another session,
// or a folder whose default is that jump host.
type JumpUse struct {
	Path     string `json:"path"`     // the session's path, or the folder's path for a folder default
	Name     string `json:"name"`     // what to show
	Protocol string `json:"protocol"` // SSH2, RDP, VNC, … ("" for a folder default)
	Folder   bool   `json:"folder"`   // a folder default, not a session
	Jump     string `json:"jump"`     // the jump host it uses (one of the asked paths)
}

// JumpUsers lists what uses any of the sessions as its jump host - the
// sessions that name it in "Jump host", and the folders whose default is it -
// so the Session Manager can ask before the jump host is moved, renamed or
// deleted. With deep, the users of the users too (a deleted jump host takes
// the sessions behind it down with it). Sessions named in except are left out.
func (a *App) JumpUsers(paths []string, deep bool, except []string) []JumpUse {
	if a.ready() != nil {
		return nil
	}
	tree, err := a.store.Tree()
	if err != nil {
		return nil
	}
	type ref struct{ path, name, proto, jump string }
	var refs []ref
	var walk func(n *session.Node)
	walk = func(n *session.Node) {
		for _, c := range n.Children {
			if c.IsFolder {
				walk(c)
			} else if c.Jump != "" {
				refs = append(refs, ref{c.Path, c.Name, c.Protocol, c.Jump})
			}
		}
	}
	walk(tree)
	skip := map[string]bool{}
	for _, p := range except {
		skip[p] = true
	}
	want := map[string]bool{}
	for _, p := range paths {
		want[p] = true
	}
	var out []JumpUse
	seen := map[string]bool{}
	for grew := true; grew; {
		grew = false
		for _, r := range refs {
			if want[r.jump] && !seen[r.path] && !skip[r.path] {
				seen[r.path] = true
				out = append(out, JumpUse{Path: r.path, Name: r.path, Protocol: r.proto, Jump: r.jump})
				if deep {
					want[r.path] = true
					grew = true
				}
			}
		}
	}
	for _, d := range a.cfg.Get().FolderDefaults {
		if d.JumpSession != "" && want[d.JumpSession] {
			out = append(out, JumpUse{Path: strings.Trim(d.Folder, "/"), Name: strings.Trim(d.Folder, "/"), Folder: true, Jump: d.JumpSession})
		}
	}
	return out
}

// RepointJumps points everything that uses a jump host at its new path
// (moves: old path → new path; "" clears it). It returns how many sessions
// and folder defaults it changed.
func (a *App) RepointJumps(moves map[string]string) (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	n := 0
	olds := make([]string, 0, len(moves))
	for o := range moves {
		olds = append(olds, o)
	}
	for _, u := range a.JumpUsers(olds, false, nil) {
		if u.Folder {
			continue
		}
		s, err := a.store.Get(u.Path)
		if err != nil {
			continue
		}
		s.JumpSession = moves[u.Jump]
		if _, err := a.store.Save(s, u.Path); err != nil {
			return n, err
		}
		n++
	}
	st := a.cfg.Get()
	changed := false
	for i := range st.FolderDefaults {
		if to, ok := moves[st.FolderDefaults[i].JumpSession]; ok && st.FolderDefaults[i].JumpSession != "" {
			st.FolderDefaults[i].JumpSession = to
			changed = true
			n++
		}
	}
	if changed {
		if err := a.cfg.Save(st); err != nil {
			return n, err
		}
	}
	return n, nil
}

// ClearJumpDefaults removes deleted jump hosts from the folder defaults that
// named them (sessions are left as they are).
func (a *App) ClearJumpDefaults(paths []string) (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	gone := map[string]bool{}
	for _, p := range paths {
		gone[p] = true
	}
	st := a.cfg.Get()
	n := 0
	for i := range st.FolderDefaults {
		if gone[st.FolderDefaults[i].JumpSession] {
			st.FolderDefaults[i].JumpSession = ""
			n++
		}
	}
	if n > 0 {
		if err := a.cfg.Save(st); err != nil {
			return 0, err
		}
	}
	return n, nil
}
