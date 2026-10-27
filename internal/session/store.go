package session

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/termwright/termwright/internal/sessionfile"
)

const (
	folderDataFile = "__FolderData__.ini"
	defaultFile    = "Default"
	ext            = ".ini"
)

// Node is one entry in the session tree.
type Node struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	IsFolder    bool    `json:"isFolder"`
	Protocol    string  `json:"protocol,omitempty"`
	Hostname    string  `json:"hostname,omitempty"`
	Username    string  `json:"username,omitempty"`
	Port        int     `json:"port,omitempty"`
	Description string  `json:"description,omitempty"`
	HopTarget   string  `json:"hopTarget,omitempty"` // user@host of an "ssh … host" remote command
	OpenAs      string  `json:"openAs,omitempty"`    // "sftp" when the session opens as an SFTP browser
	DeviceType  string  `json:"deviceType,omitempty"`
	Environment string  `json:"environment,omitempty"`
	Jump        string  `json:"jump,omitempty"`  // jump session name
	Proxy       bool    `json:"proxy,omitempty"` // connects through a SOCKS/HTTP proxy
	MAC         string  `json:"mac,omitempty"`
	Console     string  `json:"console,omitempty"` // console port: "<console server path> port <n>"
	Children    []*Node `json:"children,omitempty"`
}

// Store is a directory tree of session .ini files laid out like the
// "Config/Sessions" directory of the original application.
type Store struct {
	mu   sync.Mutex
	root string
}

// NewStore opens (and creates) a session directory.
func NewStore(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

// Root returns the directory holding the sessions.
func (s *Store) Root() string { return s.root }

var invalidName = strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")

// CleanName makes a string safe for use as a session or folder name.
func CleanName(n string) string {
	n = strings.TrimSpace(invalidName.Replace(n))
	n = strings.Trim(n, ".")
	if n == "" {
		n = "Session"
	}
	return n
}

// cleanPath validates a slash-separated session path.
func cleanPath(p string) (string, error) {
	p = strings.Trim(filepath.ToSlash(p), "/")
	if p == "" {
		return "", nil
	}
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid path %q", p)
		}
		parts[i] = CleanName(part)
	}
	return strings.Join(parts, "/"), nil
}

func (s *Store) fsPath(p string) string { return filepath.Join(s.root, filepath.FromSlash(p)) }

// Tree returns the full session tree.
func (s *Store) Tree() (*Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root := &Node{Name: "Sessions", IsFolder: true}
	err := s.walk(root, "")
	return root, err
}

func (s *Store) walk(n *Node, rel string) error {
	entries, err := os.ReadDir(s.fsPath(rel))
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			c := &Node{Name: name, Path: JoinPath(rel, name), IsFolder: true}
			if err := s.walk(c, c.Path); err != nil {
				return err
			}
			n.Children = append(n.Children, c)
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ext) || strings.EqualFold(name, folderDataFile) {
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if rel == "" && strings.EqualFold(base, defaultFile) {
			continue // template for new sessions, not shown in the tree
		}
		p := JoinPath(rel, base)
		c := &Node{Name: base, Path: p}
		if f, err := s.readFile(p); err == nil {
			ss := FromFile(p, f)
			c.Protocol, c.Hostname, c.Username, c.Port = ss.Protocol, ss.Hostname, ss.Username, ss.Port
			c.Description = strings.Join(ss.Description, "\n")
			c.DeviceType, c.Environment, c.Jump, c.Proxy = ss.DeviceType, ss.Environment, ss.JumpSession, ss.ProxyType != ""
			c.MAC = ss.MAC
			if ss.Protocol == ProtoConsole && ss.ConsoleServer != "" {
				c.Console = ss.ConsoleServer + " port " + strconv.Itoa(ss.ConsolePort)
			}
			if ss.Protocol == ProtoSerial {
				c.Hostname = ss.ComPort
			}
			if h, ok := HopOf(ss); ok {
				c.HopTarget = h.Target
			}
			if ss.Protocol == ProtoSSH2 {
				c.OpenAs = ss.OpenAs
			}
		}
		n.Children = append(n.Children, c)
	}
	sort.SliceStable(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		if a.IsFolder != b.IsFolder {
			return a.IsFolder
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return nil
}

func (s *Store) readFile(p string) (*sessionfile.File, error) {
	data, err := os.ReadFile(s.fsPath(p) + ext)
	if err != nil {
		return nil, err
	}
	return sessionfile.Parse(data)
}

// template returns the Default session file if present, otherwise an empty file.
func (s *Store) template() *sessionfile.File {
	if f, err := s.readFile(defaultFile); err == nil {
		return f.Clone()
	}
	return sessionfile.NewFile()
}

// Get loads a session.
func (s *Store) Get(p string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := cleanPath(p)
	if err != nil {
		return Session{}, err
	}
	f, err := s.readFile(p)
	if err != nil {
		return Session{}, err
	}
	return FromFile(p, f), nil
}

// NewSession returns defaults merged from the Default template.
func (s *Store) NewSession() Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.template()
	if len(t.Entries) == 0 {
		return Defaults()
	}
	ss := FromFile("", t)
	ss.Hostname, ss.Username, ss.Description = "", "", []string{}
	return ss
}

