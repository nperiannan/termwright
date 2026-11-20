package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// deviceTool is the "device" network tool (AI assistant, MCP, POST /tools/device): a saved session's
// device facts and its device type.
func (a *App) deviceTool(_ context.Context, args map[string]any, ask netAsker) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	act := strings.ToLower(argStr(args, "action"))
	path := argStr(args, "session")
	if act == "types" {
		var b strings.Builder
		for _, d := range a.cfg.Get().DeviceTypes {
			fmt.Fprintf(&b, "%s  detect %s  (command: %s)\n", d.Name, orDash(d.Detect), d.DetectCmd())
		}
		return b.String(), nil
	}
	s, err := a.store.Get(path)
	if err != nil {
		return "", fmt.Errorf("no saved session %q (find it with list_sessions or find)", path)
	}
	switch act {
	case "facts", "":
		return factsText(s.Path, s.DeviceType, a.cfg.Get().DeviceFacts[s.Path]), nil
	case "refresh_facts":
		f, err := a.RefreshFacts(s.Path, true)
		if err != nil {
			return "", err
		}
		return factsText(s.Path, s.DeviceType, f), nil
	case "identify":
		m, err := a.IdentifyDeviceType(s.Path)
		if err != nil {
			return "", err
		}
		if m.Type == "" {
			return fmt.Sprintf("%s: no device type's Detect pattern matched the output of %s (now: %s). A device type with a Detect pattern for this device can be added in Global Options → Device Types.", s.Path, strings.Join(m.Tried, ", "), orDash(m.Current)), nil
		}
		if m.Type == m.Current {
			return fmt.Sprintf("%s is %s (the output of %s matched %q).", s.Path, m.Type, m.Command, m.Matched), nil
		}
		return fmt.Sprintf("%s looks like %s: the output of %s matched %q (now: %s). Use set_type to change it.", s.Path, m.Type, m.Command, m.Matched, orDash(m.Current)), nil
	case "set_type":
		typ := argStr(args, "type")
		found := ""
		for _, d := range a.cfg.Get().DeviceTypes {
			if strings.EqualFold(d.Name, typ) {
				found = d.Name
			}
		}
		if found == "" && typ != "" {
			return "", fmt.Errorf("no device type %q (types lists them)", typ)
		}
		if strings.EqualFold(found, s.DeviceType) {
			return fmt.Sprintf("%s is already %s.", s.Path, orDash(found)), nil
		}
		if err := ask("Set the device type of "+s.Path, fmt.Sprintf("Device type of %s: %s → %s", s.Path, orDash(s.DeviceType), orDash(found)), false); err != nil {
			return "", err
		}
		s.DeviceType = found
		if _, err := a.store.Save(s, s.Path); err != nil {
			return "", err
		}
		emitter{a}.Emit("sessions:changed", s.Path)
		return fmt.Sprintf("%s is now %s.", s.Path, orDash(found)), nil
	}
	return "", fmt.Errorf("unknown action %q (facts, refresh_facts, identify, set_type, types)", act)
}

func factsText(path, typ string, f map[string]string) string {
	var ks []string
	for k := range f {
		if !strings.HasPrefix(k, "_") {
			ks = append(ks, k)
		}
	}
	if len(ks) == 0 {
		return fmt.Sprintf("%s (%s): no device facts yet - refresh_facts reads them (the device type needs fact rules).", path, orDash(typ))
	}
	sort.Strings(ks)
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)", path, orDash(typ))
	if at := f["_at"]; at != "" {
		b.WriteString(", read " + at)
	}
	b.WriteString("\n")
	for _, k := range ks {
		fmt.Fprintf(&b, "  %s: %s\n", k, f[k])
	}
	return b.String()
}
