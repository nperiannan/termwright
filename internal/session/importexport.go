package session

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/termwright/termwright/internal/sessionfile"
)

// ImportResult summarises an import.
type ImportResult struct {
	Imported []string `json:"imported"`
	Renamed  []string `json:"renamed"`
	Skipped  []string `json:"skipped"`
	Errors   []string `json:"errors"`
	// PasswordsImported counts saved SecureCRT passwords moved to the OS keychain.
	PasswordsImported int      `json:"passwordsImported"`
	PasswordErrors    []string `json:"passwordErrors"`
	// Passwords maps imported session paths to their decrypted passwords; the
	// caller stores them in the OS keychain (never written to session files).
	Passwords map[string]string `json:"-"`
	// Paths maps each session path of the source to the path it has now
	// (renamed, or the existing one when skipped).
	Paths map[string]string `json:"-"`
	// Settings lists what was taken from the export's settings file.
	Settings []string `json:"settings"`
}

// ImportOptions control conflict handling.
type ImportOptions struct {
	TargetFolder string `json:"targetFolder"`
	// Conflict: "rename" (default), "overwrite" or "skip".
	Conflict string `json:"conflict"`
	// IncludeDefault also imports the root Default session as the template.
	IncludeDefault bool `json:"includeDefault"`
	// ConfigPassphrase is SecureCRT's configuration passphrase, needed when
	// the saved passwords were protected by one ("" = none).
	ConfigPassphrase string `json:"configPassphrase"`
	// Settings are the parts of a Termwright export's settings to import
	// ("logins", "library", "options"); empty = sessions only.
	Settings []string `json:"settings"`
}

// Import detects the source type (SecureCRT directory, .zip, .xml or .ini; PuTTY .reg or
// session files; MobaXterm .mxtsessions; OpenSSH config) and imports it.
func (s *Store) Import(src string, opt ImportOptions) (*ImportResult, error) {
	defer s.changed()
	st, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		if res, ok := s.importPuTTYDir(src, opt); ok {
			return res, nil
		}
		return s.importFS(os.DirFS(src), opt)
	}
	lower := strings.ToLower(src)
	if !strings.HasSuffix(lower, ".zip") && !strings.HasSuffix(lower, ".xml") && st.Size() < 64<<20 {
		// PuTTY (.reg, session files) and MobaXterm (.mxtsessions, MobaXterm.ini) are recognised by content
		if data, err := os.ReadFile(src); err == nil {
			if isMRemoteNG(data) {
				return s.importMRemoteNG(data, opt)
			}
			if res, ok := s.importThirdParty(src, data, opt); ok {
				return res, nil
			}
		}
	}
	switch {
	case strings.HasSuffix(lower, ".zip"):
		zr, err := zip.OpenReader(src)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return s.importFS(zr, opt)
	case strings.HasSuffix(lower, ".xml"):
		if data, err := os.ReadFile(src); err == nil {
			if isMRemoteNG(data) {
				return s.importMRemoteNG(data, opt)
			}
			if isSuperPuTTY(data) {
				return s.importSuperPuTTY(data, opt)
			}
		}
		fh, err := os.Open(src)
		if err != nil {
			return nil, err
		}
		defer fh.Close()
		sessions, err := sessionfile.ParseXML(fh)
		if err != nil {
			return nil, err
		}
		res := &ImportResult{}
		for p, f := range sessions {
			s.importOne(p, f, opt, res)
		}
		s.remapJumps(res)
		return res, nil
	case strings.HasSuffix(lower, ".ini"):
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, err
		}
		f, err := sessionfile.Parse(data)
		if err != nil {
			return nil, err
		}
		res := &ImportResult{}
		s.importOne(strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)), f, opt, res)
		return res, nil
	default:
		return s.ImportSSHConfig(src, opt)
	}
}

// findSessionsRoot locates the "Sessions" directory inside a config tree.
func findSessionsRoot(fsys fs.FS) string {
	for _, c := range []string{"Config/Sessions", "Sessions", "VanDyke/Config/Sessions"} {
		if st, err := fs.Stat(fsys, c); err == nil && st.IsDir() {
			return c
		}
	}
	// search one level deeper (zip files usually contain a top folder)
	entries, _ := fs.ReadDir(fsys, ".")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, c := range []string{"Config/Sessions", "Sessions"} {
			p := path.Join(e.Name(), c)
			if st, err := fs.Stat(fsys, p); err == nil && st.IsDir() {
				return p
			}
		}
	}
	return "."
}

