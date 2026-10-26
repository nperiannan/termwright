package discover

import (
	"bufio"
	"context"
	"encoding/csv"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// neighbours returns the IPv4 → MAC table of this computer (ARP cache).
func neighbours() map[netip.Addr]string {
	out := map[netip.Addr]string{}
	if runtime.GOOS == "linux" {
		if f, err := os.Open("/proc/net/arp"); err == nil {
			defer f.Close()
			parseProcARP(f, out)
			return out
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := []string{"-an"}
	if runtime.GOOS == "windows" {
		args = []string{"-a"}
	}
	cmd := exec.CommandContext(ctx, "arp", args...)
	hideWindow(cmd)
	b, err := cmd.Output()
	if err != nil {
		return out
	}
	parseARP(string(b), out)
	return out
}

var (
	reARPBSD = regexp.MustCompile(`\((\d+\.\d+\.\d+\.\d+)\) at ([0-9a-fA-F:]+)`)                         // macOS / BSD: ? (10.0.0.1) at aa:bb:cc:dd:ee:ff on en0
	reARPWin = regexp.MustCompile(`^\s*(\d+\.\d+\.\d+\.\d+)\s+([0-9a-fA-F]{2}(?:-[0-9a-fA-F]{2}){5})\s`) // Windows: 10.0.0.1  aa-bb-cc-dd-ee-ff  dynamic
)

func parseARP(s string, out map[netip.Addr]string) {
	for _, l := range strings.Split(s, "\n") {
		var ip, mac string
		if m := reARPBSD.FindStringSubmatch(l); m != nil {
			ip, mac = m[1], m[2]
		} else if m := reARPWin.FindStringSubmatch(l); m != nil {
			ip, mac = m[1], m[2]
		} else {
			continue
		}
		if a, err := netip.ParseAddr(ip); err == nil {
			if norm := normMAC(mac); norm != "" {
				out[a] = norm
			}
		}
	}
}

func parseProcARP(r io.Reader, out map[netip.Addr]string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		// IP address  HW type  Flags  HW address  Mask  Device; flags 0x0 = incomplete
		if len(f) < 4 || f[2] == "0x0" {
			continue
		}
		if a, err := netip.ParseAddr(f[0]); err == nil {
			if norm := normMAC(f[3]); norm != "" {
				out[a] = norm
			}
		}
	}
}

// normMAC returns aa:bb:cc:dd:ee:ff, or "" for incomplete, broadcast or zero addresses.
func normMAC(s string) string {
	s = strings.ReplaceAll(s, "-", ":")
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return ""
	}
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p // macOS prints 0:1b:… without leading zeros
		}
	}
	hw, err := net.ParseMAC(strings.Join(parts, ":"))
	if err != nil {
		return ""
	}
	m := hw.String()
	if m == "00:00:00:00:00:00" || m == "ff:ff:ff:ff:ff:ff" {
		return ""
	}
	return m
}

// OUI maps the first three bytes of a MAC address ("AABBCC") to the vendor.
type OUI map[string]string

// Vendor returns the vendor of a MAC address; randomised (private) addresses are named as such.
func (o OUI) Vendor(mac string) string {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) < 3 {
		return ""
	}
	if hw[0]&0x02 != 0 {
		return "private (randomised) address"
	}
	return o[strings.ToUpper(strings.ReplaceAll(mac[:8], ":", ""))]
}

// LoadOUI reads the IEEE MA-L registry (oui.csv: Registry,Assignment,Organization Name,Organization Address).
func LoadOUI(r io.Reader) (OUI, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	out := OUI{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
		if len(rec) >= 3 && len(rec[1]) == 6 {
			out[strings.ToUpper(rec[1])] = strings.TrimSpace(rec[2])
		}
	}
	return out, nil
}
