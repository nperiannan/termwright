package emu

import (
	"encoding/json"
	"errors"
	"strings"
)

// qemuProps are the QEMU settings of an appliance (registry v4: "qemu";
// v8: a "settings" entry's "template_properties").
type qemuProps struct {
	AdapterType string `json:"adapter_type"`
	Adapters    int    `json:"adapters"`
	RAM         int    `json:"ram"`
	CPUs        int    `json:"cpus"`
	DiskIf      string `json:"hda_disk_interface"`
	HdbIf       string `json:"hdb_disk_interface"`
	HdcIf       string `json:"hdc_disk_interface"`
	HddIf       string `json:"hdd_disk_interface"`
	Arch        string `json:"arch"`
	Platform    string `json:"platform"`
	Options     string `json:"options"`
	PortName    string `json:"port_name_format"`
}

// over puts the values set in o over p.
func (p qemuProps) over(o qemuProps) qemuProps {
	if o.AdapterType != "" {
		p.AdapterType = o.AdapterType
	}
	if o.Adapters != 0 {
		p.Adapters = o.Adapters
	}
	if o.RAM != 0 {
		p.RAM = o.RAM
	}
	if o.CPUs != 0 {
		p.CPUs = o.CPUs
	}
	if o.DiskIf != "" {
		p.DiskIf = o.DiskIf
	}
	if o.HdbIf != "" {
		p.HdbIf = o.HdbIf
	}
	if o.HdcIf != "" {
		p.HdcIf = o.HdcIf
	}
	if o.HddIf != "" {
		p.HddIf = o.HddIf
	}
	if o.Arch != "" {
		p.Arch = o.Arch
	}
	if o.Platform != "" {
		p.Platform = o.Platform
	}
	if o.Options != "" {
		p.Options = o.Options
	}
	if o.PortName != "" {
		p.PortName = o.PortName
	}
	return p
}

// appliance is the part of a GNS3 appliance file (.gns3a, public JSON) that says how to boot an image.
type appliance struct {
	Name     string     `json:"name"`
	Category string     `json:"category"`
	Vendor   string     `json:"vendor_name"`
	Usage    string     `json:"usage"`
	User     string     `json:"default_username"`
	Password string     `json:"default_password"`
	Qemu     *qemuProps `json:"qemu"`
	Settings []struct {
		Name    string    `json:"name"`
		Default bool      `json:"default"`
		Inherit bool      `json:"inherit_default_properties"`
		Type    string    `json:"template_type"`
		Props   qemuProps `json:"template_properties"`
	} `json:"settings"`
	PortName string `json:"port_name_format"`
	Images   []struct {
		Filename string `json:"filename"`
		Version  string `json:"version"`
		Size     int64  `json:"filesize"`
		MD5      string `json:"md5sum"`
		URL      string `json:"direct_download_url"`
		Page     string `json:"download_url"`
	} `json:"images"`
	Versions []struct {
		Name     string            `json:"name"`
		Settings string            `json:"settings"`
		Images   map[string]string `json:"images"`
	} `json:"versions"`
}

// Appliance is an image profile from a GNS3 appliance file, per version it lists.
type Appliance struct {
	Name     string  `json:"name"`
	Usage    string  `json:"usage"`
	User     string  `json:"user"`
	Password string  `json:"password"`
	Profiles []Image `json:"profiles"` // FileName says which image file each version needs
}

