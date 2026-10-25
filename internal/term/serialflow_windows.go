//go:build windows

package term

import (
	"errors"
	"reflect"

	"go.bug.st/serial"
	"golang.org/x/sys/windows"
)

const (
	dcbOutxCtsFlow  = 0x00000004
	dcbOutX         = 0x00000100
	dcbInX          = 0x00000200
	dcbRtsMask      = 0x00003000
	dcbRtsEnable    = 0x00001000
	dcbRtsHandshake = 0x00002000
)

func portHandle(p serial.Port) (windows.Handle, error) {
	v := reflect.ValueOf(p)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, errors.New("unsupported serial port type")
	}
	f := v.FieldByName("handle")
	if !f.IsValid() {
		return 0, errors.New("cannot access the serial port handle")
	}
	switch f.Kind() {
	case reflect.Uintptr, reflect.Uint, reflect.Uint64, reflect.Uint32:
		return windows.Handle(f.Uint()), nil
	}
	return 0, errors.New("cannot access the serial port handle")
}

// setFlowControl enables "rtscts" (hardware) or "xonxoff" (software) flow control.
func setFlowControl(p serial.Port, mode string) error {
	h, err := portHandle(p)
	if err != nil {
		return err
	}
	var dcb windows.DCB
	dcb.DCBlength = uint32(reflect.TypeOf(dcb).Size())
	if err := windows.GetCommState(h, &dcb); err != nil {
		return err
	}
	dcb.Flags &^= dcbOutxCtsFlow | dcbOutX | dcbInX | dcbRtsMask
	switch mode {
	case "rtscts":
		dcb.Flags |= dcbOutxCtsFlow | dcbRtsHandshake
	case "xonxoff":
		dcb.Flags |= dcbOutX | dcbInX | dcbRtsEnable
		dcb.XonChar, dcb.XoffChar = 0x11, 0x13
	default:
		dcb.Flags |= dcbRtsEnable
	}
	return windows.SetCommState(h, &dcb)
}
