package term

import (
	"bytes"
	"context"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// runPeer starts an lrzsz program connected to our implementation through pipes.
func runPeer(t *testing.T, dir string, args ...string) (<-chan byte, func([]byte) error, *exec.Cmd) {
	t.Helper()
	if _, err := exec.LookPath(args[0]); err != nil {
		t.Skip(args[0] + " (lrzsz) not installed")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	in := make(chan byte, 1<<16)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			for _, b := range buf[:n] {
				in <- b
			}
			if err != nil {
				return
			}
		}
	}()
	write := func(p []byte) error { _, err := stdin.Write(p); return err }
	return in, write, cmd
}

func randomFile(t *testing.T, dir, name string, n int) []byte {
	data := make([]byte, n)
	rand.New(rand.NewSource(int64(n))).Read(data)
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestZmodemReceiveFromSz(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	a := randomFile(t, src, "a.bin", 70000)
	b := randomFile(t, src, "b.txt", 10)
	in, write, cmd := runPeer(t, src, "sz", "-q", "a.bin", "b.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	files, err := zmodemReceive(ctx, in, write, dst, func(string, int64, int64) {})
	cmd.Wait()
	if err != nil || len(files) != 2 {
		t.Fatalf("%v %v", err, files)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "a.bin")); !bytes.Equal(got, a) {
		t.Fatal("a.bin differs")
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "b.txt")); !bytes.Equal(got, b) {
		t.Fatal("b.txt differs")
	}
}

func TestZmodemSendToRz(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	a := randomFile(t, src, "fw.img", 150001)
	in, write, cmd := runPeer(t, dst, "rz", "-q")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := zmodemSend(ctx, in, write, []string{filepath.Join(src, "fw.img")}, func(string, int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if got, _ := os.ReadFile(filepath.Join(dst, "fw.img")); !bytes.Equal(got, a) {
		t.Fatalf("received %d bytes, want %d", len(got), len(a))
	}
}

func TestXmodemReceiveFromSx(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	a := randomFile(t, src, "x.bin", 5000)
	in, write, cmd := runPeer(t, src, "sx", "-q", "x.bin")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out := filepath.Join(dst, "x.bin")
	if err := xmodemReceive(ctx, write, in, out, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, a) {
		t.Fatalf("got %d bytes", len(got))
	}
}

func TestYmodemReceiveFromSb(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	a := randomFile(t, src, "a.bin", 3000)
	b := randomFile(t, src, "b.bin", 100)
	in, write, cmd := runPeer(t, src, "sb", "-q", "a.bin", "b.bin")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	files, err := ymodemReceive(ctx, write, in, dst, func(string, int64, int64) {})
	cmd.Wait()
	if err != nil || len(files) != 2 {
		t.Fatalf("%v %v", err, files)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "a.bin")); !bytes.Equal(got, a) {
		t.Fatal("a differs")
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "b.bin")); !bytes.Equal(got, b) {
		t.Fatal("b differs")
	}
}

func TestYmodemSendToRb(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	a := randomFile(t, src, "fw.bin", 5000)
	b := randomFile(t, src, "s.txt", 50)
	in, write, cmd := runPeer(t, dst, "rb", "-q")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ymodemSend(ctx, write, in, []string{filepath.Join(src, "fw.bin"), filepath.Join(src, "s.txt")}, func(string, int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if got, _ := os.ReadFile(filepath.Join(dst, "fw.bin")); !bytes.Equal(got, a) {
		t.Fatalf("fw.bin got %d bytes", len(got))
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "s.txt")); !bytes.Equal(got, b) {
		t.Fatalf("s.txt got %d bytes", len(got))
	}
}
