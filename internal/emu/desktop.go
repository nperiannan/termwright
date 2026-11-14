package emu

// DesktopScript sets up a desktop with a web browser on a Linux computer (Alpine; Debian or Ubuntu): X, the
// Openbox window manager and Firefox, started on its screen at every boot, Firefox maximized. Its screen and
// keyboard are used through Termwright's Screen (the mouse is a USB tablet). It needs internet and about 1 GB
// of memory.
func DesktopScript(home string) string {
	if home == "" {
		home = "about:blank"
	}
	return `set -e
if command -v apk >/dev/null; then
  setup-xorg-base >/dev/null 2>&1 || apk add -q xorg-server eudev
  apk add -q xinit openbox firefox-esr font-dejavu dbus xf86-input-libinput xf86-input-evdev >/dev/null
  rc-update add dbus default >/dev/null 2>&1 || true
  FF=firefox-esr
elif command -v apt-get >/dev/null; then
  DEBIAN_FRONTEND=noninteractive apt-get install -y -q xserver-xorg xinit openbox firefox-esr fonts-dejavu dbus-x11 >/dev/null 2>&1 || DEBIAN_FRONTEND=noninteractive apt-get install -y -q xserver-xorg xinit openbox firefox fonts-dejavu dbus-x11 >/dev/null
  FF=$(command -v firefox-esr || command -v firefox)
else
  echo 'tw-desktop: no apk or apt-get'; exit 1
fi
mkdir -p /root/.config/openbox
cat > /root/.config/openbox/autostart <<TWEOF
xsetroot -solid '#1d2128' 2>/dev/null &
while true; do $FF --new-instance ` + "'" + home + "'" + `; sleep 2; done &
TWEOF
cat > /root/.config/openbox/rc.xml <<'TWEOF'
<?xml version="1.0" encoding="UTF-8"?>
<openbox_config xmlns="http://openbox.org/3.4/rc"><applications><application class="*"><decor>no</decor><maximized>yes</maximized></application></applications></openbox_config>
TWEOF
printf 'exec openbox-session\n' > /root/.xinitrc
mkdir -p /etc/local.d
printf '#!/bin/sh\n(sleep 3; HOME=/root startx -- :0 vt7 >/var/log/tw-desktop.log 2>&1 &)\n' > /etc/local.d/tw-desktop.start
chmod +x /etc/local.d/tw-desktop.start
(rc-update add local default >/dev/null 2>&1 || true)
if command -v systemctl >/dev/null && [ ! -d /etc/runlevels ]; then
  printf '[Unit]\nDescription=Termwright desktop\nAfter=network.target\n[Service]\nEnvironment=HOME=/root\nExecStart=/usr/bin/startx -- :0 vt7\nRestart=always\n[Install]\nWantedBy=multi-user.target\n' > /etc/systemd/system/tw-desktop.service
  systemctl enable tw-desktop >/dev/null 2>&1 || true
fi
echo tw-desktop-ok
`
}

// DesktopOffScript stops the desktop starting at boot (the packages stay).
func DesktopOffScript() string {
	return "rm -f /etc/local.d/tw-desktop.start; (systemctl disable tw-desktop >/dev/null 2>&1 || true); pkill -f 'openbox|firefox|Xorg' 2>/dev/null; echo tw-desktop-ok"
}
