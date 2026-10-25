package term

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
)

// The test binary doubles as a fake "ssh" on the jump station: with -s it serves
// SFTP on stdin/stdout, otherwise it runs the last argument with sh (like the
// remote shell of the target would). Arguments are logged for the assertions.
func init() {
	if os.Getenv("TW_FAKE_SSH") != "1" {
		return
	}
	args := os.Args[1:]
	if f, err := os.OpenFile(os.Getenv("TW_FAKE_SSH_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintln(f, strings.Join(args, " "))
		f.Close()
	}
	for _, a := range args {
		if a == "-s" {
			srv, err := sftp.NewServer(stdio{})
			if err == nil {
				_ = srv.Serve()
			}
			os.Exit(0)
		}
	}
	cmd := exec.Command("sh", "-c", args[len(args)-1])
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdout.Close() }

// SFTP and SCP reach the remote command's target through the jump station.
func TestSFTPThroughHop(t *testing.T) {
	if _, err := exec.LookPath("scp"); err != nil {
		t.Skip("scp not installed")
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "args.log")
	exe, _ := os.Executable()
	script := fmt.Sprintf("#!/bin/sh\nTW_FAKE_SSH=1 TW_FAKE_SSH_LOG=%q exec %q \"$@\"\n", log, exe)
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	addr := startSSHServer(t, "pw")
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	autoAnswer(m, em, "pw")
	defer m.CloseAll()
	s := sshSession(addr)
	s.UseRemoteCommand = true
	s.RemoteCommand = filepath.Join(bin, "ssh") + " -t -i ~/.ssh/aws_key ec2-user@10.0.0.5"

	if _, _, _, err := m.SFTPConnect(sshSession(addr), "target"); err == nil {
		t.Fatal("target without a remote command should fail")
	}
	id, _, info, err := m.SFTPConnect(s, "target")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(info, "→ ec2-user@10.0.0.5") {
		t.Fatalf("info %q", info)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	list, err := m.SFTPList(id, dir)
	if err != nil || len(list) != 1 || list[0].Name != "a.txt" {
		t.Fatalf("list %v %v", list, err)
	}
	ch := xferChan(em)
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "up.txt"), []byte("through the hop"), 0o644)
	if _, err := m.SCPTransferSFTP(id, "up", filepath.Join(src, "up.txt"), dir); err != nil {
		t.Fatal(err)
	}
	if tr := waitXfer(t, ch); tr.State != "done" {
		t.Fatalf("%+v", tr)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "up.txt")); string(b) != "through the hop" {
		t.Fatalf("got %q", b)
	}
	b, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || lines[0] != "-T -o BatchMode=yes -i "+os.Getenv("HOME")+"/.ssh/aws_key -s ec2-user@10.0.0.5 sftp" ||
		!strings.HasPrefix(lines[1], "-T -o BatchMode=yes -i") || !strings.Contains(lines[1], "ec2-user@10.0.0.5 scp -t") {
		t.Fatalf("ssh calls:\n%s", b)
	}
	if want := filepath.Base(os.Getenv("SHELL")); os.Getenv("SHELL") != "" {
		if got := m.SFTPLoginShell(id); got != want {
			t.Fatalf("login shell through hop: %q, want %q", got, want)
		}
	}
	// Run As on the target, through the jump station (the target gets the terminal)
	os.WriteFile(filepath.Join(bin, "sftp-server"), []byte(fmt.Sprintf("#!/bin/sh\nTW_FAKE_SSH=1 TW_FAKE_SSH_LOG=/dev/null exec %q -s x sftp\n", exe)), 0o755)
	oldPaths := sftpServerPaths
	sftpServerPaths = []string{filepath.Join(bin, "sftp-server")}
	defer func() { sftpServerPaths = oldPaths }()
	if _, user, err := m.SFTPRunAs(id, "sh"); err != nil || user == "" {
		t.Fatalf("run as through hop: %q %v", user, err)
	}
	if _, err := m.SFTPList(id, dir); err != nil {
		t.Fatal(err)
	}
	// mode "" follows the session setting
	id2, _, info2, err := m.SFTPConnect(s, "")
	if err != nil || strings.Contains(info2, "→") {
		t.Fatalf("jump mode %q %v", info2, err)
	}
	m.SFTPClose(id2)
}

