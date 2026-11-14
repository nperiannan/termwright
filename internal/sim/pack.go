package sim

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

// A device pack (.vm file) describes the simulated devices: their models,
// port types, and for each operating system its prompts, commands (what they
// change in the simulated switch) and outputs (Go text/template). Termwright
// itself carries no vendor's commands or outputs - a pack is loaded only with
// the user's consent. A .vm file is a zip of this layout:
//
//	pack.json            id, name, version, vendor, description, license, os: [ids]
//	models.json          models: SKU, family, OS, system type, versions, serial / MAC formats, fields
//	ports.json           port types by the letters in a model name (T copper, X 10G fiber…)
//	os/<id>/os.json      prompts, modes, errors, keep rules, commands
//	os/<id>/*.tmpl       outputs
//	tests/*.txt          transcripts ("> command" then the expected output)
//	emulator.json        optional: image profiles and personas for the emulated lab

// Pack is a loaded device pack.
type Pack struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Vendor      string   `json:"vendor"`
	Description string   `json:"description"`
	License     string   `json:"license"`
	OSIDs       []string `json:"os"`

	OS        map[string]*OSDef   `json:"-"`
	Models    []Model             `json:"-"`
	PortTypes map[string]PortType `json:"-"`
	Hash      string              `json:"-"` // sha256 of the .vm file
	Tests     map[string]string   `json:"-"` // tests/*.txt
	Emulator  []byte              `json:"-"` // emulator.json: image profiles for the emulated lab (the app reads it)
}

// PortType is what the letters of a model name stand for ("MW" multirate…).
type PortType struct {
	Name   string `json:"name"`   // shown: copper, multirate, fiber…
	Speed  string `json:"speed"`  // 1G, 2.5G, 10G, 25G, 100G…
	Uplink bool   `json:"uplink"` // used for uplinks by the cabling presets
}

// Model is a switch model of a pack.
type Model struct {
	SKU     string            `json:"sku"`     // D100-24T-4X
	Family  string            `json:"family"`  // a group for the wizard: for example "Universal"
	OS      []string          `json:"os"`      // the operating systems it runs (pack ids)
	SysType map[string]string `json:"sysType"` // OS → system type, "{sku}" is the SKU (default "{sku}")
	Version map[string]string `json:"version"` // OS → software version
	Serial  string            `json:"serial"`  // serial number format (see Generate)
	MAC     string            `json:"mac"`     // base MAC format
	Ports   []PortGroup       `json:"ports,omitempty"`
	Fields  map[string]string `json:"fields"` // anything the outputs use (part, rev, bootrom…); values may be formats
}

// PortGroup is a run of ports of one type.
type PortGroup struct {
	Count int    `json:"count"`
	Type  string `json:"type"` // a key of ports.json
}

// OSDef is one operating system of a pack.
type OSDef struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Short      string            `json:"short"`         // a short label (for example "DOS")
	ConfigCmd  string            `json:"configCommand"` // the command that prints the configuration
	DeviceType string            `json:"deviceType"`    // the Termwright device type for its sessions
	PortStyle  string            `json:"portStyle"`     // "n" (1, 2…) or "slot" (1/1, 1/2…)
	StartMode  string            `json:"startMode"`     // mode after login (exec over SSH starts in ExecMode)
	ExecMode   string            `json:"execMode"`
	Prompts    map[string]string `json:"prompts"` // mode → prompt (template)
	ExitTo     map[string]string `json:"exitTo"`  // mode → mode after "exit" ("quit" = log out)
	CtrlCTo    map[string]string `json:"ctrlCTo"` // mode → mode after Ctrl+C
	Banner     string            `json:"banner"`  // template
	More       string            `json:"more"`    // the paging prompt
	Count      bool              `json:"count"`   // count commands (.CmdNo in the prompt)
	Errors     map[string]string `json:"errors"`  // templates: invalid, incomplete, ambiguous, unrecognized, cancelled, and action errors
	MgmtVRs    []string          `json:"mgmtVRs"` // VR / VRF names that mean the management network
	VRs        map[string]string `json:"vrs"`     // known VR / VRF names, lower case → shown
	Keep       *KeepRules        `json:"keep"`
	Commands   []CommandDef      `json:"commands"`

	tmpl *template.Template
}

