package session

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Bulk importers: CSV, Ansible inventories (INI and YAML), NetBox device
// lists and AWS EC2 instance lists. They return sessions; the store saves them.

// ImportSessions saves sessions (Folder/Name) with the usual conflict rules.
func (s *Store) ImportSessions(list []Session, opt ImportOptions) *ImportResult {
	res := &ImportResult{}
	s.importSessions(list, opt, res)
	return res
}

// deviceFromOS maps Ansible network_os / NetBox platform names to the
// built-in device types.
func deviceFromOS(v string) string {
	v = strings.ToLower(v)
	switch {
	case strings.Contains(v, "voss") || strings.Contains(v, "vsp") || strings.Contains(v, "fabric"):
		return "VOSS"
	case strings.Contains(v, "exos") || strings.Contains(v, "extremexos"):
		return "EXOS"
	case strings.Contains(v, "ios") || strings.Contains(v, "cisco"):
		return "Cisco IOS"
	case strings.Contains(v, "linux") || strings.Contains(v, "ubuntu") || strings.Contains(v, "rhel"):
		return "Linux"
	}
	return ""
}

func normProto(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", "ssh", "ssh2":
		return ProtoSSH2
	case "telnet":
		return ProtoTelnet
	case "serial", "console":
		return ProtoSerial
	case "raw", "tcp":
		return ProtoRaw
	case "rdp":
		return ProtoRDP
	case "vnc":
		return ProtoVNC
	case "wol", "wake-on-lan", "wakeonlan":
		return ProtoWOL
	}
	return ProtoSSH2
}

func newBulk(name, host string) Session {
	n := Defaults()
	n.Name, n.Hostname = strings.ReplaceAll(strings.TrimSpace(name), "/", "-"), strings.TrimSpace(host)
	if n.Name == "" {
		n.Name = n.Hostname
	}
	return n
}

// ParseCSV reads sessions from CSV with a header row. Recognised columns
// (any order, case-insensitive): name, host|hostname|ip|address, protocol,
// port, username|user, folder|group, device_type|type|os, description,
// identity_file|key, com_port, baud. The delimiter (, ; or tab) is detected.
func ParseCSV(data []byte) ([]Session, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	first, _, _ := bytes.Cut(data, []byte("\n"))
	r := csv.NewReader(bytes.NewReader(data))
	for _, d := range []rune{'\t', ';'} {
		if bytes.Count(first, []byte(string(d))) > bytes.Count(first, []byte(",")) {
			r.Comma = d
		}
	}
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, errors.New("csv: need a header row and at least one session")
	}
	col := map[string]int{}
	alias := map[string]string{"hostname": "host", "ip": "host", "address": "host", "ip_address": "host", "user": "username",
		"group": "folder", "type": "device_type", "devicetype": "device_type", "device type": "device_type", "os": "device_type",
		"platform": "device_type", "key": "identity_file", "identity": "identity_file", "comport": "com_port", "baudrate": "baud", "session": "name",
		"mac_address": "mac", "mac address": "mac", "env": "environment", "jump": "jump_host", "jump_session": "jump_host", "jumphost": "jump_host"}
	for i, h := range rows[0] {
		k := strings.ToLower(strings.TrimSpace(h))
		if a, ok := alias[k]; ok {
			k = a
		}
		if _, dup := col[k]; !dup {
			col[k] = i
		}
	}
	if _, ok := col["host"]; !ok {
		_, hasMAC := col["mac"]
		if _, ok := col["com_port"]; !ok && !hasMAC {
			return nil, errors.New("csv: no host column (expected one of host, hostname, ip, address)")
		}
	}
	get := func(row []string, k string) string {
		if i, ok := col[k]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	var out []Session
	for _, row := range rows[1:] {
		host, com := get(row, "host"), get(row, "com_port")
		wol := normProto(get(row, "protocol")) == ProtoWOL
		if host == "" && com == "" && !(wol && get(row, "mac") != "") {
			continue
		}
		n := newBulk(get(row, "name"), host)
		n.Protocol = normProto(get(row, "protocol"))
		if com != "" && get(row, "protocol") == "" {
			n.Protocol = ProtoSerial
		}
		n.Port = defaultPortFor(n.Protocol)
		if p, err := strconv.Atoi(get(row, "port")); err == nil && p > 0 {
			n.Port = p
		}
		n.Username = get(row, "username")
		n.Folder = cleanFolder(get(row, "folder"))
		if dt := get(row, "device_type"); dt != "" {
			if m := deviceFromOS(dt); m != "" && !strings.EqualFold(dt, m) && len(dt) > 12 {
				dt = m // long platform strings like extreme.voss.voss
			}
			n.DeviceType = dt
		}
		if d := get(row, "description"); d != "" {
			n.Description = []string{d}
		}
		n.IdentityFile = get(row, "identity_file")
		// written by Termwright's own CSV export, so a round trip keeps them
		n.MAC = strings.ToLower(get(row, "mac"))
		switch e := strings.ToLower(get(row, "environment")); e {
		case "production", "staging", "test", "development":
			n.Environment = e
		}
		n.JumpSession = get(row, "jump_host")
		n.Account = get(row, "account")
		n.FilesPanel = strings.EqualFold(get(row, "files_panel"), "yes") || get(row, "files_panel") == "1" || strings.EqualFold(get(row, "files_panel"), "true")
		if wol {
			n.WOLNetwork, n.Port = get(row, "network"), 0
			if n.Name == "" {
				n.Name = n.MAC
			}
		}
		if n.Protocol == ProtoSerial {
			n.ComPort, n.Hostname, n.Port = com, "", 0
			if n.Name == "" {
				n.Name = com
			}
			if b, err := strconv.Atoi(get(row, "baud")); err == nil {
				n.BaudRate = b
			}
		}
		out = append(out, n)
	}
	return out, nil
}

