// Package skills keeps AI skills: procedures the AI assistant follows when a request matches, as
// SKILL.md files (the open Agent Skills format: YAML front matter with name and description, then
// Markdown), one folder per skill, so skills written for other AI tools work here and the other way.
package skills

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// FileName is the file of a skill in its folder.
const FileName = "SKILL.md"

// MaxSize of a SKILL.md.
const MaxSize = 256 << 10

// Skill is one skill.
type Skill struct {
	// Name: the folder name and the name in the front matter (lower case, digits and dashes).
	Name string `json:"name"`
	// Description: what it does and when to use it (all the AI sees until it loads the skill).
	Description string `json:"description"`
	// DeviceTypes it is for (empty: any).
	DeviceTypes []string `json:"deviceTypes,omitempty"`
	// Text: the whole SKILL.md (only when asked for one skill).
	Text string `json:"text,omitempty"`
	// Files: the other files in its folder (scripts, templates).
	Files []string `json:"files,omitempty"`
	// Path of its SKILL.md.
	Path string `json:"path"`
	// Modified: when the file was last changed.
	Modified time.Time `json:"modified"`
}

// Store keeps skills in Dir/<name>/SKILL.md.
type Store struct{ Dir string }

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidName says whether a name can be a skill's name.
func ValidName(n string) bool { return nameRe.MatchString(n) }

// Slug makes a skill name from any text ("Upgrade EXOS firmware" → upgrade-exos-firmware).
func Slug(s string) string {
	s = strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 64 {
		s = strings.TrimRight(s[:64], "-")
	}
	return s
}

// Parse reads the front matter of a SKILL.md: name, description and device-types (a list or comma
// separated). The body is the rest.
func Parse(text string) (Skill, string, error) {
	text = strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\ufeff")
	if !strings.HasPrefix(text, "---\n") {
		return Skill{}, "", errors.New("a SKILL.md starts with front matter: ---, name: …, description: …, ---")
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return Skill{}, "", errors.New("the front matter has no closing ---")
	}
	fm, body := text[4:4+end], strings.TrimLeft(text[4+end+4:], "\n")
	var sk Skill
	var key string
	vals := map[string]string{}
	for _, l := range strings.Split(fm, "\n") {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && key != "" { // continued value or list item
			v := strings.TrimSpace(l)
			if strings.HasPrefix(v, "- ") {
				v = strings.TrimSpace(v[2:])
				if vals[key] != "" {
					v = ", " + v
				}
				vals[key] += v
			} else {
				vals[key] = strings.TrimSpace(vals[key] + " " + v)
			}
			continue
		}
		i := strings.Index(l, ":")
		if i < 0 {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(l[:i]))
		vals[key] = strings.TrimSpace(l[i+1:])
		switch vals[key] { // a YAML block scalar: the value is on the next lines
		case ">", "|", ">-", "|-", ">+", "|+":
			vals[key] = ""
		}
	}
	unq := func(s string) string {
		s = strings.TrimSpace(s)
		if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
		return s
	}
	sk.Name, sk.Description = unq(vals["name"]), unq(vals["description"])
	for _, k := range []string{"device-types", "device_types", "devicetypes"} {
		if v := strings.Trim(strings.TrimSpace(vals[k]), "[]"); v != "" {
			for _, t := range strings.Split(v, ",") {
				if t = unq(t); t != "" {
					sk.DeviceTypes = append(sk.DeviceTypes, t)
				}
			}
		}
	}
	if sk.Name == "" {
		return sk, body, errors.New("the front matter needs a name")
	}
	if !ValidName(sk.Name) {
		return sk, body, fmt.Errorf("the name %q may have only lower-case letters, digits and dashes (at most 64), e.g. %s", sk.Name, Slug(sk.Name))
	}
	if sk.Description == "" {
		return sk, body, errors.New("the front matter needs a description: what the skill does and when to use it")
	}
	return sk, body, nil
}