// KeepRules: configuration commands the pack does not model are kept.
type KeepRules struct {
	Verbs     []string          `json:"verbs"`     // first words taken (abbreviated from 3 letters); empty = any line
	Modes     string            `json:"modes"`     // the modes they are taken in ("" = every)
	No        string            `json:"no"`        // the word that takes a kept line back ("no")
	Opposites map[string]string `json:"opposites"` // verb → the verb whose line it takes back
	CtxPorts  string            `json:"ctxPorts"`  // context name for a port interface ("interface gigabitEthernet {ports}")
	CtxVLAN   string            `json:"ctxVlan"`   // context name for a VLAN interface ("interface vlan {id}")
}

// CommandDef is one command: its words, what it does and what it prints.
type CommandDef struct {
	Cmd   string   `json:"cmd"`   // words; <x> = a value, <x...> = the rest of the line
	Modes string   `json:"modes"` // modes it is in ("" = every)
	Help  string   `json:"help"`
	Do    []string `json:"do"`  // actions (see actions.go)
	Out   string   `json:"out"` // output: a template file name (x.tmpl) or the template itself
}

var packID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// LoadPackFile reads a .vm file (or, for pack makers, a folder).
func LoadPackFile(p string) (*Pack, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return LoadPack(os.DirFS(p), "")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	if len(b) > 32<<20 {
		return nil, errors.New("a device pack is at most 32 MB")
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("not a device pack (.vm): %w", err)
	}
	sum := sha256.Sum256(b)
	return LoadPack(zr, hex.EncodeToString(sum[:]))
}

// LoadPack reads a pack from a file system (the zip or a folder).
func LoadPack(fsys fs.FS, hash string) (*Pack, error) {
	// a zip made of a folder may have it as its only top folder
	if _, err := fs.Stat(fsys, "pack.json"); err != nil {
		if ents, _ := fs.ReadDir(fsys, "."); len(ents) == 1 && ents[0].IsDir() {
			if sub, err := fs.Sub(fsys, ents[0].Name()); err == nil {
				fsys = sub
			}
		}
	}
	p := &Pack{Hash: hash, OS: map[string]*OSDef{}, Tests: map[string]string{}}
	if err := readJSON(fsys, "pack.json", p); err != nil {
		return nil, err
	}
	if !packID.MatchString(p.ID) || p.Name == "" {
		return nil, errors.New("pack.json: id (lower case, digits, . _ -) and name are needed")
	}
	var models struct {
		Models []Model `json:"models"`
	}
	if err := readJSON(fsys, "models.json", &models); err != nil {
		return nil, err
	}
	p.Models = models.Models
	if f, err := fsys.Open("emulator.json"); err == nil {
		p.Emulator, _ = io.ReadAll(io.LimitReader(f, 8<<20))
		f.Close()
	}
	if err := readJSON(fsys, "ports.json", &p.PortTypes); err != nil {
		return nil, err
	}
	norm := map[string]PortType{}
	for k, v := range p.PortTypes {
		norm[strings.ToUpper(k)] = v
	}
	p.PortTypes = norm
	for _, id := range p.OSIDs {
		o := &OSDef{}
		if err := readJSON(fsys, path.Join("os", id, "os.json"), o); err != nil {
			return nil, err
		}
		o.ID = id
		if o.PortStyle == "" {
			o.PortStyle = "n"
		}
		t := template.New(id).Funcs(tmplFuncs).Option("missingkey=zero")
		ents, _ := fs.ReadDir(fsys, path.Join("os", id))
		for _, e := range ents {
			if !strings.HasSuffix(e.Name(), ".tmpl") {
				continue
			}
			b, err := fs.ReadFile(fsys, path.Join("os", id, e.Name()))
			if err != nil {
				return nil, err
			}
			if _, err := t.New(e.Name()).Parse(string(b)); err != nil {
				return nil, fmt.Errorf("os/%s/%s: %w", id, e.Name(), err)
			}
		}
		// inline templates: prompts, banner, errors, outputs
		inline := map[string]string{"banner": o.Banner, "more": o.More}
		for k, v := range o.Prompts {
			inline["prompt:"+k] = v
		}
		for k, v := range o.Errors {
			inline["error:"+k] = v
		}
		for i, c := range o.Commands {
			if c.Out != "" && !strings.HasSuffix(c.Out, ".tmpl") {
				inline["out:"+strconv.Itoa(i)] = c.Out
			} else if c.Out != "" && t.Lookup(c.Out) == nil {
				return nil, fmt.Errorf("os/%s: %q: no template %s", id, c.Cmd, c.Out)
			}
		}
		for k, v := range inline {
			if _, err := t.New(k).Parse(v); err != nil {
				return nil, fmt.Errorf("os/%s %s: %w", id, k, err)
			}
		}
		o.tmpl = t
		p.OS[id] = o
	}
	if len(p.OS) == 0 {
		return nil, errors.New("pack.json: no operating systems (os)")
	}
	for _, m := range p.Models {
		for _, o := range m.OS {
			if p.OS[o] == nil {
				return nil, fmt.Errorf("models.json: %s runs %q, which the pack does not have", m.SKU, o)
			}
		}
	}
	if ents, err := fs.ReadDir(fsys, "tests"); err == nil {
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".txt") {
				b, _ := fs.ReadFile(fsys, path.Join("tests", e.Name()))
				p.Tests[e.Name()] = string(b)
			}
		}
	}
	return p, nil
}

