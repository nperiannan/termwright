package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// Checkpoints: a running switch's whole state kept in its disk (QEMU snapshots). Termwright takes one before
// Claude, the API or the AI agent types on a lab switch (at most one every two minutes per switch, none while
// traffic runs: a checkpoint pauses the switch for a moment), so Revert in the change history can undo typed
// commands too; Checkpoint now and Restore are also in the switch's panel.

// emuCkQuiet: a switch gets no new automatic checkpoint this soon after its last one.
const emuCkQuiet = 2 * time.Minute

// EmuCheckpoint takes a checkpoint of a running switch now (note: why); the oldest beyond three goes.
func (a *App) EmuCheckpoint(id, note string) (EmuView, error) {
	defer emuAs("user")()
	if _, err := a.emuCheckpoint(id, orStr(strings.TrimSpace(note), "taken by hand"), ""); err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// emuCheckpoint takes one (change: the history change it is for).
func (a *App) emuCheckpoint(id, note, change string) (emu.Checkpoint, error) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return emu.Checkpoint{}, errors.New("no such switch")
	}
	if im := emuImage(v.Image); im != nil && im.Board != "" {
		emuLab.mu.Unlock()
		return emu.Checkpoint{}, errors.New("a microcontroller has no checkpoints (its flash keeps what it wrote)")
	}
	host := emuHostOf(*v)
	lh := a.emuFor(host)
	ck := emu.Checkpoint{Tag: "tw-ck-" + randHex(3), At: time.Now().Format(time.RFC3339), Note: note, Change: change}
	var drop []string
	if n := len(v.Checkpoints) + 1 - emu.CheckpointKeep; n > 0 {
		for _, c := range v.Checkpoints[:n] {
			drop = append(drop, c.Tag)
		}
	}
	script := lh.CheckpointScript(*v, ck.Tag, drop)
	name := v.Name
	emuLab.mu.Unlock()
	out, err := a.emuRun(host, script, 6*time.Minute)
	if r := strings.TrimSpace(out); err != nil || !strings.HasSuffix(r, "ok") {
		why := strings.TrimPrefix(r[strings.LastIndex(r, "\n")+1:], "err ")
		if err != nil {
			why = err.Error()
		}
		emuEv(id, "warn", "no checkpoint: "+why)
		return emu.Checkpoint{}, fmt.Errorf("no checkpoint of %s: %s", name, why)
	}
	emuLab.mu.Lock()
	if v := emuLab.f.Lab.VMByID(id); v != nil {
		v.Checkpoints = append(slices.DeleteFunc(v.Checkpoints, func(c emu.Checkpoint) bool { return slices.Contains(drop, c.Tag) }), ck)
		_ = a.emuSave()
	}
	emuLab.mu.Unlock()
	emuEv(id, "info", "checkpoint "+ck.Tag+" taken ("+note+")")
	return ck, nil
}

// emuAutoCheckpoint is the checkpoint before typed commands on a lab switch (change: the history change they
// are part of): none when it is not running, when it has a fresh one, when traffic runs or when they are off.
// It returns the checkpoint used ("" = none) and why there is none.
func (a *App) emuAutoCheckpoint(id, what, change string) (string, string) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil || l.NoCheckpoints {
		emuLab.mu.Unlock()
		return "", "checkpoints are off"
	}
	if n := len(v.Checkpoints); n > 0 {
		if at, err := time.Parse(time.RFC3339, v.Checkpoints[n-1].At); err == nil && time.Since(at) < emuCkQuiet {
			tag := v.Checkpoints[n-1].Tag
			emuLab.mu.Unlock()
			return tag, ""
		}
	}
	traffic := emuTraf.run != 0 && !emuTraf.final
	emuLab.mu.Unlock()
	if traffic {
		return "", "no checkpoint while traffic runs (it would pause the switch)"
	}
	if !emuUp(a.emuPollFresh().Up, id) {
		return "", "it was not running"
	}
	ck, err := a.emuCheckpoint(id, "before "+what, change)
	if err != nil {
		return "", err.Error()
	}
	return ck.Tag, ""
}

// EmuCheckpointRestore brings a running switch back to a checkpoint.
func (a *App) EmuCheckpointRestore(id, tag string) (EmuView, error) {
	defer emuAs("user")()
	if err := a.emuRestore(id, tag); err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

func (a *App) emuRestore(id, tag string) error {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil || !slices.ContainsFunc(v.Checkpoints, func(c emu.Checkpoint) bool { return c.Tag == tag }) {
		emuLab.mu.Unlock()
		return errors.New("no such checkpoint")
	}
	host := emuHostOf(*v)
	script := a.emuFor(host).RestoreScript(*v, tag)
	emuLab.mu.Unlock()
	if !emuUp(a.emuPollFresh().Up, id) {
		return errors.New("start it first: a checkpoint is restored into the running switch")
	}
	out, err := a.emuRun(host, script, 6*time.Minute)
	if r := strings.TrimSpace(out); err != nil || !strings.HasSuffix(r, "ok") {
		if err == nil {
			err = errors.New(strings.TrimPrefix(r[strings.LastIndex(r, "\n")+1:], "err "))
		}
		return fmt.Errorf("restoring the checkpoint: %w", err)
	}
	emuEv(id, "info", "restored to its checkpoint "+tag)
	return nil
}

// EmuCheckpointRemove deletes a checkpoint.
func (a *App) EmuCheckpointRemove(id, tag string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such switch")
	}
	host := emuHostOf(*v)
	vv := *v
	emuLab.mu.Unlock()
	run := emuUp(a.emuPollFresh().Up, id)
	emuLab.mu.Lock()
	script := a.emuFor(host).DropCheckpointScript(vv, []string{tag}, run)
	emuLab.mu.Unlock()
	_, _ = a.emuRun(host, script, time.Minute)
	emuLab.mu.Lock()
	if v := emuLab.f.Lab.VMByID(id); v != nil {
		v.Checkpoints = slices.DeleteFunc(v.Checkpoints, func(c emu.Checkpoint) bool { return c.Tag == tag })
		_ = a.emuSave()
	}
	emuLab.mu.Unlock()
	return a.EmuState()
}

// EmuSetCheckpoints turns the automatic checkpoints before typed commands on or off for the lab.
func (a *App) EmuSetCheckpoints(on bool) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	if emuLab.f.Lab == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	emuLab.f.Lab.NoCheckpoints = !on
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// emuVMOfSession: the lab switch a session (path) is the SSH or console session of ("" = none).
func emuVMOfSession(path string) string {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	for _, m := range []map[string]string{emuLab.f.Sessions, emuLab.f.Consoles} {
		for id, p := range m {
			if p == path {
				return id
			}
		}
	}
	return ""
}

func emuUp(up map[string]float64, id string) bool { _, ok := up[id]; return ok }