// Exists reports whether a session exists.
func (s *Store) Exists(p string) bool {
	_, err := os.Stat(s.fsPath(p) + ext)
	return err == nil
}

// Save writes a session. If oldPath is set and differs from the new path the
// session is moved/renamed. Unknown keys of an existing file are preserved.
func (s *Store) Save(ss Session, oldPath string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss.Name = CleanName(ss.Name)
	folder, err := cleanPath(ss.Folder)
	if err != nil {
		return ss, err
	}
	ss.Folder = folder
	ss.Path = JoinPath(folder, ss.Name)
	if folder == "" && strings.EqualFold(ss.Name, defaultFile) {
		return ss, errors.New(`"Default" is reserved for the default session template`)
	}
	var f *sessionfile.File
	if oldPath != "" {
		if f, err = s.readFile(oldPath); err != nil {
			return ss, err
		}
		if oldPath != ss.Path {
			if _, err := os.Stat(s.fsPath(ss.Path) + ext); err == nil {
				return ss, fmt.Errorf("a session named %q already exists", ss.Path)
			}
		}
	} else {
		if _, err := os.Stat(s.fsPath(ss.Path) + ext); err == nil {
			return ss, fmt.Errorf("a session named %q already exists", ss.Path)
		}
		f = s.template()
	}
	ss.Apply(f)
	if err := s.writeFile(ss.Path, f); err != nil {
		return ss, err
	}
	if oldPath != "" && oldPath != ss.Path {
		if err := os.Remove(s.fsPath(oldPath) + ext); err != nil {
			return ss, err
		}
		s.updateFolderData(parentOf(oldPath), "Session List", baseOf(oldPath), false)
	}
	return ss, nil
}

func (s *Store) writeFile(p string, f *sessionfile.File) error {
	full := s.fsPath(p) + ext
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, f.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, full); err != nil {
		return err
	}
	s.updateFolderData(parentOf(p), "Session List", baseOf(p), true)
	return nil
}

// WriteRaw stores an already-parsed file at path (used by import).
func (s *Store) WriteRaw(p string, f *sessionfile.File) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := cleanPath(p)
	if err != nil {
		return err
	}
	return s.writeFile(p, f)
}

// ReadRaw returns the parsed file for a session path.
func (s *Store) ReadRaw(p string) (*sessionfile.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readFile(p)
}

// Duplicate copies a session to a new unique name in the same folder.
func (s *Store) Duplicate(p string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readFile(p)
	if err != nil {
		return "", err
	}
	np := s.uniquePath(p)
	return np, s.writeFile(np, f)
}

// uniquePath returns p or "p (2)", "p (3)", ... that does not exist yet.
func (s *Store) uniquePath(p string) string {
	if _, err := os.Stat(s.fsPath(p) + ext); err != nil {
		return p
	}
	for i := 2; ; i++ {
		c := fmt.Sprintf("%s (%d)", p, i)
		if _, err := os.Stat(s.fsPath(c) + ext); err != nil {
			return c
		}
	}
}

