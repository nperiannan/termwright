package session

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := []byte{0xFF, 0xFE}
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return b
}

const puttyReg = "Windows Registry Editor Version 5.00\r\n\r\n" +
	"[HKEY_CURRENT_USER\\Software\\SimonTatham\\PuTTY\\Sessions\\Default%20Settings]\r\n\"HostName\"=\"\"\r\n\r\n" +
	"[HKEY_CURRENT_USER\\Software\\SimonTatham\\PuTTY\\Sessions\\Core%20Switch]\r\n" +
	"\"HostName\"=\"admin@10.1.1.1\"\r\n\"Protocol\"=\"ssh\"\r\n\"PortNumber\"=dword:00000822\r\n" +
	"\"PublicKeyFile\"=\"C:\\\\keys\\\\lab.ppk\"\r\n\"PortForwardings\"=\"L8080=localhost:80,D1080,R127.0.0.1:2222=10.0.0.5:22\"\r\n" +
	"\"PsmPath\"=\"Lab\\\\Core\"\r\n\"PingIntervalSecs\"=dword:0000001e\r\n\r\n" +
	"[HKEY_CURRENT_USER\\Software\\SimonTatham\\PuTTY\\Sessions\\console]\r\n" +
	"\"Protocol\"=\"serial\"\r\n\"SerialLine\"=\"COM3\"\r\n\"SerialSpeed\"=dword:0001c200\r\n\"SerialDataBits\"=dword:00000008\r\n" +
	"\"SerialStopHalfbits\"=dword:00000002\r\n\"SerialParity\"=dword:00000000\r\n\"SerialFlowControl\"=dword:00000002\r\n"

const moba = "[Bookmarks]\r\nSubRep=\r\nImgNum=42\r\n" +
	"web01=#109#0%10.0.0.7%2222%ec2-user%%-1%-1%%%%%0%0%0%_ProfileDir_\\.ssh\\aws.pem%%-1%0%0%0%%1080%%0%0%1#MobaFont%10%0%0%-1%15#0#Prod web#-1\r\n" +
	"rdp=#91#4%10.0.0.9%3389%%-1%0%0%0%%%%%0%0%%#MobaFont#0# #-1\r\n\r\n" +
	"[Bookmarks_1]\r\nSubRep=Lab\\Switches\r\nImgNum=41\r\n" +
	"sw1=#98#1%10.2.2.2%23%%%2%%%%%0%0%%1080%#MobaFont#0# #-1\r\n" +
	"con=#131#8%COM4  (USB Serial Port (COM4))%115200%3%0%0%1%2%%#MobaFont#0# #-1\r\n"

func TestImportPuTTYAndMoba(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	dir := t.TempDir()
	reg := filepath.Join(dir, "putty.reg")
	os.WriteFile(reg, utf16le(puttyReg), 0o644)
	res, err := st.Import(reg, ImportOptions{})
	if err != nil || len(res.Imported) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	s, err := st.Get("Lab/Core/Core Switch")
	if err != nil || s.Hostname != "10.1.1.1" || s.Username != "admin" || s.Port != 2082 || s.KeepaliveSec != 30 ||
		len(s.PortForwards) != 3 || s.PortForwards[2].BindAddr != "127.0.0.1" || s.PortForwards[1].Type != "dynamic" {
		t.Fatalf("%+v %v", s, err)
	}
	c, err := st.Get("console")
	if err != nil || c.Protocol != ProtoSerial || c.ComPort != "COM3" || c.BaudRate != 115200 || c.SerialFlow != "rtscts" {
		t.Fatalf("%+v %v", c, err)
	}

	mx := filepath.Join(dir, "sessions.mxtsessions")
	os.WriteFile(mx, []byte(moba), 0o644)
	res, err = st.Import(mx, ImportOptions{TargetFolder: "Moba"})
	if err != nil || len(res.Imported) != 3 || len(res.Skipped) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	w, _ := st.Get("Moba/web01")
	if w.Hostname != "10.0.0.7" || w.Port != 2222 || w.Username != "ec2-user" || filepath.Base(filepath.FromSlash(w.IdentityFile)) != "aws.pem" {
		t.Fatalf("%+v", w)
	}
	if sw, _ := st.Get("Moba/Lab/Switches/sw1"); sw.Protocol != ProtoTelnet || sw.Hostname != "10.2.2.2" {
		t.Fatalf("%+v", sw)
	}
	if cn, _ := st.Get("Moba/Lab/Switches/con"); cn.ComPort != "COM4" || cn.BaudRate != 115200 {
		t.Fatalf("%+v", cn)
	}

	// Linux ~/.putty/sessions directory
	pd := filepath.Join(dir, "sessions")
	os.MkdirAll(pd, 0o755)
	os.WriteFile(filepath.Join(pd, "my%20box"), []byte("HostName=box.lan\nProtocol=telnet\nPortNumber=2323\n"), 0o644)
	res, err = st.Import(pd, ImportOptions{})
	if b, _ := st.Get("my box"); err != nil || b.Port != 2323 || b.Protocol != ProtoTelnet {
		t.Fatalf("%+v %+v %v", res, b, err)
	}
}
