package main

import (
	_ "embed"
	"errors"
	"fmt"
)

// The script "Set Up Remote Desktop" runs on the Linux machine (Ubuntu 24.04):
// XFCE with TigerVNC and/or xrdp, each only when it is not already running.
//
//go:embed scripts/remote-desktop/setup-remote-desktop.sh
var remoteDesktopScript []byte

// UploadRemoteDesktopScript copies the setup script to the machine of an open
// SSH terminal tab, over that tab's own connection (no second login), and
// returns where it is. The file is private to the login user and is removed
// again by the command that runs it.
func (a *App) UploadRemoteDesktopScript(tabID string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	id, _, err := a.terms.SFTPOpen(tabID, "")
	if err != nil {
		return "", fmt.Errorf("could not open a file channel on this SSH session: %w", err)
	}
	defer a.terms.SFTPClose(id)
	remote := "/tmp/termwright-rd-" + randHex(6) + ".sh"
	if len(remoteDesktopScript) == 0 {
		return "", errors.New("the setup script is missing from this build")
	}
	if err := a.terms.SFTPWriteFile(id, remote, remoteDesktopScript, 0o600); err != nil {
		return "", fmt.Errorf("could not copy the script to %s: %w", remote, err)
	}
	return remote, nil
}