// Browse as root: sftp-server is started through sudo, the password is sent
// only when sudo prompts for it.
func TestSFTPAsRoot(t *testing.T) {
	bin := t.TempDir()
	exe, _ := os.Executable()
	sudoLog := filepath.Join(bin, "sudo.log")
	os.WriteFile(filepath.Join(bin, "sftp-server"), []byte(fmt.Sprintf("#!/bin/sh\nTW_FAKE_SSH=1 TW_FAKE_SSH_LOG=/dev/null exec %q -s x sftp\n", exe)), 0o755)
	os.WriteFile(filepath.Join(bin, "sudo"), []byte(fmt.Sprintf(`#!/bin/sh
[ "$1" = "-S" ] && [ "$2" = "-p" ] || exit 2
prompt=$3; shift 3
printf '%%s' "$prompt" >&2
read pw
[ "$pw" = "pw" ] || { echo "Sorry, try again." >&2; exit 1; }
echo "ok $*" >> %q
exec "$@"
`, sudoLog)), 0o755)
	old := sftpServerPaths
	sftpServerPaths = []string{filepath.Join(bin, "sftp-server")}
	defer func() { sftpServerPaths = old }()
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	addr := startSSHServer(t, "pw")
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	autoAnswer(m, em, "pw")
	defer m.CloseAll()
	id, _, _, err := m.SFTPConnect(sshSession(addr), "")
	if err != nil {
		t.Fatal(err)
	}
	home, err := m.SFTPSetRoot(id, true)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "r.txt"), []byte("x"), 0o644)
	if list, err := m.SFTPList(id, dir); err != nil || len(list) != 1 {
		t.Fatalf("list as root: %v %v (home %q)", list, err, home)
	}
	if b, _ := os.ReadFile(sudoLog); !strings.Contains(string(b), "ok sh -c") {
		t.Fatalf("sudo not used: %q", b)
	}
	if _, err := m.SCPTransferSFTP(id, "up", filepath.Join(dir, "r.txt"), dir); err == nil {
		t.Fatal("SCP as root should be refused")
	}
	if _, err := m.SFTPSetRoot(id, false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SFTPList(id, dir); err != nil {
		t.Fatal(err)
	}
}

// Run As: a command that switches user (here a fake "su") is answered with
// the password, then the new shell is replaced by sftp-server.
func TestSFTPRunAs(t *testing.T) {
	bin := t.TempDir()
	exe, _ := os.Executable()
	os.WriteFile(filepath.Join(bin, "sftp-server"), []byte(fmt.Sprintf("#!/bin/sh\nTW_FAKE_SSH=1 TW_FAKE_SSH_LOG=/dev/null exec %q -s x sftp\n", exe)), 0o755)
	os.WriteFile(filepath.Join(bin, "fakesu"), []byte(`#!/bin/sh
printf 'Last login: today\r\n'
printf 'Password: '
read pw
[ "$pw" = "pw" ] || { echo "su: Authentication failure"; exit 1; }
printf 'switched to %s\r\n' "$1"
exec sh
`), 0o755)
	old := sftpServerPaths
	sftpServerPaths = []string{filepath.Join(bin, "sftp-server")}
	defer func() { sftpServerPaths = old }()
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	addr := startSSHServer(t, "pw")
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	autoAnswer(m, em, "pw")
	defer m.CloseAll()
	id, _, _, err := m.SFTPConnect(sshSession(addr), "")
	if err != nil {
		t.Fatal(err)
	}
	_, user, err := m.SFTPRunAs(id, "fakesu oracle")
	if err != nil || user == "" {
		t.Fatalf("run as: %q %v", user, err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), []byte("x"), 0o644)
	if list, err := m.SFTPList(id, dir); err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	if _, _, err := m.SFTPRunAs(id, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SFTPList(id, dir); err != nil {
		t.Fatal(err)
	}
	// a command that fails reports its output
	if _, _, err := m.SFTPRunAs(id, "echo nope; exit 1"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected failure with output, got %v", err)
	}
}