// ParseAppliance reads a GNS3 appliance file.
func ParseAppliance(data []byte) (Appliance, error) {
	var a appliance
	if err := json.Unmarshal(data, &a); err != nil {
		return Appliance{}, errors.New("not a GNS3 appliance file: " + err.Error())
	}
	// the QEMU settings: v4 "qemu", or v8's default "settings" entry (others per version)
	var def *qemuProps
	named := map[string]qemuProps{}
	if a.Qemu != nil {
		def = a.Qemu
	}
	for _, st := range a.Settings {
		if st.Type != "" && st.Type != "qemu" {
			continue
		}
		if st.Default && def == nil {
			p := st.Props
			def = &p
		}
		named[st.Name] = st.Props
	}
	if def == nil {
		for _, st := range a.Settings { // no default: the first QEMU one
			if st.Type == "" || st.Type == "qemu" {
				p := st.Props
				def = &p
				break
			}
		}
	}
	if def == nil {
		return Appliance{}, errors.New(a.Name + " is not a QEMU appliance (only QEMU images can run on a KVM host)")
	}
	arch := orStr(def.Arch, def.Platform)
	if arch != "" && arch != "x86_64" && arch != "i386" {
		return Appliance{}, errors.New(a.Name + " needs " + arch + "; KVM hosts run x86_64")
	}
	mk := func(q qemuProps) Image {
		im := Image{Name: a.Name, CPUs: q.CPUs, RAM: q.RAM, NICs: q.Adapters, NIC: q.AdapterType, Disk: q.DiskIf,
			PortName: gnsPortName(orStr(q.PortName, a.PortName)), Ready: "login:", Login: a.User, Extra: cleanOptions(q.Options), Source: "GNS3 appliance " + a.Name}
		if im.CPUs == 0 {
			im.CPUs = 1
		}
		if im.NIC == "" {
			im.NIC = "e1000"
		}
		return im
	}
	base := mk(*def)
	out := Appliance{Name: a.Name, Usage: a.Usage, User: a.User, Password: a.Password}
	for _, v := range a.Versions {
		f := v.Images["hda_disk_image"]
		if f == "" {
			continue
		}
		im := base
		q := *def
		if o, ok := named[v.Settings]; ok && v.Settings != "" {
			q = def.over(o)
			im = mk(q)
		}
		im.Disks = nil
		for _, sl := range []struct{ key, slot, bus string }{{"hdb_disk_image", "hdb", q.HdbIf}, {"hdc_disk_image", "hdc", q.HdcIf}, {"hdd_disk_image", "hdd", q.HddIf}, {"cdrom_image", "cdrom", ""}, {"bios_image", "bios", ""}} {
			if fn := v.Images[sl.key]; fn != "" {
				d := Disk{Slot: sl.slot, FileName: fn, Bus: sl.bus}
				for _, x := range a.Images {
					if x.Filename == fn {
						d.Size, d.MD5, d.URL, d.Page = x.Size, strings.ToLower(x.MD5), x.URL, x.Page
					}
				}
				im.Disks = append(im.Disks, d)
			}
		}
		im.FileName = f
		im.Version = strings.TrimPrefix(strings.TrimPrefix(v.Name, "v"), "V")
		for _, x := range a.Images {
			if x.Filename == f {
				im.Size, im.MD5, im.URL, im.Page = x.Size, strings.ToLower(x.MD5), x.URL, x.Page
				if x.Version != "" && v.Name == "" {
					im.Version = strings.TrimPrefix(strings.TrimPrefix(x.Version, "v"), "V")
				}
			}
		}
		out.Profiles = append(out.Profiles, im)
	}
	if len(out.Profiles) == 0 {
		out.Profiles = append(out.Profiles, base)
	}
	return out, nil
}

// gnsPortName turns GNS3's "Ethernet{0}" / "1/{port1}" into "{n}" style (data port n).
func gnsPortName(f string) string {
	if f == "" {
		return ""
	}
	f = strings.NewReplacer("{port1}", "{n}", "{0}", "{n}", "{port0}", "{n}").Replace(f)
	return f
}

// cleanOptions drops the options Termwright sets itself.
func cleanOptions(o string) string {
	a := splitArgs(o)
	var keep []string
	for i := 0; i < len(a); i++ {
		switch a[i] {
		case "-nographic", "-enable-kvm", "-daemonize":
			continue
		case "-display", "-serial", "-monitor":
			i++
			continue
		}
		keep = append(keep, a[i])
	}
	return strings.Join(keep, " ")
}
