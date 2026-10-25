package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/securecrt"
)

// Saved SecureCRT passwords are decrypted on import, handed to the caller for
// the OS keychain and removed from the session files.
func TestImportSecureCRTPasswords(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	cfg := filepath.Join(t.TempDir(), "Config", "Sessions", "Lab")
	os.MkdirAll(cfg, 0o755)
	write := func(name, body string) { os.WriteFile(filepath.Join(cfg, name+".ini"), []byte(body), 0o644) }
	write("sw1", "S:\"Hostname\"=10.0.0.1\r\nS:\"Protocol Name\"=SSH2\r\nS:\"Password V2\"="+securecrt.EncryptV2("pw-one", "")+"\r\n")
	write("sw2", "S:\"Hostname\"=10.0.0.2\r\nS:\"Password\"="+securecrt.EncryptV1("pw-two")+"\r\n")
	write("sw3", "S:\"Hostname\"=10.0.0.3\r\nS:\"Password V2\"="+securecrt.EncryptV2("pw-three", "cfg")+"\r\n")
	write("sw4", "S:\"Hostname\"=10.0.0.4\r\n")
	res, err := st.Import(filepath.Dir(filepath.Dir(cfg)), ImportOptions{})
	if err != nil || len(res.Imported) != 4 {
		t.Fatalf("%+v %v", res, err)
	}
	if res.Passwords["Lab/sw1"] != "pw-one" || res.Passwords["Lab/sw2"] != "pw-two" || len(res.Passwords) != 2 ||
		len(res.PasswordErrors) != 1 || !strings.HasPrefix(res.PasswordErrors[0], "Lab/sw3") {
		t.Fatalf("%+v", res)
	}
	s, _ := st.Get("Lab/sw1")
	if !s.SavePassword {
		t.Fatal("save password flag not set")
	}
	data, _ := os.ReadFile(st.fsPath("Lab/sw1") + ext)
	if strings.Contains(string(data), "Password V2") {
		t.Fatalf("password left in the session file:\n%s", data)
	}
	// with the configuration passphrase the protected one works too
	res, _ = st.Import(filepath.Join(cfg, "sw3.ini"), ImportOptions{ConfigPassphrase: "cfg", Conflict: "overwrite"})
	if res.Passwords["sw3"] != "pw-three" {
		t.Fatalf("%+v", res)
	}
}
