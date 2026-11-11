package emu

import (
	"encoding/json"
	"errors"
	"strings"
)

// appliance is the part of a GNS3 appliance file (.gns3a, public JSON) that says how to boot an image.
type appliance struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Vendor   string `json:"vendor_name"`
	Usage    string `json:"usage"`
	Qemu     *struct {
		AdapterType string `json:"adapter_type"`
		Adapters    int    `json:"adapters"`
		RAM         int    `json:"ram"`
		CPUs        int    `json:"cpus"`
		DiskIf      string `json:"hda_disk_interface"`
		Arch        string `json:"arch"`
		Console     string `json:"console_type"`
		Options     string `json:"options"`
	} `json:"qemu"`
	PortName string `json:"port_name_format"`
	Images   []struct {
		Filename string `json:"filename"`
		Version  string `json:"version"`
		Size     int64  `json:"filesize"`
		MD5      string `json:"md5sum"`
		URL      string `json:"direct_download_url"`
	} `json:"images"`
	Versions []struct {
		Name   string            `json:"name"`
		Images map[string]string `json:"images"`
	} `json:"versions"`
}

// Appliance is an image profile from a GNS3 appliance file, per version it lists.
type Appliance struct {
	Name     string  `json:"name"`
	Usage    string  `json:"usage"`
	Profiles []Image `json:"profiles"` // FileName says which image file each version needs
}

// ParseAppliance reads a GNS3 appliance file.
func ParseAppliance(data []byte) (Appliance, error) {
	var a appliance
	if err := json.Unmarshal(data, &a); err != nil {
		return Appliance{}, errors.New("not a GNS3 appliance file: " + err.Error())
	}
	if a.Qemu == nil {
		return Appliance{}, errors.New(a.Name + " is not a QEMU appliance (only QEMU images can run on a KVM host)")
	}
	if a.Qemu.Arch != "" && a.Qemu.Arch != "x86_64" && a.Qemu.Arch != "i386" {
		return Appliance{}, errors.New(a.Name + " needs " + a.Qemu.Arch + "; KVM hosts run x86_64")
	}
	base := Image{Name: a.Name, CPUs: a.Qemu.CPUs, RAM: a.Qemu.RAM, NICs: a.Qemu.Adapters, NIC: a.Qemu.AdapterType, Disk: a.Qemu.DiskIf,
		PortName: gnsPortName(a.PortName), Ready: "login:", Extra: cleanOptions(a.Qemu.Options), Source: "GNS3 appliance " + a.Name}
	if base.CPUs == 0 {
		base.CPUs = 1
	}
	if base.NIC == "" {
		base.NIC = "e1000"
	}
	out := Appliance{Name: a.Name, Usage: a.Usage}
	for _, v := range a.Versions {
		f := v.Images["hda_disk_image"]
		if f == "" {
			continue
		}
		im := base
		im.FileName = f
		im.Version = strings.TrimPrefix(strings.TrimPrefix(v.Name, "v"), "V")
		for _, x := range a.Images {
			if x.Filename == f {
				im.Size, im.MD5, im.URL = x.Size, strings.ToLower(x.MD5), x.URL
				if x.Version != "" {
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