// ansibleHost applies Ansible host variables to a session.
func ansibleHost(name string, vars map[string]string) Session {
	n := newBulk(name, name)
	if h := vars["ansible_host"]; h != "" {
		n.Hostname = h
	}
	if u := vars["ansible_user"]; u != "" {
		n.Username = u
	}
	if p, err := strconv.Atoi(vars["ansible_port"]); err == nil {
		n.Port = p
	}
	if vars["ansible_connection"] == "telnet" {
		n.Protocol = ProtoTelnet
		if vars["ansible_port"] == "" {
			n.Port = 23
		}
	}
	if k := vars["ansible_ssh_private_key_file"]; k != "" {
		n.IdentityFile = k
	}
	n.DeviceType = deviceFromOS(vars["ansible_network_os"])
	return n
}

var iniVarRe = regexp.MustCompile(`([\w.]+)=("[^"]*"|'[^']*'|\S+)`)

// ParseAnsibleINI reads an INI-style Ansible inventory. Each group becomes a
// folder; a host in several groups is imported once (first group).
func ParseAnsibleINI(text string) []Session {
	type host struct {
		group string
		vars  map[string]string
	}
	hosts := map[string]*host{}
	var order []string
	groupVars := map[string]map[string]string{}
	group, section := "ungrouped", "hosts"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			g := line[1 : len(line)-1]
			group, section = g, "hosts"
			if i := strings.Index(g, ":"); i >= 0 {
				group, section = g[:i], g[i+1:]
			}
			continue
		}
		switch section {
		case "vars":
			if k, v, ok := strings.Cut(line, "="); ok {
				if groupVars[group] == nil {
					groupVars[group] = map[string]string{}
				}
				groupVars[group][strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
			}
		case "hosts":
			f := strings.Fields(line)
			name := f[0]
			if strings.ContainsAny(name, "[]") { // host ranges like sw[01:10] are not expanded
				continue
			}
			h := hosts[name]
			if h == nil {
				h = &host{group: group, vars: map[string]string{}}
				hosts[name] = h
				order = append(order, name)
			}
			for _, m := range iniVarRe.FindAllStringSubmatch(line[len(name):], -1) {
				h.vars[m[1]] = strings.Trim(m[2], `"'`)
			}
		}
	}
	var out []Session
	for _, name := range order {
		h := hosts[name]
		vars := map[string]string{}
		for _, g := range []string{"all", h.group} {
			for k, v := range groupVars[g] {
				vars[k] = v
			}
		}
		for k, v := range h.vars {
			vars[k] = v
		}
		n := ansibleHost(name, vars)
		if h.group != "ungrouped" {
			n.Folder = cleanFolder(h.group)
		}
		out = append(out, n)
	}
	return out
}

// ParseAnsibleYAML reads a YAML Ansible inventory (all: / children: / hosts:).
// Nested groups become nested folders.
func ParseAnsibleYAML(data []byte) ([]Session, error) {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("ansible yaml: %w", err)
	}
	seen := map[string]bool{}
	var out []Session
	strVars := func(m any) map[string]string {
		res := map[string]string{}
		if mm, ok := m.(map[string]any); ok {
			for k, v := range mm {
				if v != nil {
					res[k] = fmt.Sprint(v)
				}
			}
		}
		return res
	}
	var walk func(name string, node any, folder string, inherited map[string]string)
	walk = func(name string, node any, folder string, inherited map[string]string) {
		g, _ := node.(map[string]any)
		vars := map[string]string{}
		for k, v := range inherited {
			vars[k] = v
		}
		for k, v := range strVars(g["vars"]) {
			vars[k] = v
		}
		sub := folder
		if name != "all" && name != "ungrouped" {
			sub = strings.Trim(folder+"/"+name, "/")
		}
		if hs, ok := g["hosts"].(map[string]any); ok {
			names := make([]string, 0, len(hs))
			for h := range hs {
				names = append(names, h)
			}
			sort.Strings(names)
			for _, h := range names {
				if seen[h] {
					continue
				}
				seen[h] = true
				hv := map[string]string{}
				for k, v := range vars {
					hv[k] = v
				}
				for k, v := range strVars(hs[h]) {
					hv[k] = v
				}
				n := ansibleHost(h, hv)
				n.Folder = cleanFolder(sub)
				out = append(out, n)
			}
		}
		if ch, ok := g["children"].(map[string]any); ok {
			names := make([]string, 0, len(ch))
			for c := range ch {
				names = append(names, c)
			}
			sort.Strings(names)
			for _, c := range names {
				walk(c, ch[c], sub, vars)
			}
		}
	}
	names := make([]string, 0, len(root))
	for k := range root {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		walk(k, root[k], "", map[string]string{})
	}
	if len(out) == 0 {
		return nil, errors.New("ansible yaml: no hosts found")
	}
	return out, nil
}

