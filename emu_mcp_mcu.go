package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// lab_microcontrollers: the emulated lab's ESP32 boards through the connector (the GUI's Add Firmware, Get
// MicroPython, Hosts → This computer and the palette's boards).

var mcpMCUTool = mcpTool{"lab_microcontrollers", "Microcontrollers (ESP32-S3, -C3, -C6, -H2) in the emulated lab, run by Espressif's QEMU on a KVM host or on this computer (the one Termwright runs on; macOS or Linux, microcontrollers only). Actions: list (boards, firmwares, this computer as a host, the microcontrollers in the lab), this_computer (add this computer as a host), sample (get MicroPython's newest release from micropython.org as a firmware: the ESP32-C3 only - the S3 and C6 builds stop in Espressif's QEMU, the H2 has none), add (put microcontrollers of a board in the lab: its newest firmware, or MicroPython downloaded first; on this computer when it is a host), flash (give one another firmware of its board: its flash is made again). Type on their console (the UART: MicroPython's >>> prompt) with lab_console; start and stop them with lab_power; cable their port 1 (Ethernet) with lab_cable. Changes are asked in Termwright first.",
	mcpSchema(map[string]any{
		"action":   map[string]any{"type": "string", "enum": []string{"list", "this_computer", "sample", "add", "flash"}},
		"board":    map[string]any{"type": "string", "description": "sample, add: esp32s3, esp32c3, esp32c6 or esp32h2 (or ESP32-S3 …)"},
		"firmware": map[string]any{"type": "string", "description": "add: a firmware's image id (default the board's newest, else MicroPython); flash: the firmware to give it"},
		"host":     map[string]any{"type": "string", "description": "add: a KVM host name, or \"this computer\" (default this computer when it is a host, else the lab's host)"},
		"count":    map[string]any{"type": "integer", "description": "add: how many (default 1, at most 16)"},
		"names":    map[string]any{"type": "string", "description": "add: their names, comma separated, or a pattern like esp-{n}"},
		"start":    map[string]any{"type": "boolean", "description": "add: start them at once"},
		"switch":   map[string]any{"type": "string", "description": "flash: the microcontroller"},
	}, "action")}

// emuMCPBoard finds a board by its key or name ("ESP32-S3", "s3").
func emuMCPBoard(s string) *emu.Board {
	k := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(s))
	if b := emu.BoardOf(k); b != nil {
		return b
	}
	return emu.BoardOf("esp32" + strings.TrimPrefix(k, "esp32"))
}

