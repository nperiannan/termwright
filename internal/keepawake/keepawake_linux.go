package keepawake

import (
	"errors"
	"os"

	"github.com/godbus/dbus/v5"
)

// Linux: the desktop's own inhibit (GNOME, then KDE/Xfce and others through the freedesktop
// PowerManagement interface) stops its automatic suspend, and systemd-logind's "idle" inhibit
// stops logind's idle action. Each is released when Termwright exits, even on a crash.
const why = "Termwright is open"

var (
	sess        *dbus.Conn
	gnomeCookie uint32
	pmCookie    uint32
	idleFD      *os.File
)

func start() error {
	n := 0
	if c, err := dbus.ConnectSessionBus(); err == nil {
		if c.Object("org.gnome.SessionManager", "/org/gnome/SessionManager").
			Call("org.gnome.SessionManager.Inhibit", 0, "Termwright", uint32(0), why, uint32(8)).Store(&gnomeCookie) == nil {
			n++
		} else if c.Object("org.freedesktop.PowerManagement", "/org/freedesktop/PowerManagement/Inhibit").
			Call("org.freedesktop.PowerManagement.Inhibit.Inhibit", 0, "Termwright", why).Store(&pmCookie) == nil {
			n++
		}
		if n > 0 {
			sess = c
		} else {
			c.Close()
		}
	}
	if c, err := dbus.ConnectSystemBus(); err == nil {
		var fd dbus.UnixFD
		if c.Object("org.freedesktop.login1", "/org/freedesktop/login1").
			Call("org.freedesktop.login1.Manager.Inhibit", 0, "idle", "Termwright", why, "block").Store(&fd) == nil {
			idleFD = os.NewFile(uintptr(fd), "termwright-idle-inhibit")
			n++
		}
		c.Close()
	}
	if n == 0 {
		return errors.New("this desktop offers no way to stop sleep (no session manager or logind on D-Bus)")
	}
	return nil
}

func stop() {
	if sess != nil {
		if gnomeCookie != 0 {
			sess.Object("org.gnome.SessionManager", "/org/gnome/SessionManager").Call("org.gnome.SessionManager.Uninhibit", 0, gnomeCookie)
		}
		if pmCookie != 0 {
			sess.Object("org.freedesktop.PowerManagement", "/org/freedesktop/PowerManagement/Inhibit").Call("org.freedesktop.PowerManagement.Inhibit.UnInhibit", 0, pmCookie)
		}
		sess.Close()
	}
	sess, gnomeCookie, pmCookie = nil, 0, 0
	if idleFD != nil {
		idleFD.Close() // logind lifts the inhibit when its file is closed
		idleFD = nil
	}
}
