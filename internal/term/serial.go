package term

import (
	"errors"
	"fmt"
	"time"

	"go.bug.st/serial"

	"github.com/termwright/termwright/internal/session"
)

type serialConn struct {
	p    serial.Port
	info string
}

func (s *serialConn) Read(p []byte) (int, error)  { return s.p.Read(p) }
func (s *serialConn) Write(p []byte) (int, error) { return s.p.Write(p) }
func (s *serialConn) Resize(int, int) error       { return nil }
func (s *serialConn) Close() error                { return s.p.Close() }
func (s *serialConn) Info() string                { return s.info }
func (s *serialConn) SendBreak() error            { return s.p.Break(300 * time.Millisecond) }

// serialMode is the port setup of a session (9600 8N1 when not set).
func serialMode(s session.Session) serial.Mode {
	mode := serial.Mode{BaudRate: s.BaudRate, DataBits: s.DataBits,
		InitialStatusBits: &serial.ModemOutputBits{DTR: s.SerialDTR, RTS: s.SerialRTS}}
	if mode.BaudRate == 0 {
		mode.BaudRate = 9600
	}
	if mode.DataBits == 0 {
		mode.DataBits = 8
	}
	switch s.StopBits {
	case 1:
		mode.StopBits = serial.OnePointFiveStopBits
	case 2:
		mode.StopBits = serial.TwoStopBits
	default:
		mode.StopBits = serial.OneStopBit
	}
	switch s.Parity {
	case 1:
		mode.Parity = serial.OddParity
	case 2:
		mode.Parity = serial.EvenParity
	case 3:
		mode.Parity = serial.MarkParity
	case 4:
		mode.Parity = serial.SpaceParity
	default:
		mode.Parity = serial.NoParity
	}
	return mode
}

func openSerial(s session.Session) (Conn, error) {
	if s.ComPort == "" {
		return nil, errors.New("no serial port selected")
	}
	mode := serialMode(s)
	detected := ""
	if s.SerialAutoBaud {
		if b, _, err := DetectBaud(s.ComPort, mode, nil, 0); err != nil {
			return nil, err
		} else if b > 0 {
			mode.BaudRate, detected = b, " (detected)"
		} else {
			detected = " (could not detect the speed)"
		}
	}
	p, err := serial.Open(s.ComPort, &mode)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", s.ComPort, err)
	}
	if s.SerialFlow == "rtscts" || s.SerialFlow == "xonxoff" {
		if err := setFlowControl(p, s.SerialFlow); err != nil {
			p.Close()
			return nil, fmt.Errorf("flow control: %w", err)
		}
	}
	parity := []string{"N", "O", "E", "M", "S"}
	pi := "N"
	if s.Parity >= 0 && s.Parity < len(parity) {
		pi = parity[s.Parity]
	}
	return &serialConn{p: p, info: fmt.Sprintf("Serial %s %d %d%s%d%s", s.ComPort, mode.BaudRate, mode.DataBits, pi, map[int]int{0: 1, 1: 1, 2: 2}[s.StopBits], detected)}, nil
}

// SerialPorts lists available serial ports.
func SerialPorts() []string {
	ports, err := serial.GetPortsList()
	if err != nil || ports == nil {
		return []string{}
	}
	return ports
}
