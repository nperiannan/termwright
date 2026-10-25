#!/usr/bin/env bash
# Builds a .deb from build/bin/termwright. Usage: build/linux/build-deb.sh <version> <outdir>
set -euo pipefail
VERSION="$1"; OUT="$2"
ARCH=$(dpkg --print-architecture)
ROOT=$(mktemp -d)
chmod 755 "$ROOT"
install -Dm755 build/bin/termwright "$ROOT/usr/bin/termwright"
install -Dm644 build/linux/termwright.desktop "$ROOT/usr/share/applications/termwright.desktop"
install -Dm644 build/appicon.png "$ROOT/usr/share/pixmaps/termwright.png"
install -Dm644 LICENSE "$ROOT/usr/share/doc/termwright/copyright"
mkdir -p "$ROOT/DEBIAN"
cat > "$ROOT/DEBIAN/control" <<CTRL
Package: termwright
Version: $VERSION
Section: net
Priority: optional
Architecture: $ARCH
Maintainer: Termwright contributors <termwright@users.noreply.github.com>
Depends: libgtk-3-0t64 | libgtk-3-0, libwebkit2gtk-4.1-0
Recommends: lrzsz
Homepage: https://github.com/nperiannan/termwright
Description: Terminal emulator and session manager (SSH2, Telnet, Serial, SFTP)
 Termwright is an open-source terminal emulator with a session manager that
 reads and writes SecureCRT-compatible session files.
CTRL
mkdir -p "$OUT"
dpkg-deb --build --root-owner-group "$ROOT" "$OUT/termwright_${VERSION}_${ARCH}.deb"
rm -rf "$ROOT"
