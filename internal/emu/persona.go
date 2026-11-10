package emu

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strconv"
	"strings"
	"text/template"
)

// A persona makes a plain VM image act as a given device: a file the image
// reads at boot (for example the model, serial numbers, modules and stack
// members it should be), written into the VM's disk while it is off, then
// booted. The image profile defines the file (its path in the VM and a Go
// text/template), the models to choose from and extra values; each VM keeps
// its choice. Termwright knows no persona of its own - image profiles bring them.

// PersonaDef is an image's persona file.
type PersonaDef struct {
	Name     string         `json:"name"`     // what it is called ("Lab identity")
	File     string         `json:"file"`     // its path in the VM (/scratch/config.yaml)
	Template string         `json:"template"` // Go text/template over PersonaData
	Models   []PersonaModel `json:"models"`
	Values   []PersonaValue `json:"values"`  // more fields, the same for all slots
	Stack    bool           `json:"stack"`   // the persona can be a stack of several slots
	Modules  string         `json:"modules"` // what a module is called ("VIM")
}

// PersonaModel is a model a VM can be, with the modules it can take.
type PersonaModel struct {
	Name    string   `json:"name"`
	Modules []string `json:"modules"`
}

// PersonaValue is a field of the persona (a licence…).
type PersonaValue struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Default string `json:"default"`
}

// Persona is a VM's choice.
type Persona struct {
	Slots   []Slot            `json:"slots"`
	Values  map[string]string `json:"values"`
	Applied string            `json:"applied,omitempty"` // hash of the file last written into its disk
}

// Slot is one device of the persona (one stack member).
type Slot struct {
	Num    int       `json:"num"`
	Model  string    `json:"model"`
	Serial string    `json:"serial"`
	Module string    `json:"module"`
	Master bool      `json:"master"`
	Ports  []PortMap `json:"ports"`
}

// PortMap maps a front-panel port of a slot to one of the VM's network ports.
type PortMap struct {
	Port int `json:"port"`
	VIF  int `json:"vif"`
}

// PersonaData is what a persona template sees.
type PersonaData struct {
	Name   string // the VM's name (hostname)
	Stack  bool   // more than one slot
	Slots  []Slot
	Values map[string]string
}

// Render makes the persona file for a VM ("" when it has none).
func (d *PersonaDef) Render(vm VM, p *Persona) (string, error) {
	if d == nil || p == nil || len(p.Slots) == 0 {
		return "", nil
	}
	t, err := template.New("persona").Option("missingkey=zero").Parse(d.Template)
	if err != nil {
		return "", fmt.Errorf("persona template: %w", err)
	}
	vals := map[string]string{}
	for _, v := range d.Values {
		vals[v.Key] = v.Default
	}
	for k, v := range p.Values {
		if v != "" {
			vals[k] = v
		}
	}
	slots := append([]Slot(nil), p.Slots...)
	master := false
	for i := range slots {
		if slots[i].Num == 0 {
			slots[i].Num = i + 1
		}
		master = master || slots[i].Master
	}
	if !master {
		slots[0].Master = true
	}
	var b bytes.Buffer
	if err := t.Execute(&b, PersonaData{Name: vm.Name, Stack: len(slots) > 1, Slots: slots, Values: vals}); err != nil {
		return "", fmt.Errorf("persona template: %w", err)
	}
	return b.String(), nil
}

// Hash says whether a persona file changed since it was written.
func Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// ParsePorts reads "10:1, 3:3" (front-panel port : VM port).
func ParsePorts(s string) ([]PortMap, error) {
	var out []PortMap
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		a, b, ok := strings.Cut(f, ":")
		p, err1 := strconv.Atoi(a)
		v, err2 := strconv.Atoi(b)
		if !ok || err1 != nil || err2 != nil || p < 1 || v < 1 {
			return nil, fmt.Errorf("%q is not port:vm-port", f)
		}
		out = append(out, PortMap{Port: p, VIF: v})
	}
	return out, nil
}

// WriteFileScript writes content into the file at file (a path in the VM)
// in a VM's disk with libguestfs, on the host, while the VM is off. It finds
// the filesystem that has the file's folder, or one labelled like it (a
// /scratch partition). It prints "ok <filesystem>" or "fail <why>".
func (l *Lab) WriteFileScript(v VM, file, content string) string {
	disk := l.VMDir() + "/" + v.ID + ".qcow2"
	dir, base := path.Dir(file), path.Base(file)
	label := strings.TrimPrefix(path.Base(dir), "/")
	tmp := l.VMDir() + "/" + v.ID + ".persona"
	return fmt.Sprintf(`command -v guestfish >/dev/null 2>&1 || { echo "fail libguestfs-tools (guestfish) is not installed on the host: use Set Up QEMU/KVM"; exit 0; }
export LIBGUESTFS_BACKEND=direct
d=%s
cat > %s <<'TW_PERSONA_END'
%sTW_PERSONA_END
target=""
for f in $(guestfish --ro -a "$d" run : list-filesystems 2>/dev/null | cut -d: -f1); do
 if [ "$(guestfish --ro -a "$d" -m "$f" is-dir %s 2>/dev/null)" = true ]; then target="$f %s"; break; fi
 if [ "$(guestfish --ro -a "$d" run : vfs-label "$f" 2>/dev/null)" = %s ]; then target="$f /"; break; fi
done
if [ -z "$target" ]; then echo "fail no filesystem with %s in the disk (has the switch booted once?)"; rm -f %s; exit 0; fi
set -- $target
p="$2/%s"; [ "$2" = / ] && p="/%s"
if out=$(guestfish -a "$d" -m "$1" upload %s "$p" 2>&1); then echo "ok $1"; else echo "fail $(echo $out | tr '\n' ' ')"; fi
rm -f %s
`, Q(disk), Q(tmp), ensureNL(content), Q(dir), Q(dir), Q(label), Q(dir), Q(tmp), base, base, Q(tmp), Q(tmp))
}

// ShutdownScript asks VMs to power off (ACPI) and waits up to secs seconds,
// then stops the ones still running.
func (l *Lab) ShutdownScript(ids []string, secs int) string {
	var b strings.Builder
	dir := l.VMDir()
	for _, id := range ids {
		v := l.VMByID(id)
		if v == nil {
			continue
		}
		fmt.Fprintf(&b, "(printf 'system_powerdown\\r\\n'; sleep 1) | timeout 5 sh -c 'exec 3<>/dev/tcp/127.0.0.1/%d 2>/dev/null && cat >&3' 2>/dev/null || true\n", l.MonitorPort(*v))
		_ = dir
	}
	fmt.Fprintf(&b, "for i in $(seq %d); do alive=0; for id in %s; do [ -f %s/$id.pid ] && kill -0 \"$(cat %s/$id.pid)\" 2>/dev/null && alive=1; done; [ $alive = 0 ] && break; sleep 1; done\n",
		secs, strings.Join(ids, " "), Q(dir), Q(dir))
	b.WriteString(l.StopScript(ids))
	return b.String()
}

func ensureNL(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
