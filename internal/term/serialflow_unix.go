//go:build linux || darwin

package term

import (
	"errors"
	"reflect"

	"go.bug.st/serial"
	"golang.org/x/sys/unix"
)

// portFD reads the file descriptor of a go.bug.st/serial port (v1.6.x keeps
// it in an unexported "handle" field).
func portFD(p serial.Port) (int, error) {
	v := reflect.ValueOf(p)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, errors.New("unsupported serial port type")
	}
	f := v.FieldByName("handle")
	if !f.IsValid() || (f.Kind() != reflect.Int && f.Kind() != reflect.Int32) {
		return 0, errors.New("cannot access the serial port handle")
	}
	return int(f.Int()), nil
}

// setFlowControl enables "rtscts" (hardware) or "xonxoff" (software) flow control.
func setFlowControl(p serial.Port, mode string) error {
	fd, err := portFD(p)
	if err != nil {
		return err
	}
	t, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return err
	}
	t.Cflag &^= unix.CRTSCTS
	t.Iflag &^= unix.IXON | unix.IXOFF
	switch mode {
	case "rtscts":
		t.Cflag |= unix.CRTSCTS
	case "xonxoff":
		t.Iflag |= unix.IXON | unix.IXOFF
	}
	return unix.IoctlSetTermios(fd, ioctlSetTermios, t)
}

// flowControl reports the current mode (used by tests).
func flowControl(p serial.Port) (string, error) {
	fd, err := portFD(p)
	if err != nil {
		return "", err
	}
	t, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return "", err
	}
	switch {
	case t.Cflag&unix.CRTSCTS != 0:
		return "rtscts", nil
	case t.Iflag&unix.IXON != 0:
		return "xonxoff", nil
	}
	return "none", nil
}