func readJSON(fsys fs.FS, name string, v any) error {
	f, err := fsys.Open(name)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 8<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// ModelOf finds a model by SKU or system type.
func (p *Pack) ModelOf(name string) (Model, bool) {
	if p == nil {
		return Model{}, false
	}
	for _, m := range p.Models {
		if strings.EqualFold(m.SKU, name) {
			return m, true
		}
		for os := range m.SysType {
			if strings.EqualFold(m.SysTypeFor(os), name) {
				return m, true
			}
		}
	}
	return Model{}, false
}

// SysTypeFor is the system type the model shows when it runs os.
func (m Model) SysTypeFor(os string) string {
	t := m.SysType[os]
	if t == "" {
		t = "{sku}"
	}
	return strings.ReplaceAll(t, "{sku}", m.SKU)
}

// Layout: the model's ports, from its port list or from the letters of its
// name ("D300-16MW-8T": 16 of type MW, 8 of type T; "D400-48p-10G4": 48 of type p, 4 of type 10G).
func (p *Pack) Layout(m Model) []PortGroup {
	if len(m.Ports) > 0 {
		return m.Ports
	}
	var out []PortGroup
	parts := strings.Split(m.SKU, "-")
	re := regexp.MustCompile(`^(\d+)([A-Za-z]+)$`)
	reG := regexp.MustCompile(`^(\d+G[A-Za-z]*?)(\d+)$`) // 10G4, 10GE4
	for _, s := range parts[1:] {
		if g := reG.FindStringSubmatch(s); g != nil {
			if _, ok := p.PortTypes[strings.ToUpper(g[1])]; ok {
				n, _ := strconv.Atoi(g[2])
				out = append(out, PortGroup{Count: n, Type: strings.ToUpper(g[1])})
			}
			continue
		}
		if g := re.FindStringSubmatch(s); g != nil {
			n, _ := strconv.Atoi(g[1])
			if _, ok := p.PortTypes[strings.ToUpper(g[2])]; ok && n > 0 && n <= 512 {
				out = append(out, PortGroup{Count: n, Type: strings.ToUpper(g[2])})
			}
		}
	}
	return out
}

// PortCount: how many ports a model has.
func (p *Pack) PortCount(m Model) int {
	n := 0
	for _, g := range p.Layout(m) {
		n += g.Count
	}
	return n
}

// Uplinks: the port numbers of the model's uplink types (else its last four).
func (p *Pack) Uplinks(m Model) []int {
	var up []int
	i := 0
	for _, g := range p.Layout(m) {
		for k := 0; k < g.Count; k++ {
			i++
			if p.PortTypes[g.Type].Uplink {
				up = append(up, i)
			}
		}
	}
	if len(up) == 0 {
		for k := max(1, i-3); k <= i; k++ {
			up = append(up, k)
		}
	}
	return up
}

// Describe: "16 multirate, 8 copper".
func (p *Pack) Describe(m Model) string {
	var parts []string
	for _, g := range p.Layout(m) {
		pt := p.PortTypes[g.Type]
		d := pt.Name
		if pt.Speed != "" {
			d = pt.Speed + " " + d
		}
		parts = append(parts, fmt.Sprintf("%d %s", g.Count, strings.TrimSpace(d)))
	}
	return strings.Join(parts, ", ")
}

// SortedModels: the models by family, then SKU.
func (p *Pack) SortedModels() []Model {
	out := append([]Model(nil), p.Models...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Family != out[j].Family {
			return out[i].Family < out[j].Family
		}
		return out[i].SKU < out[j].SKU
	})
	return out
}