// List the skills by name (a folder whose SKILL.md cannot be read is left out).
func (s Store) List() []Skill {
	ents, _ := os.ReadDir(s.Dir)
	var out []Skill
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if sk, err := s.Get(e.Name()); err == nil {
			sk.Text = ""
			out = append(out, sk)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get a skill with its text.
func (s Store) Get(name string) (Skill, error) {
	if !ValidName(name) {
		return Skill{}, fmt.Errorf("no skill %q", name)
	}
	p := filepath.Join(s.Dir, name, FileName)
	b, err := os.ReadFile(p)
	if err != nil {
		return Skill{}, fmt.Errorf("no skill %q", name)
	}
	sk, _, err := Parse(string(b))
	if err != nil {
		return Skill{}, fmt.Errorf("skill %s: %w", name, err)
	}
	sk.Name, sk.Text, sk.Path = name, string(b), p
	if st, err := os.Stat(p); err == nil {
		sk.Modified = st.ModTime()
	}
	ents, _ := os.ReadDir(filepath.Join(s.Dir, name))
	for _, e := range ents {
		if e.Name() != FileName && !strings.HasPrefix(e.Name(), ".") {
			sk.Files = append(sk.Files, e.Name())
		}
	}
	return sk, nil
}

// Save writes a SKILL.md (its front matter names it); old is the skill's name before (a rename).
func (s Store) Save(old, text string) (Skill, error) {
	if len(text) > MaxSize {
		return Skill{}, fmt.Errorf("a skill is at most %d KB", MaxSize>>10)
	}
	sk, _, err := Parse(text)
	if err != nil {
		return Skill{}, err
	}
	dir := filepath.Join(s.Dir, sk.Name)
	if old != "" && old != sk.Name && ValidName(old) {
		if _, err := os.Stat(dir); err == nil {
			return Skill{}, fmt.Errorf("a skill named %s already exists", sk.Name)
		}
		if err := os.Rename(filepath.Join(s.Dir, old), dir); err != nil && !os.IsNotExist(err) {
			return Skill{}, err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Skill{}, err
	}
	p := filepath.Join(dir, FileName)
	if err := os.WriteFile(p+".tmp", []byte(text), 0o600); err != nil {
		return Skill{}, err
	}
	if err := os.Rename(p+".tmp", p); err != nil {
		return Skill{}, err
	}
	return s.Get(sk.Name)
}

// Remove moves a skill's folder to Dir/.removed (it can be put back from there).
func (s Store) Remove(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("no skill %q", name)
	}
	to := filepath.Join(s.Dir, ".removed")
	if err := os.MkdirAll(to, 0o700); err != nil {
		return err
	}
	return os.Rename(filepath.Join(s.Dir, name), filepath.Join(to, name+"-"+time.Now().Format("20060102-150405")))
}

// Import adds a skill from a SKILL.md (or any .md with its front matter), a folder holding one, or a
// .zip of such a folder; the other files of the folder come too. An existing skill of that name is
// replaced only when replace is set.
func (s Store) Import(path string, replace bool) (Skill, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Skill{}, err
	}
	files := map[string][]byte{} // relative path → content
	switch {
	case st.IsDir():
		err = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(path, p)
			if len(files) > 200 {
				return errors.New("too many files for a skill")
			}
			b, err := os.ReadFile(p)
			files[filepath.ToSlash(rel)] = b
			return err
		})
	case strings.EqualFold(filepath.Ext(path), ".zip"):
		files, err = unzip(path)
	default:
		var b []byte
		b, err = os.ReadFile(path)
		files[FileName] = b
	}
	if err != nil {
		return Skill{}, err
	}
	// the SKILL.md may be in a folder of the zip: that folder is the skill
	root := ""
	for k := range files {
		if strings.EqualFold(filepath.Base(k), FileName) && (root == "" || len(k) < len(root)+len(FileName)) {
			root = strings.TrimSuffix(k, filepath.Base(k))
		}
	}
	md, ok := files[root+FileName]
	if !ok {
		for k, v := range files {
			if strings.EqualFold(k, root+FileName) {
				md, ok = v, true
			}
		}
	}
	if !ok {
		return Skill{}, errors.New("no SKILL.md in it")
	}
	sk, _, err := Parse(string(md))
	if err != nil {
		return Skill{}, err
	}
	dir := filepath.Join(s.Dir, sk.Name)
	if _, err := os.Stat(dir); err == nil {
		if !replace {
			return Skill{}, fmt.Errorf("a skill named %s already exists", sk.Name)
		}
		if err := s.Remove(sk.Name); err != nil {
			return Skill{}, err
		}
	}
	for k, v := range files {
		if !strings.HasPrefix(k, root) {
			continue
		}
		rel := filepath.FromSlash(strings.TrimPrefix(k, root))
		if strings.EqualFold(rel, FileName) {
			rel = FileName
		}
		dst := filepath.Join(dir, rel)
		if !strings.HasPrefix(dst, dir+string(os.PathSeparator)) { // no ../ out of the folder
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return Skill{}, err
		}
		if err := os.WriteFile(dst, v, 0o600); err != nil {
			return Skill{}, err
		}
	}
	return s.Get(sk.Name)
}

func unzip(path string) (map[string][]byte, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := map[string][]byte{}
	var total int64
	for _, f := range r.File {
		if f.FileInfo().IsDir() || strings.Contains(f.Name, "__MACOSX") {
			continue
		}
		if total += int64(f.UncompressedSize64); total > 20<<20 || len(out) > 200 {
			return nil, errors.New("the zip is too large for a skill")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, 20<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		out[f.Name] = b
	}
	return out, nil
}

// Zip is a skill's folder as a zip (the folder inside it, as other tools share skills).
func (s Store) Zip(name string) ([]byte, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("no skill %q", name)
	}
	dir := filepath.Join(s.Dir, name)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		f, err := w.Create(name + "/" + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// For says whether a skill fits a device type (a skill for no type fits all).
func (sk Skill) For(deviceType string) bool {
	if len(sk.DeviceTypes) == 0 || deviceType == "" {
		return true
	}
	for _, t := range sk.DeviceTypes {
		if strings.EqualFold(t, deviceType) || strings.EqualFold(t, "any") {
			return true
		}
	}
	return false
}
