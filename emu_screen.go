package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// The screen of a VM (a running lab device, or an installer): what its display shows, read through its QEMU
// monitor (screendump), and typing on its keyboard (sendkey) - for installers and systems that use the
// screen rather than the serial console. And typing a line on an installer's serial console.

// EmuScreen is a picture of a VM's screen.
type EmuScreen struct {
	PNG string `json:"png"` // data:image/png;base64,…
	W   int    `json:"w"`
	H   int    `json:"h"`
	At  string `json:"at"`
}

// emuScreenOf: where a VM's monitor is (kind "vm": a lab device; "install": an installer or its verification boot).
func (a *App) emuScreenOf(kind, id string) (host string, mon int, file string, err error) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	switch kind {
	case "install":
		j := emuInstance(id)
		if j == nil || j.Port == 0 {
			return "", 0, "", errors.New("the installer is not running")
		}
		port := j.Port
		if j.VerifyPort != 0 && j.Step == "verify" {
			port = j.VerifyPort
		}
		return j.Host, port + 1, "/tmp/tw-screen-" + j.ID + ".ppm", nil
	default:
		l := emuLab.f.Lab
		if l == nil {
			return "", 0, "", errors.New("there is no emulated lab")
		}
		v := l.VMByID(id)
		if v == nil {
			return "", 0, "", errors.New("no such device")
		}
		h := emuHostOf(*v)
		lh := a.emuFor(h)
		return h, lh.MonitorPort(*v), "/tmp/tw-screen-" + l.ID + "-" + v.ID + ".ppm", nil // (QEMU's own folder is not the lab's: a full path)
	}
}

// EmuScreen takes a picture of a VM's screen.
func (a *App) EmuScreen(kind, id string) (EmuScreen, error) {
	host, mon, file, err := a.emuScreenOf(kind, id)
	if err != nil {
		return EmuScreen{}, err
	}
	h, err := a.emuMonitor(host, mon)
	if err != nil {
		return EmuScreen{}, fmt.Errorf("it is not running (%w)", err)
	}
	out, err := h.do("screendump "+file, 15*time.Second)
	h.close()
	if err != nil || strings.Contains(out, "rror") {
		return EmuScreen{}, fmt.Errorf("its screen: %s", strings.TrimSpace(out+" "+errText(err)))
	}
	b64, err := a.emuRun(host, "gzip -c "+emu.Q(file)+" | base64 -w0; rm -f "+emu.Q(file), 30*time.Second)
	if err != nil {
		return EmuScreen{}, err
	}
	gz, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return EmuScreen{}, errors.New("its screen could not be read")
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return EmuScreen{}, err
	}
	img, err := emuPPM(zr)
	if err != nil {
		return EmuScreen{}, err
	}
	var pb bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&pb, img); err != nil {
		return EmuScreen{}, err
	}
	r := img.Bounds()
	return EmuScreen{PNG: "data:image/png;base64," + base64.StdEncoding.EncodeToString(pb.Bytes()), W: r.Dx(), H: r.Dy(), At: time.Now().Format(time.RFC3339)}, nil
}

// emuPPM reads a binary PPM (P6, 8 bits), as QEMU's screendump writes it.
func emuPPM(r io.Reader) (image.Image, error) {
	br := bufio.NewReader(r)
	var f [4]int
	for i := 0; i < 4; {
		var tok string
		if _, err := fmt.Fscan(br, &tok); err != nil {
			return nil, errors.New("not a screen picture")
		}
		if strings.HasPrefix(tok, "#") {
			_, _ = br.ReadString('\n')
			continue
		}
		if i == 0 {
			if tok != "P6" {
				return nil, errors.New("not a P6 picture")
			}
			i++
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil {
			return nil, errors.New("a bad picture header")
		}
		f[i] = n
		i++
	}
	if _, err := br.ReadByte(); err != nil { // the one white space after the header
		return nil, err
	}
	w, h := f[1], f[2]
	if w <= 0 || h <= 0 || w > 8192 || h > 8192 || f[3] != 255 {
		return nil, errors.New("a picture this size is not read")
	}
	px := make([]byte, w*h*3)
	if _, err := io.ReadFull(br, px); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Set(i%w, i/w, color.RGBA{px[i*3], px[i*3+1], px[i*3+2], 255})
	}
	return img, nil
}

// EmuScreenKeys types on a VM's keyboard: the text (enter: then Enter) and keys (QEMU key names: ret, esc, tab,
// up, down, f2, ctrl-alt-delete …).
func (a *App) EmuScreenKeys(kind, id, text string, enter bool, keys []string) error {
	host, mon, _, err := a.emuScreenOf(kind, id)
	if err != nil {
		return err
	}
	cmds := emu.SendKeys(text)
	if enter {
		cmds = append(cmds, "sendkey ret 40")
	}
	for _, k := range keys {
		if strings.Trim(k, "abcdefghijklmnopqrstuvwxyz0123456789_-") != "" {
			return fmt.Errorf("%q is not a key name", k)
		}
		cmds = append(cmds, "sendkey "+k)
	}
	if len(cmds) == 0 {
		return errors.New("nothing to type")
	}
	h, err := a.emuMonitor(host, mon)
	if err != nil {
		return fmt.Errorf("it is not running (%w)", err)
	}
	defer h.close()
	for _, c := range cmds {
		if out, err := h.do(c, 5*time.Second); err != nil || strings.Contains(out, "rror") || strings.Contains(out, "nvalid") {
			return fmt.Errorf("%s: %s", c, strings.TrimSpace(out+" "+errText(err)))
		}
	}
	return nil
}

// EmuInstallType types a line on an installer's serial console (enter: then Enter).
func (a *App) EmuInstallType(id, line string, enter bool) error {
	emuLab.mu.Lock()
	j := emuInstance(id)
	if j == nil || j.Port == 0 || !emuInstallRunning(j.Stage) && j.Stage != instWaiting {
		emuLab.mu.Unlock()
		return errors.New("the installer is not running")
	}
	host, port := j.Host, j.Port
	emuLab.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cl, err := a.emuClient(ctx, host)
	if err != nil {
		return err
	}
	c, err := cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("its console: %w", err)
	}
	defer c.Close()
	if enter {
		line += "\r"
	}
	_, err = c.Write([]byte(line))
	time.Sleep(300 * time.Millisecond)
	return err
}

// emuMCPIDView finds a lab device by name or id (for the API's paths).
func emuMCPIDView(a *App, name string) (string, error) {
	v, err := a.EmuState()
	if err != nil {
		return "", err
	}
	d, err := emuMCPID(v, name)
	return d.ID, err
}
