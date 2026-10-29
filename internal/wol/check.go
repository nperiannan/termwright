package wol

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Ping sends one ICMP echo with the system's ping command (no special
// rights needed) and reports whether the host answered within timeout.
func Ping(ctx context.Context, host string, timeout time.Duration) bool {
	if host == "" || strings.ContainsAny(host, " ;&|$`'\"\\") || strings.HasPrefix(host, "-") {
		return false
	}
	secs := int(timeout.Seconds())
	if secs < 1 {
		secs = 1
	}
	var args []string
	switch runtime.GOOS {
	case "windows":
		args = []string{"-n", "1", "-w", itoa(secs * 1000), host}
	case "darwin":
		args = []string{"-c", "1", "-t", itoa(secs), host}
	default:
		args = []string{"-c", "1", "-W", itoa(secs), host}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ping", args...)
	hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	// Windows ping succeeds for "Destination host unreachable" too: an
	// answer has a TTL.
	return runtime.GOOS != "windows" || strings.Contains(strings.ToUpper(string(out)), "TTL=")
}

func itoa(n int) string {
	var b [12]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	return string(b[i:])
}

// NetBIOSName asks a host for its NetBIOS name (an NBSTAT query to UDP port
// 137, answered by Windows and Samba): the computer name, "" when it does
// not answer.
func NetBIOSName(ip string, timeout time.Duration) string {
	c, err := net.DialTimeout("udp4", net.JoinHostPort(ip, "137"), timeout)
	if err != nil {
		return ""
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	q := []byte{0x7a, 0x31, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0x20}
	q = append(q, []byte("CKAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")...) // "*" padded, first-level encoded
	q = append(q, 0, 0, 0x21, 0, 1)                              // NBSTAT, IN
	if _, err := c.Write(q); err != nil {
		return ""
	}
	b := make([]byte, 1500)
	n, err := c.Read(b)
	if err != nil {
		return ""
	}
	return parseNBSTAT(b[:n])
}

// parseNBSTAT returns the first unique workstation name (suffix 0x00) of an
// NBSTAT answer.
func parseNBSTAT(b []byte) string {
	if len(b) < 57 || binary.BigEndian.Uint16(b[6:8]) == 0 {
		return ""
	}
	// header (12) + name (34) + type/class (4) + TTL (4) + rdlength (2) = 56
	i := 56
	count := int(b[i])
	i++
	for k := 0; k < count && i+18 <= len(b); k++ {
		name := strings.TrimRight(string(bytes.TrimRight(b[i:i+15], "\x00")), " ")
		suffix := b[i+15]
		flags := binary.BigEndian.Uint16(b[i+16 : i+18])
		i += 18
		if suffix == 0 && flags&0x8000 == 0 && name != "" {
			return name
		}
	}
	return ""
}
