package skills

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSaveListImportZip(t *testing.T) {
	sk, body, err := Parse("---\nname: add-vlan\ndescription: >\n  Add a VLAN everywhere.\ndevice-types:\n  - EXOS\n  - VOSS\n---\n# Add a VLAN\n1. do it\n")
	if err != nil || sk.Name != "add-vlan" || sk.Description != "Add a VLAN everywhere." || len(sk.DeviceTypes) != 2 || !strings.HasPrefix(body, "# Add a VLAN") {
		t.Fatalf("%+v %q %v", sk, body, err)
	}
	for _, bad := range []string{"# no front matter", "---\nname: Bad Name\ndescription: x\n---\n", "---\nname: ok\n---\n"} {
		if _, _, err := Parse(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	st := Store{Dir: t.TempDir()}
	for _, ex := range Examples {
		if _, err := st.Save("", ex); err != nil {
			t.Fatal(err)
		}
	}
	l := st.List()
	if len(l) != 3 || l[2].Name != "upgrade-exos-firmware" || !l[2].For("EXOS") || l[2].For("VOSS") || !l[0].For("VOSS") || l[0].Text != "" {
		t.Fatalf("%+v", l)
	}
	// rename by saving with another name
	g, _ := st.Get("troubleshoot-port")
	if _, err := st.Save("troubleshoot-port", strings.Replace(g.Text, "name: troubleshoot-port", "name: port-check", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get("port-check"); err != nil {
		t.Fatal(err)
	}
	// a zip with a folder and a script, then export and import again
	zp := filepath.Join(t.TempDir(), "s.zip")
	f, _ := os.Create(zp)
	w := zip.NewWriter(f)
	a, _ := w.Create("onboard/SKILL.md")
	_, _ = a.Write([]byte("---\nname: onboard-switch\ndescription: Set NTP and syslog.\n---\nsteps\n"))
	b, _ := w.Create("onboard/config.txt")
	_, _ = b.Write([]byte("ntp server 10.0.0.1\n"))
	c, _ := w.Create("../evil.txt")
	_, _ = c.Write([]byte("x"))
	_ = w.Close()
	_ = f.Close()
	got, err := st.Import(zp, false)
	if err != nil || got.Name != "onboard-switch" || len(got.Files) != 1 {
		t.Fatal(err, got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(st.Dir), "evil.txt")); err == nil {
		t.Fatal("wrote outside")
	}
	if _, err := st.Import(zp, false); err == nil {
		t.Fatal("replaced without asking")
	}
	z, err := st.Zip("onboard-switch")
	if err != nil || len(z) == 0 {
		t.Fatal(err)
	}
	if err := st.Remove("onboard-switch"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get("onboard-switch"); err == nil {
		t.Fatal("not removed")
	}
	if Slug("Upgrade EXOS firmware!") != "upgrade-exos-firmware" {
		t.Fatal(Slug("Upgrade EXOS firmware!"))
	}
}
