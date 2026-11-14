package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// lab_server: the emulated lab's servers through the connector (the palette's Server and its side pane).

var mcpServerTool = mcpTool{"lab_server", "Servers in the emulated lab: an Alpine Linux computer with HTTP, FTP and TFTP from one shared folder (/srv), DHCP on its data port 1 and SSH/SFTP for files (BusyBox and Dropbear). Its services answer on its management address too, so a switch can fetch a file over its management VR (EXOS: download url http://<server>/<file> vr VR-Mgmt). Actions: add (a new server, on a KVM host), show (its settings, services, files, DHCP leases and how to reach it), set (change settings: http, ftp, tftp, tftp_write, dhcp, from, to, gateway, dns, lease_min, boot_file, boot_from), upload (files from this computer into its shared folder), delete (files or folders of the shared folder). Changes are asked in Termwright first.",
	mcpSchema(map[string]any{
		"action":     map[string]any{"type": "string", "enum": []string{"add", "show", "set", "upload", "delete"}},
		"server":     map[string]any{"type": "string", "description": "the server (show, set, upload, delete)"},
		"host":       map[string]any{"type": "string", "description": "add: the KVM host (default the lab's)"},
		"start":      map[string]any{"type": "boolean", "description": "add: start it at once"},
		"files":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "upload: paths on this computer; delete: paths in the shared folder"},
		"dir":        map[string]any{"type": "string", "description": "upload: a folder in the shared folder (default its top)"},
		"http":       map[string]any{"type": "boolean"},
		"ftp":        map[string]any{"type": "boolean"},
		"tftp":       map[string]any{"type": "boolean"},
		"tftp_write": map[string]any{"type": "boolean", "description": "uploads by TFTP (a switch saving its configuration)"},
		"dhcp":       map[string]any{"type": "boolean"},
		"from":       map[string]any{"type": "string", "description": "DHCP: the first address it gives"},
		"to":         map[string]any{"type": "string", "description": "DHCP: the last"},
		"gateway":    map[string]any{"type": "string"},
		"dns":        map[string]any{"type": "string"},
		"lease_min":  map[string]any{"type": "integer"},
		"boot_file":  map[string]any{"type": "string", "description": "DHCP option 67: a file of the shared folder"},
		"boot_from":  map[string]any{"type": "string", "description": "DHCP: the TFTP server it names (default itself)"},
	}, "action")}

func (a *App) emuMCPServer(args map[string]any, v EmuView, ask func(string, string, bool) error) (string, error) {
	act := argStr(args, "action")
	if act == "add" {
		host := ""
		if h := argStr(args, "host"); h != "" {
			hh, err := emuMCPHost(h)
			if err != nil {
				return "", err
			}
			host = hh
		}
		start, _ := args["start"].(bool)
		if err := ask("Claude wants to add a server", "Claude (through the Termwright connector) asks to add a server (HTTP, FTP, TFTP and DHCP) to the emulated lab"+map[bool]string{true: " and start it", false: ""}[start]+".", false); err != nil {
			return "", err
		}
		before := map[string]bool{}
		for _, d := range v.Devices {
			before[d.ID] = true
		}
		nv, err := a.EmuAddServer(host, start)
		if err != nil {
			return "", err
		}
		for _, d := range nv.Devices {
			if !before[d.ID] {
				return "Added the server " + d.Hostname + map[bool]string{true: " - starting: the first start installs its packages (it needs internet once); lab_wait, then show", false: " - not started (lab_power start)"}[start], nil
			}
		}
		return "Added.", nil
	}
	d, err := emuMCPID(v, argStr(args, "server"))
	if err != nil {
		return "", err
	}
	if d.Server == nil {
		return "", fmt.Errorf("%s is not a server", d.Hostname)
	}
	switch act {
	case "show":
		sv, err := a.EmuServerState(d.ID)
		if err != nil {
			return "", err
		}
		j, _ := json.MarshalIndent(sv, "", " ")
		return string(j), nil
	case "set":
		s := *d.Server
		for k, p := range map[string]*bool{"http": &s.HTTP, "ftp": &s.FTP, "tftp": &s.TFTP, "tftp_write": &s.TFTPWrite, "dhcp": &s.DHCP} {
			if b, ok := args[k].(bool); ok {
				*p = b
			}
		}
		for k, p := range map[string]*string{"from": &s.From, "to": &s.To, "gateway": &s.Gateway, "dns": &s.DNS, "boot_file": &s.BootFile, "boot_from": &s.BootFrom} {
			if x, ok := args[k].(string); ok {
				*p = strings.TrimSpace(x)
			}
		}
		if _, ok := args["lease_min"]; ok {
			s.Lease = argInt(args, "lease_min", 60, 10080) * 60
		}
		if err := ask("Claude wants to change a server", "Claude (through the Termwright connector) asks to change the server "+d.Hostname+"'s settings.", false); err != nil {
			return "", err
		}
		if _, err := a.EmuSetServer(d.ID, s); err != nil {
			return "", err
		}
		return d.Hostname + "'s settings are kept" + map[bool]string{true: " and applied.", false: "; they apply when it starts."}[d.Running], nil
	case "upload":
		files := argStrs(args, "files")
		if len(files) == 0 {
			return "", errors.New("give the files (paths on this computer)")
		}
		if err := ask("Claude wants to copy files to a server", "Claude (through the Termwright connector) asks to copy these files of this computer into "+d.Hostname+"'s shared folder:\n\n    "+strings.Join(files, "\n    "), false); err != nil {
			return "", err
		}
		if _, err := a.EmuServerUpload(d.ID, files, argStr(args, "dir")); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d file(s) copied into %s's shared folder.", len(files), d.Hostname), nil
	case "delete":
		files := argStrs(args, "files")
		if len(files) == 0 {
			return "", errors.New("give the files (paths in the shared folder)")
		}
		if err := ask("Claude wants to delete files of a server", "Claude (through the Termwright connector) asks to delete from "+d.Hostname+"'s shared folder:\n\n    "+strings.Join(files, "\n    "), true); err != nil {
			return "", err
		}
		if _, err := a.EmuServerDelete(d.ID, files); err != nil {
			return "", err
		}
		return "Deleted.", nil
	}
	return "", errors.New("action: add, show, set, upload or delete")
}