func (a *App) emuMCPMCU(args map[string]any, v EmuView, ask func(string, string, bool) error) (string, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	imgs := append([]emu.Image{}, emuLab.f.Images...)
	local := false
	var localInfo emu.HostInfo
	for _, h := range emuLab.f.Hosts {
		if h.Session == emu.LocalHost {
			local, localInfo = true, h.Info
		}
	}
	emuLab.mu.Unlock()
	fwOf := func(key string) []emu.Image {
		var out []emu.Image
		for _, im := range imgs {
			if im.Board == key {
				out = append(out, im)
			}
		}
		return out
	}
	switch argStr(args, "action") {
	case "list":
		var b strings.Builder
		b.WriteString("Boards:\n")
		for _, bd := range emu.Boards {
			fmt.Fprintf(&b, "- %s (%s): %s, flash %v MB", bd.Name, bd.Key, bd.CPU, bd.Flash)
			if bd.Sample != "" {
				b.WriteString(", MicroPython " + bd.Sample)
			}
			b.WriteString("\n")
			for _, im := range fwOf(bd.Key) {
				fmt.Fprintf(&b, "  firmware %s: %s, %d MB (%s)\n", im.ID, im.Name, im.FlashMB, strings.TrimPrefix(im.Source, "firmware "))
			}
		}
		if local {
			fmt.Fprintf(&b, "This computer is a host: %s, %s, %d CPUs", orStr(localInfo.OS, "?"), localInfo.Arch, localInfo.CPUs)
			if localInfo.QEMU != "" {
				b.WriteString(", " + localInfo.QEMU)
			} else {
				b.WriteString(", Espressif's QEMU downloaded at the first start")
			}
			if len(localInfo.Problems) > 0 {
				b.WriteString("; problems: " + strings.Join(localInfo.Problems, " "))
			}
			b.WriteString("\n")
		} else {
			b.WriteString("This computer is not a host (action this_computer adds it).\n")
		}
		n := 0
		for _, d := range v.Devices {
			if d.Board == "" {
				continue
			}
			if n == 0 {
				b.WriteString("Microcontrollers:\n")
			}
			n++
			host := emuHostName(d.Host)
			fmt.Fprintf(&b, "- %s | %s | %s | %s | console telnet %s:%d", d.Hostname, strings.ToUpper(d.Board), emuImageName(imgs, d.Image), orStr(d.StateText, d.State), host, d.Console)
			if d.GDB > 0 {
				fmt.Fprintf(&b, " | GDB :%d", d.GDB)
			}
			if d.Error != "" {
				b.WriteString(" | " + d.Error)
			}
			b.WriteString("\n")
		}
		if n == 0 {
			b.WriteString("No microcontrollers in the lab yet (action add).\n")
		}
		return b.String(), nil
	case "this_computer":
		if local && len(localInfo.Problems) == 0 {
			return "This computer is a host already.", nil
		}
		if err := ask("Claude wants to add this computer as a host", "Claude (through the Termwright connector) asks to add this computer as a host of the emulated lab, for microcontrollers (Espressif's QEMU, in the termwright-lab folder of your home folder; no port is opened).", false); err != nil {
			return "", err
		}
		h, err := a.EmuCheckHost(emu.LocalHost)
		if err != nil {
			return "", err
		}
		if len(h.Info.Problems) > 0 {
			return "This computer was checked, but: " + strings.Join(h.Info.Problems, " "), nil
		}
		return fmt.Sprintf("This computer is a host now: %s, %s, %d CPUs, %d MB of memory free.", h.Info.OS, h.Info.Arch, h.Info.CPUs, h.Info.MemFree), nil
	case "sample":
		bd := emuMCPBoard(argStr(args, "board"))
		if bd == nil {
			return "", errors.New("give a board: esp32s3, esp32c3, esp32c6 or esp32h2")
		}
		if err := ask("Claude wants to get MicroPython", "Claude (through the Termwright connector) asks to download MicroPython's newest release for the "+bd.Name+" from micropython.org and add it as a firmware.", false); err != nil {
			return "", err
		}
		nv, err := a.EmuSampleFirmware(bd.Key)
		if err != nil {
			return "", err
		}
		for _, im := range nv.Images {
			if im.Board == bd.Key && strings.HasPrefix(im.Source, "firmware "+bd.Sample+"-") {
				return fmt.Sprintf("Firmware %s: %s for the %s, %d MB flash.", im.ID, im.Name, bd.Name, im.FlashMB), nil
			}
		}
		return "MicroPython was added.", nil
	case "add":
		bd := emuMCPBoard(argStr(args, "board"))
		fw := argStr(args, "firmware")
		if fw != "" {
			for _, im := range imgs {
				if im.ID == fw && im.Board != "" {
					bd = emu.BoardOf(im.Board)
				}
			}
			if bd == nil {
				return "", fmt.Errorf("%s is not a microcontroller firmware (list)", fw)
			}
		}
		if bd == nil {
			return "", errors.New("give a board (esp32s3, esp32c3, esp32c6, esp32h2) or a firmware id")
		}
		if fw == "" {
			if l := fwOf(bd.Key); len(l) > 0 {
				fw = l[len(l)-1].ID
			} else if bd.Sample == "" {
				return "", fmt.Errorf("the %s has no firmware yet and there is no MicroPython sample for it (only the ESP32-C3's runs in Espressif's QEMU): add one in Termwright (Appliances → Add Firmware)", bd.Name)
			}
		}
		host := argStr(args, "host")
		switch {
		case strings.EqualFold(host, "this computer") || host == emu.LocalHost || host == "" && local:
			host = emu.LocalHost
		default:
			h, err := emuMCPHost(host)
			if err != nil {
				return "", err
			}
			host = h
		}
		count := argInt(args, "count", 1, 16)
		start, _ := args["start"].(bool)
		what := fmt.Sprintf("%d %s", count, bd.Name)
		if fw == "" {
			what += " with MicroPython (downloaded from micropython.org first)"
		} else {
			what += " with the firmware " + emuImageName(imgs, fw)
		}
		if err := ask("Claude wants to add microcontrollers", "Claude (through the Termwright connector) asks to add "+what+" on "+emuHostName(host)+map[bool]string{true: " and start them", false: ""}[start]+".", false); err != nil {
			return "", err
		}
		if host == emu.LocalHost && !local {
			if _, err := a.EmuCheckHost(emu.LocalHost); err != nil {
				return "", err
			}
		}
		if fw == "" {
			nv, err := a.EmuSampleFirmware(bd.Key)
			if err != nil {
				return "", err
			}
			for _, im := range nv.Images {
				if im.Board == bd.Key {
					fw = im.ID
				}
			}
		}
		before := map[string]bool{}
		for _, d := range v.Devices {
			before[d.ID] = true
		}
		names := orStr(argStr(args, "names"), strings.ReplaceAll(strings.ToLower(bd.Name), "-", "")+"-{n}")
		nv, err := a.EmuAdd(EmuRequest{Host: host, Groups: []EmuGroup{{Image: fw, Count: count, Names: names}}, Cabling: "none", Start: start})
		if err != nil {
			return "", err
		}
		var made []string
		for _, d := range nv.Devices {
			if !before[d.ID] {
				made = append(made, d.Hostname)
			}
		}
		return "Added on " + emuHostName(host) + ": " + strings.Join(made, ", ") + map[bool]string{true: " - starting (lab_wait, then lab_console)", false: " - not started (lab_power start)"}[start], nil
	case "flash":
		d, err := emuMCPID(v, argStr(args, "switch"))
		if err != nil {
			return "", err
		}
		fw := argStr(args, "firmware")
		if fw == "" {
			return "", errors.New("give the firmware id (list)")
		}
		if err := ask("Claude wants to flash a microcontroller", "Claude (through the Termwright connector) asks to flash "+d.Hostname+" with "+emuImageName(imgs, fw)+": its flash is made again (what the old firmware wrote goes) and it starts again.", true); err != nil {
			return "", err
		}
		if _, err := a.EmuFlashFirmware(d.ID, fw); err != nil {
			return "", err
		}
		return d.Hostname + " is flashed with " + emuImageName(imgs, fw) + " and starting.", nil
	}
	return "", errors.New("action: list, this_computer, sample, add or flash")
}

