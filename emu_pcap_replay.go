package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"github.com/termwright/termwright/internal/emu"
)

// emuPcapMax is the largest capture a stream replays.
const emuPcapMax = 64 << 20

// emuPcapDir: where Termwright keeps the captures streams replay (as classic little-endian pcap, named by their contents).
func (a *App) emuPcapDir() string { return filepath.Join(a.labDir(), "pcap") }

// emuPcapKeep reads a pcap stream's capture, keeps it (normalized) for the hosts, and notes its frames and length.
func (a *App) emuPcapKeep(s *emu.Stream) error {
	if s.PcapID != "" { // kept already: its file may have moved since (a template, a change of its other settings)
		if _, err := os.Stat(filepath.Join(a.emuPcapDir(), s.PcapID+".pcap")); err == nil {
			if _, err := os.Stat(s.Pcap); s.Pcap == "" || err != nil {
				return nil
			}
		}
	}
	if s.Pcap == "" {
		return errors.New("choose the capture (a .pcap file) the stream replays")
	}
	fi, err := os.Stat(s.Pcap)
	if err != nil {
		return fmt.Errorf("the capture %s: %w", s.Pcap, err)
	}
	if fi.Size() > emuPcapMax {
		return fmt.Errorf("the capture is %d MB: at most %d MB", fi.Size()>>20, emuPcapMax>>20)
	}
	b, err := os.ReadFile(s.Pcap)
	if err != nil {
		return err
	}
	rs, ok := emuPcapRecords(b)
	if !ok {
		return errors.New("not a pcap file (pcapng is not read: in Wireshark, File → Save As… Wireshark/tcpdump pcap)")
	}
	if len(rs) == 0 {
		return errors.New("the capture has no frames")
	}
	norm := emuPcapWrite(rs)
	sum := sha256.Sum256(norm)
	id := hex.EncodeToString(sum[:6])
	if err := os.MkdirAll(a.emuPcapDir(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(a.emuPcapDir(), id+".pcap"), norm, 0o644); err != nil {
		return err
	}
	s.PcapID, s.PcapFrames, s.PcapSecs = id, len(rs), rs[len(rs)-1].ts-rs[0].ts
	s.Loops = max(0, s.Loops)
	return nil
}

// emuPcapUpload puts a kept capture into the folder where the lab runs on a host (dir: its VMDir; once).
func (a *App) emuPcapUpload(host, dir, id string) error {
	emuLab.mu.Lock()
	done := emuLab.pcapUp[host+"|"+dir+"|"+id]
	emuLab.mu.Unlock()
	if done {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(a.emuPcapDir(), id+".pcap"))
	if err != nil {
		return fmt.Errorf("the capture %s is gone from this computer: choose it again in its stream", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cl, err := a.emuClient(ctx, host)
	if err != nil {
		return err
	}
	sc, err := sftp.NewClient(cl)
	if err != nil {
		return fmt.Errorf("SFTP to the host: %w", err)
	}
	defer sc.Close()
	remote := dir + "/pcap/" + id + ".pcap"
	if fi, err := sc.Stat(remote); err != nil || fi.Size() != int64(len(b)) {
		_ = sc.MkdirAll(dir + "/pcap")
		f, err := sc.Create(remote + ".tmp")
		if err != nil {
			return fmt.Errorf("could not write %s on the host: %w", remote, err)
		}
		if _, err := f.Write(b); err != nil {
			_ = f.Close()
			return err
		}
		_ = f.Close()
		_ = sc.Remove(remote)
		if err := sc.Rename(remote+".tmp", remote); err != nil {
			return err
		}
	}
	emuLab.mu.Lock()
	if emuLab.pcapUp == nil {
		emuLab.pcapUp = map[string]bool{}
	}
	emuLab.pcapUp[host+"|"+dir+"|"+id] = true
	emuLab.mu.Unlock()
	return nil
}
