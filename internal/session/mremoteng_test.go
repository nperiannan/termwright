package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

func mrngSealGCM(t *testing.T, plain, master string, iter int) string {
	t.Helper()
	salt := []byte("0123456789abcdef")
	nonce := []byte("fedcba9876543210")
	key := pbkdf2.Key([]byte(master), salt, iter, 32, sha1.New)
	b, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCMWithNonceSize(b, 16)
	ct := g.Seal(nil, nonce, []byte(plain), salt)
	return base64.StdEncoding.EncodeToString(append(append(append([]byte{}, salt...), nonce...), ct...))
}

func mrngSealCBC(plain, master string) string {
	sum := md5.Sum([]byte(master))
	b, _ := aes.NewCipher(sum[:])
	iv := []byte("ivivivivivivivIV")
	pad := 16 - len(plain)%16
	msg := append([]byte(plain), []byte(strings.Repeat(string(rune(pad)), pad))...)
	out := make([]byte, len(msg))
	cipher.NewCBCEncrypter(b, iv).CryptBlocks(out, msg)
	return base64.StdEncoding.EncodeToString(append(iv, out...))
}

func mrngFile(t *testing.T, mode, master string) string {
	pw := func(s string) string {
		if mode == "CBC" {
			return mrngSealCBC(s, master)
		}
		return mrngSealGCM(t, s, master, 1000)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<mrng:Connections xmlns:mrng="http://mremoteng.org" Name="Connections" Export="false" EncryptionEngine="AES" BlockCipherMode="%s" KdfIterations="1000" FullFileEncryption="false" Protected="" ConfVersion="2.6">
  <Node Name="Lab" Type="Container" Username="labadmin" Password="%s" Protocol="SSH2" Port="2200">
    <Node Name="sw1" Type="Connection" Hostname="10.0.0.1" Username="" Password="" Protocol="SSH2" Port="22" InheritUsername="True" InheritPassword="True" InheritPort="True" Descr="core switch&#xD;&#xA;rack 4" />
    <Node Name="Inner" Type="Container">
      <Node Name="win1" Type="Connection" Hostname="10.0.0.9" Username="Administrator" Domain="CORP" Password="%s" Protocol="RDP" Port="3390" />
    </Node>
  </Node>
  <Node Name="vnc1" Type="Connection" Hostname="10.0.1.5" Protocol="VNC" Port="5901" />
  <Node Name="oldbox" Type="Connection" Hostname="10.0.1.6" Protocol="Telnet" Port="23" />
  <Node Name="web" Type="Connection" Hostname="10.0.1.7" Protocol="HTTP" Port="80" />
  <Node Name="ancient" Type="Connection" Hostname="10.0.1.8" Protocol="SSH1" />
</mrng:Connections>`, mode, pw("labpass"), pw("winpass"))
}

func TestParseMRemoteNG(t *testing.T) {
	for _, mode := range []string{"GCM", "CBC"} {
		t.Run(mode, func(t *testing.T) {
			list, skipped, locked, err := ParseMRemoteNG([]byte(mrngFile(t, mode, mrngDefaultMaster)), "")
			if err != nil {
				t.Fatal(err)
			}
			if locked != 0 {
				t.Fatalf("locked = %d", locked)
			}
			if len(list) != 4 || len(skipped) != 2 {
				t.Fatalf("got %d sessions, %d skipped: %v", len(list), len(skipped), skipped)
			}
			by := map[string]MRemoteNGSession{}
			for _, it := range list {
				by[JoinPath(it.Session.Folder, it.Session.Name)] = it
			}
			sw := by["Lab/sw1"]
			if sw.Session.Hostname != "10.0.0.1" || sw.Session.Username != "labadmin" || sw.Session.Port != 2200 || sw.Password != "labpass" {
				t.Errorf("sw1 should inherit user, port and password: %+v pw=%q", sw.Session, sw.Password)
			}
			if sw.Session.Protocol != ProtoSSH2 || len(sw.Session.Description) != 2 {
				t.Errorf("sw1 protocol/description: %+v", sw.Session)
			}
			win := by["Lab/Inner/win1"]
			if win.Session.Protocol != ProtoRDP || win.Session.RDPDomain != "CORP" || win.Session.Port != 3390 || win.Password != "winpass" {
				t.Errorf("win1: %+v pw=%q", win.Session, win.Password)
			}
			if v := by["vnc1"]; v.Session.Protocol != ProtoVNC || v.Session.Port != 5901 {
				t.Errorf("vnc1: %+v", v.Session)
			}
			if v := by["oldbox"]; v.Session.Protocol != ProtoTelnet || v.Session.Port != 23 {
				t.Errorf("oldbox: %+v", v.Session)
			}
			if !strings.Contains(strings.Join(skipped, "\n"), "HTTP") || !strings.Contains(strings.Join(skipped, "\n"), "SSH1") {
				t.Errorf("skipped = %v", skipped)
			}
		})
	}
}

func TestParseMRemoteNGMasterPassword(t *testing.T) {
	data := []byte(mrngFile(t, "GCM", "s3cret"))
	_, _, locked, err := ParseMRemoteNG(data, "")
	if err != nil || locked != 2 {
		t.Fatalf("without the master password: locked=%d err=%v", locked, err)
	}
	list, _, locked, err := ParseMRemoteNG(data, "s3cret")
	if err != nil || locked != 0 {
		t.Fatalf("with it: locked=%d err=%v", locked, err)
	}
	for _, it := range list {
		if it.Session.Name == "win1" && it.Password != "winpass" {
			t.Errorf("win1 password = %q", it.Password)
		}
	}
}

func TestParseMRemoteNGFullFileEncryption(t *testing.T) {
	_, _, _, err := ParseMRemoteNG([]byte(`<mrng:Connections xmlns:mrng="http://mremoteng.org" FullFileEncryption="true" ConfVersion="2.6">c29tZWNpcGhlcg==</mrng:Connections>`), "")
	if err == nil || !strings.Contains(err.Error(), "encrypted as a whole") {
		t.Fatalf("err = %v", err)
	}
}

func TestImportMRemoteNG(t *testing.T) {
	root := t.TempDir()
	st, _ := NewStore(root)
	file := filepath.Join(t.TempDir(), "confCons.xml")
	if err := os.WriteFile(file, []byte(mrngFile(t, "GCM", mrngDefaultMaster)), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := st.Import(file, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Imported) != 4 || len(res.Skipped) != 2 || len(res.Passwords) != 2 {
		t.Fatalf("imported=%d skipped=%d passwords=%d", len(res.Imported), len(res.Skipped), len(res.Passwords))
	}
	got, err := st.Get("Lab/sw1")
	if err != nil || got.Username != "labadmin" || !got.SavePassword {
		t.Fatalf("Lab/sw1: %+v %v", got, err)
	}
	// the password is handed to the caller for the keychain, never written to the file
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			raw, _ := os.ReadFile(p)
			for dest, pw := range res.Passwords {
				if strings.Contains(string(raw), pw) {
					t.Errorf("password of %s is in %s", dest, p)
				}
			}
		}
		return nil
	})
}
