package main

import (
	"errors"
	"sort"
	"strings"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/secretref"
	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/session"
)

// Accounts (named credentials): a username and a password (keychain or a
// password manager) that sessions, folders and device types log in with,
// so a password is changed in one place.

// AccountInfo tells where an account is used.
type AccountInfo struct {
	Name        string   `json:"name"`
	Sessions    int      `json:"sessions"`
	Folders     []string `json:"folders"`
	DeviceTypes []string `json:"deviceTypes"`
	Password    string   `json:"password"` // "keychain", "manager" or "" (asked when connecting)
}

// AccountsInfo lists each account with what uses it and where its password is.
func (a *App) AccountsInfo() ([]AccountInfo, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	st := a.cfg.Get()
	idx := map[string]*AccountInfo{}
	var out []*AccountInfo
	for _, c := range st.Credentials {
		ai := &AccountInfo{Name: c.Name, Folders: []string{}, DeviceTypes: []string{}}
		if v, ok := secrets.Lookup("credential:" + c.Name); ok && v != "" {
			ai.Password = "keychain"
			if secretref.IsRef(v) {
				ai.Password = "manager"
			}
		}
		idx[c.Name] = ai
		out = append(out, ai)
	}
	for _, f := range st.FolderDefaults {
		if ai := idx[f.Account]; ai != nil {
			ai.Folders = append(ai.Folders, f.Folder)
		}
	}
	for _, d := range st.DeviceTypes {
		if ai := idx[d.Credential]; ai != nil {
			ai.DeviceTypes = append(ai.DeviceTypes, d.Name)
		}
	}
	paths, _ := a.store.AllSessions(nil)
	for _, p := range paths {
		s, err := a.store.Get(p)
		if err != nil {
			continue
		}
		if name := a.terms.AccountOf(a.withFolderDefaults(s)); name != "" {
			if ai := idx[name]; ai != nil {
				ai.Sessions++
			}
		}
	}
	res := make([]AccountInfo, len(out))
	for i, ai := range out {
		res[i] = *ai
	}
	return res, nil
}

// AccountSessions lists the sessions that log in with an account.
func (a *App) AccountSessions(name string) []string {
	var out []string
	paths, _ := a.store.AllSessions(nil)
	for _, p := range paths {
		if s, err := a.store.Get(p); err == nil && a.terms.AccountOf(a.withFolderDefaults(s)) == name {
			out = append(out, p)
		}
	}
	return out
}

// RenameAccount renames an account everywhere it is used: sessions, logon
// actions, folder defaults, device types and its saved password.
func (a *App) RenameAccount(oldName, newName string) error {
	if err := a.ready(); err != nil {
		return err
	}
	newName = strings.TrimSpace(newName)
	if newName == "" || newName == oldName {
		return nil
	}
	st := a.cfg.Get()
	found := false
	for i := range st.Credentials {
		switch st.Credentials[i].Name {
		case newName:
			return errors.New("an account named " + newName + " already exists")
		case oldName:
			found = true
		}
	}
	if !found {
		return errors.New("no account named " + oldName)
	}
	for i := range st.Credentials {
		if st.Credentials[i].Name == oldName {
			st.Credentials[i].Name = newName
		}
	}
	for i := range st.FolderDefaults {
		if st.FolderDefaults[i].Account == oldName {
			st.FolderDefaults[i].Account = newName
		}
	}
	for i := range st.DeviceTypes {
		d := &st.DeviceTypes[i]
		if d.Credential == oldName {
			d.Credential = newName
		}
		for _, l := range [][]session.LogonAction{d.LoginSteps, d.AfterLogin} {
			for j := range l {
				if l[j].Credential == oldName {
					l[j].Credential = newName
				}
			}
		}
	}
	if v, ok := secrets.Lookup("credential:" + oldName); ok {
		if err := secrets.Set("credential:"+newName, v); err != nil {
			return err
		}
		_ = secrets.Delete("credential:" + oldName)
	}
	if err := a.cfg.Save(st); err != nil {
		return err
	}
	paths, _ := a.store.AllSessions(nil)
	for _, p := range paths {
		s, err := a.store.Get(p)
		if err != nil {
			continue
		}
		changed := s.Account == oldName
		if changed {
			s.Account = newName
		}
		for j := range s.LogonActions {
			if s.LogonActions[j].Credential == oldName {
				s.LogonActions[j].Credential, changed = newName, true
			}
		}
		if changed {
			_, _ = a.store.Save(s, p)
		}
	}
	return nil
}

// AccountGroup is a set of sessions with the same own username and the same
// saved password - candidates for one account.
type AccountGroup struct {
	Username string   `json:"username"`
	Paths    []string `json:"paths"`
}

// AccountCandidates finds sessions that log in with their own username and
// saved password and share both with other sessions.
func (a *App) AccountCandidates() []AccountGroup {
	type key struct{ user, pw string }
	groups := map[key][]string{}
	paths, _ := a.store.AllSessions(nil)
	for _, p := range paths {
		s, err := a.store.Get(p)
		if err != nil || s.Username == "" || a.terms.AccountOf(a.withFolderDefaults(s)) != "" {
			continue
		}
		if s.DeviceType != "" && !s.OwnLogin && s.Account != "-" {
			continue // logs in with the device type's login
		}
		pw, ok := secrets.Lookup(p)
		if !ok || pw == "" {
			continue
		}
		k := key{s.Username, pw}
		groups[k] = append(groups[k], p)
	}
	var out []AccountGroup
	for k, ps := range groups {
		if len(ps) > 1 {
			sort.Strings(ps)
			out = append(out, AccountGroup{Username: k.user, Paths: ps})
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i].Paths) > len(out[j].Paths) })
	return out
}

// ConvertToAccount makes the sessions log in with an account: it creates
// the account (username and saved password of the first session) when it
// does not exist, and removes the sessions' own username and password.
func (a *App) ConvertToAccount(name string, paths []string) error {
	if err := a.ready(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(paths) == 0 {
		return errors.New("choose an account name and sessions")
	}
	first, err := a.store.Get(paths[0])
	if err != nil {
		return err
	}
	st := a.cfg.Get()
	exists := false
	for _, c := range st.Credentials {
		if c.Name == name {
			exists = true
		}
	}
	if !exists {
		st.Credentials = append(st.Credentials, config.Credential{Name: name, Username: first.Username})
		if err := a.cfg.Save(st); err != nil {
			return err
		}
		if pw, ok := secrets.Lookup(paths[0]); ok && pw != "" {
			if err := a.SetCredentialPassword(name, pw); err != nil {
				return err
			}
		}
	}
	for _, p := range paths {
		s, err := a.store.Get(p)
		if err != nil {
			return err
		}
		s.Account, s.Username, s.OwnLogin = name, "", false
		if _, err := a.store.Save(s, p); err != nil {
			return err
		}
		_ = secrets.Delete(p)
	}
	return nil
}
