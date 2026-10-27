package discover

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"time"
)

// coapCore asks a device for its resource list with CoAP (the web protocol
// of small IoT devices, UDP 5683): GET /.well-known/core. It returns the
// start of the link list, "CoAP" when the device answers without one, or ""
// when nothing answers.
func coapCore(ctx context.Context, a netip.Addr, port int, timeout time.Duration) string {
	// version 1, confirmable, no token; GET; message ID; Uri-Path ".well-known", "core"
	msg := []byte{0x40, 0x01, 0x7a, 0x31, 0xbb}
	msg = append(msg, ".well-known"...)
	msg = append(msg, 0x04)
	msg = append(msg, "core"...)
	var d net.Dialer
	c, err := dialBusy(ctx, &d, "udp", netip.AddrPortFrom(a, uint16(port)).String())
	if err != nil {
		return ""
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write(msg); err != nil {
		return ""
	}
	buf := make([]byte, 1500)
	n, err := c.Read(buf)
	if err != nil || n < 4 || buf[0]>>6 != 1 {
		return ""
	}
	return coapPayload(buf[:n])
}

// coapPayload returns the printable start of a CoAP reply's payload.
func coapPayload(m []byte) string {
	i := 4 + int(m[0]&0x0f) // header and token
	for i < len(m) && m[i] != 0xff {
		delta, length := int(m[i]>>4), int(m[i]&0x0f)
		i++
		for _, v := range []int{delta, length} {
			switch v {
			case 13:
				i++
			case 14:
				i += 2
			}
		}
		if length == 13 && i-1 < len(m) {
			length = int(m[i-1]) + 13
		} else if length == 14 && i-2 >= 0 && i-1 < len(m) {
			length = int(m[i-2])<<8 | int(m[i-1]) + 269
		}
		i += length
	}
	if i+1 >= len(m) {
		return "CoAP"
	}
	p := strings.TrimSpace(printable(string(m[i+1:])))
	if len(p) > 160 {
		p = p[:160] + "…"
	}
	if p == "" {
		return "CoAP"
	}
	return p
}
