//go:build linux

package term

import (
	"strings"
	"testing"

	"github.com/creack/pty"
	"go.bug.st/serial"
)

func TestSerialFlowControl(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skip(err)
	}
	defer ptmx.Close()
	name := tty.Name()
	tty.Close()
	p, err := serial.Open(name, &serial.Mode{BaudRate: 9600})
	if err != nil {
		t.Skip(err)
	}
	defer p.Close()
	for _, mode := range []string{"rtscts", "xonxoff", "none"} {
		if err := setFlowControl(p, strings.TrimSuffix(mode, "none")); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if got, _ := flowControl(p); got != mode {
			t.Fatalf("flow = %q, want %q", got, mode)
		}
	}
}
