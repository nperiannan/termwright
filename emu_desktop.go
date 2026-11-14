package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

var emuHomeRe = regexp.MustCompile(`^(about:blank|https?://[A-Za-z0-9.:/_%?=&#~+-]+)$`)

// EmuSetDesktop gives a Linux computer a desktop with a web browser on its screen (or takes it away): X,
// Openbox and Firefox, started at each boot; its mouse is a USB tablet (that needs a restart). home is the
// page Firefox opens ("" = about:blank).
func (a *App) EmuSetDesktop(id string, on bool, home string) (EmuView, error) {
	defer emuAs("user")()
	home = strings.TrimSpace(home)
	if home != "" && !strings.Contains(home, "://") && home != "about:blank" {
		home = "http://" + home
	}
	if home != "" && !emuHomeRe.MatchString(home) {
		return EmuView{}, fmt.Errorf("%q is not a web address", home)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	var v *emu.VM
	if emuLab.f.Lab != nil {
		v = emuLab.f.Lab.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such computer")
	}
	if im := emuImage(v.Image); im == nil || len(im.PortAddr) == 0 {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("only a Linux computer has a desktop")
	}
	if on && v.RAM > 0 && v.RAM < 1024 {
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("a desktop with a browser needs 1 GB of memory or more (it has %s): give it more under Configure first", emuMemText(v.RAM))
	}
	v.Desktop = on
	running := emuRunning(id) && emuLab.ready[id]
	emuEv(id, "info", map[bool]string{true: "desktop with a web browser: setting it up", false: "desktop: off"}[on])
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	if running {
		script := emu.DesktopOffScript()
		if on {
			script = emu.DesktopScript(home)
		}
		out, err := a.emuComputerScript(id, script, 10*time.Minute)
		if err == nil && !strings.Contains(out, "tw-desktop-ok") {
			err = errors.New("setting up the desktop did not finish: " + strings.TrimSpace(out))
		}
		if err != nil {
			emuLab.mu.Lock()
			emuEv(id, "warn", "desktop: "+err.Error())
			emuLab.mu.Unlock()
			return EmuView{}, err
		}
		emuLab.mu.Lock()
		emuEv(id, "info", map[bool]string{true: "desktop set up: restart it for the mouse, then use Screen…", false: "desktop off"}[on])
		emuLab.mu.Unlock()
	}
	return a.EmuState()
}

// EmuScreenClick clicks on a VM's screen at x, y (its pixels): button left, right or middle; double for a
// double click. It needs the desktop (a USB tablet).
func (a *App) EmuScreenClick(kind, id string, x, y int, button string, double bool) error {
	host, mon, _, err := a.emuScreenOf(kind, id)
	if err != nil {
		return err
	}
	b := map[string]int{"left": 1, "": 1, "right": 2, "middle": 4}[button]
	if b == 0 {
		return fmt.Errorf("no button %q", button)
	}
	if x < 0 || y < 0 || x > 8192 || y > 8192 {
		return errors.New("a click outside the screen")
	}
	h, err := a.emuMonitor(host, mon)
	if err != nil {
		return fmt.Errorf("it is not running (%w)", err)
	}
	defer h.close()
	return emuClick(h, x, y, b, double)
}

// emuClick moves the USB tablet to x, y and clicks button b (1 left, 2 right, 4 middle).
func emuClick(h *emuHMP, x, y, b int, double bool) error {
	if out, err := h.do("info mice", 3*time.Second); err == nil { // the tablet takes the clicks (not the PS/2 mouse)
		if m := regexp.MustCompile(`Mouse #(\d+): QEMU HID Tablet`).FindStringSubmatch(out); m != nil {
			_, _ = h.do("mouse_set "+m[1], 3*time.Second)
		} else {
			return errors.New("it has no tablet for clicks yet: turn on its desktop and restart it")
		}
	}
	cmds := []string{fmt.Sprintf("mouse_move %d %d", x, y), fmt.Sprintf("mouse_button %d", b), "mouse_button 0"}
	if double {
		cmds = append(cmds, fmt.Sprintf("mouse_button %d", b), "mouse_button 0")
	}
	for _, c := range cmds {
		if out, err := h.do(c, 3*time.Second); err != nil || strings.Contains(out, "rror") {
			return fmt.Errorf("%s: %s", c, strings.TrimSpace(out+" "+errText(err)))
		}
	}
	return nil
}