func (s *Store) importFS(fsys fs.FS, opt ImportOptions) (*ImportResult, error) {
	root := findSessionsRoot(fsys)
	res := &ImportResult{}
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			return nil
		}
		if d.IsDir() || !strings.EqualFold(path.Ext(p), ext) || strings.EqualFold(d.Name(), folderDataFile) {
			return nil
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		rel = strings.TrimSuffix(rel, path.Ext(rel))
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", rel, err))
			return nil
		}
		f, err := sessionfile.Parse(data)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", rel, err))
			return nil
		}
		s.importOne(rel, f, opt, res)
		return nil
	})
	s.remapJumps(res)
	return res, err
}

// remapJumps points the jump host of every imported session at the imported
// copy of that jump session, wherever it landed (an import into a folder, a
// renamed copy). A jump host that is not part of the import keeps its path: it
// is a session of the store the sessions were imported into.
func (s *Store) remapJumps(res *ImportResult) {
	if len(res.Paths) == 0 || len(res.Imported) == 0 {
		return
	}
	imported := map[string]bool{}
	for _, p := range res.Imported {
		imported[p] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, dest := range res.Paths {
		if !imported[dest] {
			continue
		}
		f, err := s.readFile(dest)
		if err != nil {
			continue
		}
		fw := f.Str(kFirewall, "")
		if !strings.HasPrefix(fw, "Session:") {
			continue
		}
		if np, ok := res.Paths[strings.TrimPrefix(fw, "Session:")]; ok && np != "" && "Session:"+np != fw {
			f.SetStr(kFirewall, "Session:"+np)
			if err := s.writeFile(dest, f); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: the jump host could not be updated: %v", dest, err))
			}
		}
	}
}

// importOne writes one imported session and returns its path ("" when it was
// skipped or failed).
func (s *Store) importOne(rel string, f *sessionfile.File, opt ImportOptions, res *ImportResult) string {
	if strings.EqualFold(rel, defaultFile) {
		if !opt.IncludeDefault {
			res.Skipped = append(res.Skipped, rel)
			return ""
		}
		s.mu.Lock()
		err := s.writeFile(defaultFile, f)
		s.mu.Unlock()
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			return ""
		}
		res.Imported = append(res.Imported, rel)
		return rel
	}
	dest, err := cleanPath(JoinPath(opt.TargetFolder, rel))
	if err != nil || dest == "" {
		res.Errors = append(res.Errors, fmt.Sprintf("%s: invalid name", rel))
		return ""
	}
	if res.Paths == nil {
		res.Paths = map[string]string{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.fsPath(dest) + ext); err == nil {
		switch opt.Conflict {
		case "skip":
			res.Skipped = append(res.Skipped, dest)
			res.Paths[rel] = dest
			return ""
		case "overwrite":
		default:
			dest = s.uniquePath(dest)
			res.Renamed = append(res.Renamed, dest)
		}
	}
	res.Paths[rel] = dest
	for dir := parentOf(dest); dir != ""; dir = parentOf(dir) {
		s.updateFolderData(parentOf(dir), "Folder List", baseOf(dir), true)
		if err := os.MkdirAll(s.fsPath(dir), 0o700); err != nil {
			res.Errors = append(res.Errors, err.Error())
			return ""
		}
	}
	pw, hasPw, pwErr := sessionfile.SavedPassword(f, opt.ConfigPassphrase)
	if hasPw {
		// the password goes to the OS keychain, not into the session file
		f.Delete(sessionfile.PasswordV2Key)
		f.Delete(sessionfile.PasswordV1Key)
		setOptBool(f, kSavePassword, true)
	}
	if err := s.writeFile(dest, f); err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", dest, err))
		return ""
	}
	res.Imported = append(res.Imported, dest)
	switch {
	case hasPw:
		if res.Passwords == nil {
			res.Passwords = map[string]string{}
		}
		res.Passwords[dest] = pw
	case pwErr != nil:
		res.PasswordErrors = append(res.PasswordErrors, fmt.Sprintf("%s: %v", dest, pwErr))
	}
	return dest
}

// ImportSSHConfig imports Host entries from an OpenSSH client config file.
func (s *Store) ImportSSHConfig(file string, opt ImportOptions) (*ImportResult, error) {
	defer s.changed()
	fh, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	res := &ImportResult{}
	var cur *Session
	flush := func() {
		if cur != nil && cur.Hostname != "" {
			f := sessionfile.NewFile()
			cur.Apply(f)
			s.importOne(cur.Name, f, opt, res)
		}
		cur = nil
	}
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, " ")
		if !ok {
			key, val, _ = strings.Cut(line, "=")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(val), "=")), `"`)
		switch key {
		case "host":
			flush()
			if strings.ContainsAny(val, "*?! ") {
				continue // patterns are not sessions
			}
			n := Defaults()
			n.Name, n.Hostname = val, val
			cur = &n
		case "hostname":
			if cur != nil {
				cur.Hostname = val
			}
		case "user":
			if cur != nil {
				cur.Username = val
			}
		case "port":
			if cur != nil {
				if p, err := strconv.Atoi(val); err == nil {
					cur.Port = p
				}
			}
		case "identityfile":
			if cur != nil {
				if strings.HasPrefix(val, "~") {
					if h, err := os.UserHomeDir(); err == nil {
						val = filepath.Join(h, val[1:])
					}
				}
				cur.IdentityFile = val
			}
		}
	}
	flush()
	return res, sc.Err()
}