// Delete removes a session.
func (s *Store) Delete(p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := cleanPath(p)
	if err != nil || p == "" {
		return errors.New("invalid session path")
	}
	if err := os.Remove(s.fsPath(p) + ext); err != nil {
		return err
	}
	s.updateFolderData(parentOf(p), "Session List", baseOf(p), false)
	return nil
}

// CreateFolder creates a folder (and parents).
func (s *Store) CreateFolder(p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := cleanPath(p)
	if err != nil || p == "" {
		return errors.New("invalid folder name")
	}
	if err := os.MkdirAll(s.fsPath(p), 0o700); err != nil {
		return err
	}
	s.updateFolderData(parentOf(p), "Folder List", baseOf(p), true)
	return nil
}

// RenameFolder renames or moves a folder.
func (s *Store) RenameFolder(oldPath, newPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err1 := cleanPath(oldPath)
	n, err2 := cleanPath(newPath)
	if err1 != nil || err2 != nil || o == "" || n == "" {
		return errors.New("invalid folder path")
	}
	if strings.HasPrefix(n+"/", o+"/") {
		return errors.New("cannot move a folder into itself")
	}
	if _, err := os.Stat(s.fsPath(n)); err == nil {
		return fmt.Errorf("folder %q already exists", n)
	}
	if err := os.MkdirAll(filepath.Dir(s.fsPath(n)), 0o700); err != nil {
		return err
	}
	if err := os.Rename(s.fsPath(o), s.fsPath(n)); err != nil {
		return err
	}
	s.updateFolderData(parentOf(o), "Folder List", baseOf(o), false)
	s.updateFolderData(parentOf(n), "Folder List", baseOf(n), true)
	return nil
}

// DeleteFolder removes a folder and everything inside it.
func (s *Store) DeleteFolder(p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := cleanPath(p)
	if err != nil || p == "" {
		return errors.New("refusing to delete the session root")
	}
	if err := os.RemoveAll(s.fsPath(p)); err != nil {
		return err
	}
	s.updateFolderData(parentOf(p), "Folder List", baseOf(p), false)
	return nil
}

// updateFolderData keeps the per-folder index file in sync so the original
// application lists items created by Termwright.
func (s *Store) updateFolderData(folder, key, item string, add bool) {
	fp := filepath.Join(s.fsPath(folder), folderDataFile)
	var f *sessionfile.File
	if data, err := os.ReadFile(fp); err == nil {
		if f, err = sessionfile.Parse(data); err != nil {
			return
		}
	} else if add {
		f = sessionfile.NewFile()
		f.SetList("Folder List", []string{})
		f.SetList("Session List", []string{})
	} else {
		return
	}
	list := f.List(key)
	idx := -1
	for i, v := range list {
		if strings.EqualFold(v, item) {
			idx = i
		}
	}
	switch {
	case add && idx < 0:
		list = append(list, item)
	case !add && idx >= 0:
		list = append(list[:idx], list[idx+1:]...)
	default:
		return
	}
	if list == nil {
		list = []string{}
	}
	f.SetList(key, list)
	_ = os.WriteFile(fp, f.Bytes(), 0o600)
}

// AllSessions returns every session path below the given folders/sessions
// (all sessions when paths is empty).
func (s *Store) AllSessions(paths []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(paths) == 0 {
		paths = []string{""}
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		p, err := cleanPath(p)
		if err != nil {
			return nil, err
		}
		if p != "" {
			if _, err := os.Stat(s.fsPath(p) + ext); err == nil {
				if !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
				continue
			}
		}
		base := s.fsPath(p)
		err = filepath.WalkDir(base, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.EqualFold(filepath.Ext(fp), ext) || strings.EqualFold(d.Name(), folderDataFile) {
				return nil
			}
			rel, _ := filepath.Rel(s.root, fp)
			rel = filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))
			if strings.EqualFold(rel, defaultFile) || seen[rel] {
				return nil
			}
			seen[rel] = true
			out = append(out, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func parentOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

func baseOf(p string) string { return p[strings.LastIndex(p, "/")+1:] }
