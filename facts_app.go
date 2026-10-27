package main

import (
	"context"
	"time"

	"github.com/termwright/termwright/internal/facts"
)

// DeviceFacts returns the model, OS version, serial number and base MAC
// address of a device for a report: first from the given terminal text
// (output already on screen), then - when fetch is true, something is still
// missing, and the session is an SSH or Telnet session with a known device
// type - by running the device's "show" commands in a background connection
// (nothing is typed in the tab). The MAC address saved with the session is
// the last resort.
func (a *App) DeviceFacts(path, deviceType, text string, fetch bool) facts.Facts {
	fam := facts.Family(deviceType)
	f := facts.Parse(fam, text)
	if path == "" {
		return f
	}
	s, err := a.store.Get(path)
	if err != nil {
		return f
	}
	if fam == "" {
		fam = facts.Family(s.DeviceType)
	}
	if fetch && !f.Complete() && fam != "" && (s.Protocol == "SSH2" || s.Protocol == "Telnet") {
		if s2, err := a.consoleSession(a.withFolderDefaults(s)); err == nil {
			prompt := ""
			if s2.DeviceType != "" && a.terms.DeviceType != nil {
				if dt, ok := a.terms.DeviceType(s2.DeviceType); ok {
					prompt = dt.Prompt
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			out, err := a.terms.Capture(ctx, s2, facts.Commands(fam), prompt)
			cancel()
			if err == nil {
				f = f.Merge(facts.Parse(fam, out))
			}
		}
	}
	if f.MAC == "" {
		f.MAC = s.MAC
	}
	return f
}
