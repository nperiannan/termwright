package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ClipImage is a picture on the system clipboard, as base64 PNG.
type ClipImage struct {
	Data string `json:"data"`
	Type string `json:"type"`
}

// clipImageMax is the largest picture taken from the clipboard (the AI chat takes 5 MB).
const clipImageMax = 5 << 20

// ClipboardImage reads a picture from the system clipboard (a screenshot copied to it): the app's own
// Paste (⌘V / Ctrl+V in its Edit menu) reaches the page as text only, so the AI chat asks for the picture
// here. Empty when the clipboard holds no picture.
func (a *App) ClipboardImage() (*ClipImage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var png []byte
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.CommandContext(ctx, "osascript", "-e", "get the clipboard as «class PNGf»").Output()
		if err != nil {
			return nil, nil // no picture on the clipboard
		}
		png = macClipData(string(out))
	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms,System.Drawing; $i=[Windows.Forms.Clipboard]::GetImage(); if ($i) { $m=New-Object IO.MemoryStream; $i.Save($m,[Drawing.Imaging.ImageFormat]::Png); [Convert]::ToBase64String($m.ToArray()) }`
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-STA", "-Command", ps)
		hideWindow(cmd)
		out, err := cmd.Output()
		if err != nil {
			return nil, nil
		}
		png, _ = base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	default:
		for _, c := range [][]string{{"wl-paste", "--no-newline", "--type", "image/png"}, {"xclip", "-selection", "clipboard", "-t", "image/png", "-o"}} {
			if _, err := exec.LookPath(c[0]); err != nil {
				continue
			}
			if out, err := exec.CommandContext(ctx, c[0], c[1:]...).Output(); err == nil && len(out) > 0 {
				png = out
				break
			}
		}
	}
	if !isPNG(png) {
		return nil, nil
	}
	if len(png) > clipImageMax {
		return nil, errors.New("the picture on the clipboard is larger than 5 MB")
	}
	return &ClipImage{Data: base64.StdEncoding.EncodeToString(png), Type: "image/png"}, nil
}

var macData = regexp.MustCompile(`«data PNGf([0-9A-Fa-f]+)»`)

// macClipData: osascript prints the clipboard's PNG as «data PNGf89504E47…».
func macClipData(out string) []byte {
	m := macData.FindStringSubmatch(out)
	if m == nil {
		return nil
	}
	b, err := hex.DecodeString(m[1])
	if err != nil {
		return nil
	}
	return b
}

func isPNG(b []byte) bool { return bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) }