func emuImageName(imgs []emu.Image, id string) string {
	for _, im := range imgs {
		if im.ID == id {
			return im.Name
		}
	}
	return id
}

// emuHostName is a host's name for people ("This computer", or its session's name).
func emuHostName(host string) string {
	if host == emu.LocalHost {
		return "this computer"
	}
	return host[strings.LastIndex(host, "/")+1:]
}

// reMCUPrompt is MicroPython's prompt (and its continuation) at the end of what the UART printed.
var reMCUPrompt = regexp.MustCompile(`(>>> |\.\.\. )$`)

// emuMCUConsoleRun types lines on a microcontroller's UART (through Termwright's console keeper): no login. Ctrl-C
// first stops a running program; a line's output ends at MicroPython's prompt, or after 2 s without output (a
// firmware without a REPL).
func (a *App) emuMCUConsoleRun(id string, cmds []string, timeout time.Duration) (string, error) {
	k, err := a.emuMCUKeep(id)
	if err != nil {
		return "", fmt.Errorf("its console: %w (is it running?)", err)
	}
	_, ch, stop := k.watch()
	defer stop()
	var (
		mu   sync.Mutex
		text strings.Builder
		last time.Time
	)
	go func() {
		for b := range ch {
			mu.Lock()
			text.Write(b)
			last = time.Now()
			mu.Unlock()
		}
	}()
	get := func() (string, time.Time) {
		mu.Lock()
		defer mu.Unlock()
		return text.String(), last
	}
	deadline := time.Now().Add(timeout)
	settle := func(max time.Duration) string { // until the prompt, or quiet
		cur, _ := get()
		from := len(cur)
		end := time.Now().Add(max)
		if end.After(deadline) {
			end = deadline
		}
		for time.Now().Before(end) {
			time.Sleep(50 * time.Millisecond)
			all, at := get()
			t := all[from:]
			if reMCUPrompt.MatchString(t) || len(t) > 0 && time.Since(at) > 2*time.Second {
				break
			}
		}
		all, _ := get()
		return all[from:]
	}
	emuLab.mu.Lock()
	host, name := "", id
	if v := emuLab.f.Lab.VMByID(id); v != nil {
		host, name = emuHostOf(*v), v.Name
	}
	emuLab.mu.Unlock()
	_, _ = k.Write([]byte{3, '\r'})
	settle(5 * time.Second)
	var b strings.Builder
	for _, cmd := range cmds {
		emuLogf(emuLvInfo, host, name, "console (AI connector) types: %s", cmd)
		if _, err := k.Write([]byte(cmd + "\r")); err != nil {
			return b.String(), err
		}
		out := emu.CleanConsole(settle(60 * time.Second))
		out = strings.TrimPrefix(strings.TrimLeft(out, "\r\n"), cmd) // its echo
		out = strings.TrimSuffix(strings.TrimRight(reMCUPrompt.ReplaceAllString(out, ""), "\r\n "), ">>>") // its prompt
		out = strings.TrimRight(out, "\r\n ")
		fmt.Fprintf(&b, "%s\n%s\n", cmd, strings.Trim(out, "\r\n"))
		if time.Now().After(deadline) {
			return b.String(), errors.New("timed out: no answer on its console")
		}
	}
	return b.String(), nil
}
