package discover

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// WebInfo is what a device's web interface tells about it.
type WebInfo struct {
	Server string   `json:"server"` // Server header
	Title  string   `json:"title"`  // <title> of the start page
	Realm  string   `json:"realm"`  // WWW-Authenticate realm
	Cert   string   `json:"cert"`   // certificate subject (organisation / name)
	Hints  []string `json:"hints"`  // product names found in the page
}

var (
	reTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reRealm = regexp.MustCompile(`(?i)realm="([^"]*)"`)
	// product and vendor names worth noting when they appear in a start page
	reHints = regexp.MustCompile(`(?i)\b(ExtremeXOS|Switch Engine|Fabric Engine|VOSS|Extreme Networks|ExtremeCloud|ExtremeWireless|Enterasys|Avaya|` +
		`Cisco|Meraki|Juniper|Arista|Aruba|HPE|ProCurve|Ruckus|Ubiquiti|UniFi|MikroTik|RouterOS|Fortinet|FortiGate|pfSense|OPNsense|SonicWall|Palo Alto|` +
		`Yealink|Polycom|Grandstream|Snom|Mitel|Avaya IP Phone|Cisco IP Phone|Fanvil|Gigaset|` +
		`LaserJet|OfficeJet|Brother|Canon|Epson|Ricoh|Xerox|Kyocera|Lexmark|Konica|bizhub|` +
		`TerraMaster|Synology|DiskStation|QNAP|TrueNAS|FreeNAS|ReadyNAS|ASUSTOR|My Cloud|` +
		`iDRAC|iLO|IPMI|Supermicro|Proxmox|VMware ESXi|vSphere|Hikvision|Dahua|OpenWrt|FRITZ!Box|TP-Link|Netgear|Linksys|ASUS)\b`)
)

// webInfo reads the start page of a web interface (following up to three
// redirects on the same address) without logging in.
func webInfo(ctx context.Context, a netip.Addr, port int, useTLS bool, timeout time.Duration) WebInfo {
	var w WebInfo
	host := net.JoinHostPort(a.String(), strconv.Itoa(port))
	tr := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: timeout}).DialContext,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // only reading who the device says it is
		TLSHandshakeTimeout: timeout,
		DisableKeepAlives:   true,
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, Timeout: timeout + time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || req.URL.Hostname() != a.String() {
				return http.ErrUseLastResponse
			}
			return nil
		}}
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+host+"/", nil)
	if err != nil {
		return w
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Termwright device discovery)")
	resp, err := cl.Do(req)
	if err != nil {
		return w
	}
	defer resp.Body.Close()
	w.Server = printable(resp.Header.Get("Server"))
	if m := reRealm.FindStringSubmatch(resp.Header.Get("WWW-Authenticate")); m != nil {
		w.Realm = printable(m[1])
	}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		s := resp.TLS.PeerCertificates[0].Subject
		var parts []string
		if len(s.Organization) > 0 {
			parts = append(parts, s.Organization[0])
		}
		if len(s.OrganizationalUnit) > 0 {
			parts = append(parts, s.OrganizationalUnit[0])
		}
		if s.CommonName != "" && s.CommonName != a.String() {
			parts = append(parts, s.CommonName)
		}
		w.Cert = printable(strings.Join(parts, ", "))
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 96<<10))
	if m := reTitle.FindSubmatch(body); m != nil {
		w.Title = strings.Join(strings.Fields(printable(htmlText(string(m[1])))), " ")
		if len(w.Title) > 100 {
			w.Title = w.Title[:100]
		}
	}
	seen := map[string]bool{}
	for _, m := range reHints.FindAllString(string(body)+" "+w.Server+" "+w.Realm+" "+w.Cert, 40) {
		k := strings.ToLower(m)
		if !seen[k] && len(w.Hints) < 5 {
			seen[k] = true
			w.Hints = append(w.Hints, m)
		}
	}
	return w
}

var htmlEntities = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&nbsp;", " ")

func htmlText(s string) string { return htmlEntities.Replace(s) }