// Export writes the selected sessions (all when paths is empty).
// format is "dir", "zip" or "xml". For "dir" dest is a directory and the
// files are written to dest/Sessions/...
func (s *Store) Export(paths []string, dest, format string) (int, error) {
	return s.ExportWith(paths, dest, format, nil)
}

// ExportWith exports like Export and adds files next to the Sessions folder
// (zip and folder only), e.g. Termwright's settings.
func (s *Store) ExportWith(paths []string, dest, format string, extra map[string][]byte) (int, error) {
	list, err := s.AllSessions(paths)
	if err != nil {
		return 0, err
	}
	files := map[string]*sessionfile.File{}
	for _, p := range list {
		f, err := s.ReadRaw(p)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", p, err)
		}
		files[p] = f
	}
	switch format {
	case "xml":
		var buf bytes.Buffer
		if err := sessionfile.WriteXML(&buf, sessionfile.XMLSessions(files)); err != nil {
			return 0, err
		}
		return len(files), os.WriteFile(dest, buf.Bytes(), 0o600)
	case "zip":
		out, err := os.Create(dest)
		if err != nil {
			return 0, err
		}
		defer out.Close()
		zw := zip.NewWriter(out)
		err = writeTree(files, func(name string, data []byte) error {
			w, err := zw.Create("Sessions/" + name)
			if err != nil {
				return err
			}
			_, err = w.Write(data)
			return err
		})
		if err != nil {
			return 0, err
		}
		for name, data := range extra {
			w, err := zw.Create(name)
			if err != nil {
				return 0, err
			}
			if _, err := w.Write(data); err != nil {
				return 0, err
			}
		}
		return len(files), zw.Close()
	default:
		base := filepath.Join(dest, "Sessions")
		err := writeTree(files, func(name string, data []byte) error {
			fp := filepath.Join(base, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(fp), 0o700); err != nil {
				return err
			}
			return os.WriteFile(fp, data, 0o600)
		})
		if err != nil {
			return 0, err
		}
		for name, data := range extra {
			if err := os.WriteFile(filepath.Join(dest, filepath.FromSlash(name)), data, 0o600); err != nil {
				return 0, err
			}
		}
		return len(files), nil
	}
}

// ReadExportFile returns a file stored next to the Sessions folder of a
// Termwright export (a .zip, the export folder or its Sessions folder).
func ReadExportFile(src, name string) ([]byte, bool) {
	st, err := os.Stat(src)
	if err != nil {
		return nil, false
	}
	if st.IsDir() {
		for _, p := range []string{filepath.Join(src, name), filepath.Join(filepath.Dir(src), name)} {
			if data, err := os.ReadFile(p); err == nil {
				return data, true
			}
		}
		return nil, false
	}
	if !strings.HasSuffix(strings.ToLower(src), ".zip") {
		return nil, false
	}
	zr, err := zip.OpenReader(src)
	if err != nil {
		return nil, false
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == name || (strings.HasSuffix(f.Name, "/"+name) && strings.Count(f.Name, "/") == 1) {
			rc, err := f.Open()
			if err != nil {
				return nil, false
			}
			defer rc.Close()
			var b bytes.Buffer
			if _, err := b.ReadFrom(io.LimitReader(rc, 16<<20)); err != nil {
				return nil, false
			}
			return b.Bytes(), true
		}
	}
	return nil, false
}

// writeTree emits session files plus a folder index for every folder.
func writeTree(files map[string]*sessionfile.File, put func(name string, data []byte) error) error {
	type idx struct{ folders, sessions []string }
	folders := map[string]*idx{"": {}}
	get := func(p string) *idx {
		if folders[p] == nil {
			folders[p] = &idx{}
		}
		return folders[p]
	}
	for p, f := range files {
		if err := put(p+ext, f.Bytes()); err != nil {
			return err
		}
		dir := parentOf(p)
		get(dir).sessions = append(get(dir).sessions, baseOf(p))
		for d := dir; d != ""; d = parentOf(d) {
			parent := get(parentOf(d))
			if !contains(parent.folders, baseOf(d)) {
				parent.folders = append(parent.folders, baseOf(d))
			}
		}
	}
	for dir, ix := range folders {
		f := sessionfile.NewFile()
		f.SetList("Folder List", nonNil(ix.folders))
		f.SetList("Session List", nonNil(ix.sessions))
		name := folderDataFile
		if dir != "" {
			name = dir + "/" + folderDataFile
		}
		if err := put(name, f.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

func nonNil(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}
