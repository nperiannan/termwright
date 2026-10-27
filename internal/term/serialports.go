package term

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"

	"github.com/termwright/termwright/internal/session"
)

// SerialPort is a serial port found on this computer.
type SerialPort struct {
	Name   string `json:"name"`  // COM3, /dev/ttyUSB0, /dev/cu.usbserial-1410
	Label  string `json:"label"` // what it is, e.g. "FT232R USB UART (0403:6001)"
	USB    bool   `json:"usb"`
	Serial string `json:"serial,omitempty"`
}

// SerialPortList lists the serial ports in natural order (COM2 before
// COM10), with the USB adapter's product name when the system reports it.
// On macOS only the /dev/cu.* ports are listed (their /dev/tty.* twins wait
// for carrier detect, which console cables do not give).
func SerialPortList() []SerialPort {
	var out []SerialPort
	seen := map[string]bool{}
	if det, err := enumerator.GetDetailedPortsList(); err == nil {
		for _, d := range det {
			if d == nil || d.Name == "" || seen[d.Name] {
				continue
			}
			seen[d.Name] = true
			p := SerialPort{Name: d.Name, USB: d.IsUSB, Serial: d.SerialNumber}
			label := strings.TrimSpace(d.Product)
			if d.IsUSB && d.VID != "" {
				id := strings.ToLower(d.VID + ":" + d.PID)
				if label == "" {
					label = "USB serial"
				}
				label += " (" + id + ")"
			}
			p.Label = label
			out = append(out, p)
		}
	}
	if names, err := serial.GetPortsList(); err == nil {
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				out = append(out, SerialPort{Name: n})
			}
		}
	}
	return cleanPorts(out, runtime.GOOS)
}

func cleanPorts(in []SerialPort, goos string) []SerialPort {
	out := in[:0:0]
	for _, p := range in {
		if goos == "darwin" && strings.HasPrefix(p.Name, "/dev/tty.") {
			continue
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return naturalLess(out[i].Name, out[j].Name) })
	return out
}

// naturalLess compares names with their numbers as numbers: COM2 < COM10,
// ttyUSB2 < ttyUSB10; letters ignore case.
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		ra, rb := []rune(a), []rune(b)
		if unicode.IsDigit(ra[0]) && unicode.IsDigit(rb[0]) {
			na, ia := leadingNumber(ra)
			nb, ib := leadingNumber(rb)
			if na != nb {
				return na < nb
			}
			a, b = string(ra[ia:]), string(rb[ib:])
			continue
		}
		ca, cb := unicode.ToLower(ra[0]), unicode.ToLower(rb[0])
		if ca != cb {
			return ca < cb
		}
		a, b = string(ra[1:]), string(rb[1:])
	}
	return len(a) < len(b)
}

func leadingNumber(r []rune) (int, int) {
	n, i := 0, 0
	for i < len(r) && unicode.IsDigit(r[i]) && i < 9 {
		n = n*10 + int(r[i]-'0')
		i++
	}
	for i < len(r) && unicode.IsDigit(r[i]) {
		i++
	}
	return n, i
}

// CommonBauds are tried by DetectBaud, the most used first.
var CommonBauds = []int{9600, 115200, 19200, 38400, 57600, 4800, 2400, 230400, 1200}

// BaudResult is what DetectBaud saw at one speed.
type BaudResult struct {
	Baud  int     `json:"baud"`
	Bytes int     `json:"bytes"`
	Score float64 `json:"score"` // share of readable characters, 0-1
}

// DetectBaud finds the speed of a serial console: at each speed it sends a
// carriage return, reads what comes back for a moment and keeps the speed
// whose answer is readable text. The other settings (data bits, parity,
// stop bits) are those of mode. Returns 0 when nothing readable came back.
func DetectBaud(port string, mode serial.Mode, rates []int, listen time.Duration) (int, []BaudResult, error) {
	if port == "" {
		return 0, nil, errors.New("no serial port selected")
	}
	if len(rates) == 0 {
		rates = CommonBauds
	}
	if listen <= 0 {
		listen = 700 * time.Millisecond
	}
	var res []BaudResult
	for _, b := range rates {
		m := mode
		m.BaudRate = b
		p, err := serial.Open(port, &m)
		if err != nil {
			return 0, res, fmt.Errorf("open %s: %w", port, err)
		}
		_ = p.ResetInputBuffer()
		_ = p.SetReadTimeout(100 * time.Millisecond)
		_, _ = p.Write([]byte("\r"))
		var got []byte
		buf := make([]byte, 4096)
		end := time.Now().Add(listen)
		for time.Now().Before(end) && len(got) < 8192 {
			n, err := p.Read(buf)
			if n > 0 {
				got = append(got, buf[:n]...)
			}
			if err != nil {
				break
			}
		}
		_ = p.Close()
		res = append(res, BaudResult{Baud: b, Bytes: len(got), Score: readable(got)})
	}
	return bestBaud(res), res, nil
}

// bestBaud picks the speed with the most readable answer (at least three
// bytes and 85% readable); 0 when none qualifies.
func bestBaud(res []BaudResult) int {
	best, bestScore := 0, 0.0
	for _, r := range res {
		if r.Bytes < 3 || r.Score < 0.85 {
			continue
		}
		// a long readable answer beats a short one of the same quality
		s := r.Score + min(float64(r.Bytes), 200)/2000
		if s > bestScore {
			best, bestScore = r.Baud, s
		}
	}
	return best
}

// readable is the share of bytes that are printable ASCII, line ends, tabs
// or escape sequences; output at a wrong speed is mostly other bytes.
func readable(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	ok := 0
	for _, c := range b {
		if (c >= 0x20 && c < 0x7f) || c == '\r' || c == '\n' || c == '\t' || c == 0x1b || c == '\b' {
			ok++
		}
	}
	return float64(ok) / float64(len(b))
}

// DetectSerialBaud is DetectBaud with the port and settings of a session.
func DetectSerialBaud(s session.Session) (int, []BaudResult, error) {
	return DetectBaud(s.ComPort, serialMode(s), nil, 0)
}