// NetBoxDevice is the part of a NetBox /api/dcim/devices/ result we use.
type NetBoxDevice struct {
	Name      string `json:"name"`
	PrimaryIP *struct {
		Address string `json:"address"`
	} `json:"primary_ip"`
	Site *struct {
		Name string `json:"name"`
	} `json:"site"`
	Role *struct {
		Name string `json:"name"`
	} `json:"role"`
	DeviceRole *struct {
		Name string `json:"name"`
	} `json:"device_role"` // NetBox < 4.0
	Platform *struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"platform"`
	Description string `json:"description"`
}

// NetBoxSessions converts NetBox devices: folder = site/role, device type from
// the platform, host = primary IP (without prefix length).
func NetBoxSessions(devs []NetBoxDevice) []Session {
	var out []Session
	for _, d := range devs {
		if d.PrimaryIP == nil || d.PrimaryIP.Address == "" {
			continue
		}
		ip, _, _ := strings.Cut(d.PrimaryIP.Address, "/")
		n := newBulk(d.Name, ip)
		var parts []string
		if d.Site != nil {
			parts = append(parts, d.Site.Name)
		}
		if d.Role != nil {
			parts = append(parts, d.Role.Name)
		} else if d.DeviceRole != nil {
			parts = append(parts, d.DeviceRole.Name)
		}
		n.Folder = cleanFolder(strings.Join(parts, "/"))
		if d.Platform != nil {
			n.DeviceType = deviceFromOS(d.Platform.Slug + " " + d.Platform.Name)
		}
		if d.Description != "" {
			n.Description = []string{d.Description}
		}
		out = append(out, n)
	}
	return out
}

// AWSSessions converts `aws ec2 describe-instances --output json` output.
// Running instances only; folder = AWS/<region or VPC>; the key pair name
// becomes ~/.ssh/<KeyName>.pem; private IP is used unless usePublic.
func AWSSessions(data []byte, folder string, usePublic bool) ([]Session, error) {
	var resp struct {
		Reservations []struct {
			Instances []struct {
				InstanceID       string                        `json:"InstanceId"`
				PrivateIPAddress string                        `json:"PrivateIpAddress"`
				PublicIPAddress  string                        `json:"PublicIpAddress"`
				KeyName          string                        `json:"KeyName"`
				VpcID            string                        `json:"VpcId"`
				Platform         string                        `json:"Platform"`
				State            struct{ Name string }         `json:"State"`
				Tags             []struct{ Key, Value string } `json:"Tags"`
			} `json:"Instances"`
		} `json:"Reservations"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("aws: %w", err)
	}
	var out []Session
	for _, r := range resp.Reservations {
		for _, in := range r.Instances {
			if in.State.Name != "running" || in.Platform == "windows" {
				continue
			}
			name := in.InstanceID
			for _, t := range in.Tags {
				if t.Key == "Name" && t.Value != "" {
					name = t.Value + " (" + in.InstanceID + ")"
				}
			}
			ip := in.PrivateIPAddress
			if usePublic && in.PublicIPAddress != "" {
				ip = in.PublicIPAddress
			}
			if net.ParseIP(ip) == nil {
				continue
			}
			n := newBulk(name, ip)
			n.Username = "ec2-user"
			n.DeviceType = "Linux"
			if in.KeyName != "" {
				n.IdentityFile = "~/.ssh/" + in.KeyName + ".pem"
			}
			n.Folder = cleanFolder("AWS/" + folder + "/" + in.VpcID)
			out = append(out, n)
		}
	}
	return out, nil
}

// sniffBulk recognises CSV and Ansible inventories by name and content.
func sniffBulk(src, text string) string {
	low := strings.ToLower(src)
	switch {
	case strings.HasSuffix(low, ".csv") || strings.HasSuffix(low, ".tsv"):
		return "csv"
	case (strings.HasSuffix(low, ".yml") || strings.HasSuffix(low, ".yaml")) && strings.Contains(text, "hosts:"):
		return "ansible-yaml"
	case regexp.MustCompile(`(?m)^\s*\S+\s+.*ansible_host=`).MatchString(text) ||
		regexp.MustCompile(`(?m)^\[[\w.-]+(:vars|:children)?\]\s*$`).MatchString(text) && !strings.Contains(text, "[Bookmarks") && !strings.Contains(text, `S:"`):
		return "ansible-ini"
	}
	return ""
}
