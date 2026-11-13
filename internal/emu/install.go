package emu

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// InstallSpec is a machine that installs an operating system from an ISO file:
// a blank disk and the ISO in a QEMU of its own on a host, with the serial
// console on a TCP port of the host (the installer's text goes there).
type InstallSpec struct {
	ID   string
	Dir  string // the lab's folder on the host (relative to the home folder)
	ISO  string // the ISO's file name in Dir/images
	GB   int    // the disk
	MB   int    // the disk in MB, when it is not a whole number of GB (then GB is not used)
	RAM  int    // MB
	CPUs int
}

// DiskSize is the disk's size as qemu-img takes it ("8G", "512M").
func (s InstallSpec) DiskSize() string {
	if s.MB > 0 {
		return fmt.Sprintf("%dM", s.MB)
	}
	return fmt.Sprintf("%dG", s.GB)
}

// InstallDir is the folder of the installers' disks and logs on a host.
func (s InstallSpec) InstallDir() string { return s.Dir + "/install" }

// InstallDisk is the blank disk the installer writes to.
func (s InstallSpec) InstallDisk() string { return s.InstallDir() + "/" + s.ID + ".qcow2" }

// StartScript makes the disk (once) and starts the installer; it prints
// "port N" (its console) and "started", or "fail …".
func (s InstallSpec) StartScript() string {
	d, id := Q(s.InstallDir()), s.ID
	var b strings.Builder
	fmt.Fprintf(&b, "mkdir -p %s || exit 1\n", d)
	fmt.Fprintf(&b, "if %s; then echo \"port $(cat %s/%s.port)\"; echo started; exit 0; fi\n", qemuUp(d+"/"+id+".pid"), d, id)
	fmt.Fprintf(&b, "[ -f %s/%s ] || { echo 'fail the ISO is not on the host'; exit 0; }\n", Q(s.Dir+"/images"), Q(s.ISO))
	fmt.Fprintf(&b, "[ -f %s ] || e=$(qemu-img create -q -f qcow2 %s %s 2>&1) || { echo \"fail could not make the disk: $e\"; exit 0; }\n", Q(s.InstallDisk()), Q(s.InstallDisk()), s.DiskSize())
	// a free port pair for the console and the monitor
	b.WriteString("p=24900; while { ss -ltn 2>/dev/null || netstat -ltn 2>/dev/null; } | grep -qE \":($p|$((p+1)))[[:space:]]\"; do p=$((p+2)); done\n")
	b.WriteString("acc='-accel tcg'; [ -w /dev/kvm ] && acc='-enable-kvm -cpu host'\n")
	fmt.Fprintf(&b, ": > %s/%s.log; echo $p > %s/%s.port\n", d, id, d, id)
	fmt.Fprintf(&b, "out=$(qemu-system-x86_64 -name twinst-%s -m %d -smp %d $acc "+
		"-drive file=%s,if=virtio,format=qcow2 -drive file=%s/%s,media=cdrom,readonly=on,if=ide,index=3 -boot order=cd,menu=off "+
		"-netdev user,id=n0 -device virtio-net-pci,netdev=n0 "+
		"-chardev socket,id=con0,host=127.0.0.1,port=$p,server=on,wait=off,telnet=on,logfile=%s/%s.log -serial chardev:con0 "+
		"-monitor telnet:127.0.0.1:$((p+1)),server,nowait -display none -daemonize -pidfile %s/%s.pid 2>&1) || { echo \"fail $(echo $out | tr '\\n' ' ' | cut -c1-300)\"; exit 0; }\n",
		id, s.RAM, s.CPUs, Q(s.InstallDisk()), Q(s.Dir+"/images"), Q(s.ISO), d, id, d, id)
	b.WriteString("echo \"port $p\"; echo started\n")
	return b.String()
}

// StatusScript prints "up" while the installer runs, else "down"; and its console port.
func (s InstallSpec) StatusScript() string {
	d := Q(s.InstallDir())
	return fmt.Sprintf("if %s; then echo up; else echo down; fi; cat %s/%s.port 2>/dev/null | sed 's/^/port /'\n", qemuUp(d+"/"+s.ID+".pid"), d, s.ID)
}

// StopScript ends the installer (at once: the disk is what counts).
func (s InstallSpec) StopScript() string {
	d := Q(s.InstallDir())
	return fmt.Sprintf("if %s; then kill \"$p\" 2>/dev/null; sleep 1; fi; rm -f %s/%s.pid\necho stopped\n", qemuUp(d+"/"+s.ID+".pid"), d, s.ID)
}

// ProgFile is where the conversion of the disk writes its progress.
func (s InstallSpec) ProgFile() string { return s.InstallDir() + "/" + s.ID + ".prog" }

// SaveScript turns the installed disk into an image file (one file, no
// backing file, compressed) in the images folder, with its checksum; it
// prints "saved SIZE SHA" or "fail …". The conversion's progress goes to
// ProgFile (ProgressScript reads it).
func (s InstallSpec) SaveScript(fileName string) string {
	dst := Q(s.Dir + "/images/" + fileName)
	pg := Q(s.ProgFile())
	return fmt.Sprintf("mkdir -p %s; qemu-img convert -p -c -O qcow2 %s %s.part > %s 2>&1 || { e=$(tr '\\r' '\\n' < %s | grep -v '/100%%' | tr '\\n' ' ' | cut -c1-300); rm -f %s.part; echo \"fail $e\"; exit 0; }\n"+
		"mv -f %s.part %s && h=$(sha256sum %s | cut -d' ' -f1) && echo \"$h\" > %s.sha256 && echo \"saved $(stat -c %%s %s) $h\"\n",
		Q(s.Dir+"/images"), Q(s.InstallDisk()), dst, pg, pg, dst, dst, dst, dst, dst, dst)
}

// ProgressScript prints how far the install is: "disk N" (the installer's
// disk on the host, in bytes: it grows as the system is written), "part N"
// (the image being made, bytes), "pct P" (the conversion, percent), "idle S"
// (seconds since the installer's console showed anything) and "log BASE64"
// (the end of the console). fileName is the image being made ("" before).
func (s InstallSpec) ProgressScript(fileName string) string {
	d, id := Q(s.InstallDir()), s.ID
	var b strings.Builder
	fmt.Fprintf(&b, "echo \"disk $(stat -c %%s %s/%s.qcow2 2>/dev/null || echo 0)\"\n", d, id)
	if fileName != "" {
		fmt.Fprintf(&b, "echo \"part $(stat -c %%s %s.part 2>/dev/null || echo 0)\"\n", Q(s.Dir+"/images/"+fileName))
	}
	fmt.Fprintf(&b, "echo \"pct $(tr '\\r' '\\n' < %s 2>/dev/null | grep -o '[0-9.]*/100%%' | tail -1 | cut -d/ -f1)\"\n", Q(s.ProgFile()))
	fmt.Fprintf(&b, "echo \"idle $(( $(date +%%s) - $(stat -c %%Y %s/%s.log 2>/dev/null || date +%%s) ))\"\n", d, id)
	fmt.Fprintf(&b, "echo \"log $(tail -c 3000 %s/%s.log 2>/dev/null | base64 | tr -d '\\n')\"\n", d, id)
	return b.String()
}

// Progress is what ProgressScript printed.
type Progress struct {
	Disk, Part int64
	Pct        float64
	Idle       int
	Log        string // the end of the console, cleaned
}

// ParseProgress reads what ProgressScript printed.
func ParseProgress(out string) Progress {
	var p Progress
	for _, ln := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(ln), " ")
		switch k {
		case "disk":
			p.Disk, _ = strconv.ParseInt(v, 10, 64)
		case "part":
			p.Part, _ = strconv.ParseInt(v, 10, 64)
		case "pct":
			p.Pct, _ = strconv.ParseFloat(v, 64)
		case "idle":
			p.Idle, _ = strconv.Atoi(v)
		case "log":
			if raw, err := base64.StdEncoding.DecodeString(v); err == nil {
				t := string(raw)
				if i := strings.IndexByte(t, '\n'); i >= 0 && len(raw) >= 3000 {
					t = t[i+1:] // the cut start of a line
				}
				p.Log = CleanConsole(t)
			}
		}
	}
	return p
}

// LastLines are the last n lines of console text that show something, each cut to width.
func LastLines(s string, n, width int) string {
	ls := strings.Split(s, "\n")
	var out []string
	for i := len(ls) - 1; i >= 0 && len(out) < n; i-- {
		t := strings.TrimRight(ls[i], " \t")
		if strings.TrimSpace(t) == "" {
			continue
		}
		if r := []rune(t); len(r) > width {
			t = string(r[:width]) + "…"
		}
		out = append(out, t)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return strings.Join(out, "\n")
}

// CleanScript removes the installer's disk, log and files (not the ISO).
func (s InstallSpec) CleanScript() string {
	d := Q(s.InstallDir())
	return fmt.Sprintf("rm -f %s/%s.qcow2 %s/%s.log %s/%s.port %s/%s.pid %s/%s.prog\n", d, s.ID, d, s.ID, d, s.ID, d, s.ID, d, s.ID)
}

// ParseInstall reads what StartScript or StatusScript printed.
func ParseInstall(out string) (up bool, port int, fail string) {
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case ln == "up" || ln == "started":
			up = true
		case strings.HasPrefix(ln, "port "):
			fmt.Sscanf(ln, "port %d", &port)
		case strings.HasPrefix(ln, "fail "):
			fail = strings.TrimPrefix(ln, "fail ")
		}
	}
	return
}

// AlpineOpts is what the Alpine installer is told.
type AlpineOpts struct {
	Hostname string
	Password string // root's
	Disk     string // the disk to install to ("/dev/vda")
	DNS      string // its name server (default DefaultDNS[0])
}

// AlpineAnswers is the answer file of setup-alpine as one shell command that writes it to /tmp/ans.
func AlpineAnswers(o AlpineOpts) string {
	host := orStr(o.Hostname, "alpine")
	disk := orStr(o.Disk, "/dev/vda")
	lines := []string{
		`KEYMAPOPTS="us us"`,
		`HOSTNAMEOPTS="-n ` + host + `"`,
		`INTERFACESOPTS="auto lo\niface lo inet loopback\n\nauto eth0\niface eth0 inet dhcp\n"`,
		`DNSOPTS="-d lab -n ` + orStr(o.DNS, DefaultDNS[0]) + `"`, // setup-dns takes the server after -n (one: setup-alpine splits the words)
		`TIMEZONEOPTS="-z UTC"`,
		`PROXYOPTS="none"`,
		`APKREPOSOPTS="-1"`,
		`USEROPTS="none"`,
		`SSHDOPTS="none"`,
		`NTPOPTS="none"`,
		`DISKOPTS="-m sys ` + disk + `"`,
		`LBUOPTS="none"`,
		`APKCACHEOPTS="none"`,
	}
	return "printf '" + strings.Join(lines, `\n`) + `\n' > /tmp/ans`
}

var (
	alpineLogin  = regexp.MustCompile(`(?i)login:\s*$`)
	alpineRoot   = regexp.MustCompile(`#\s*$`)
	alpineDone   = regexp.MustCompile(`TWEND-0\b`)
	alpineFailed = regexp.MustCompile(`TWEND-[1-9]\d*`)
)

// InstallAlpine drives the installer of an Alpine Linux ISO on its serial
// console: it logs in as root, writes the answer file and runs setup-alpine
// with it (the disk is erased and installed with the ISO's own packages), then
// powers the machine off. note is told what it does. The caller waits for the
// machine to stop.
func InstallAlpine(ctx context.Context, c *Console, o AlpineOpts, note func(step, msg string)) error {
	if o.Password == "" {
		return errors.New("the root password must not be empty")
	}
	c.Confirm = true // "Erase the above disk(s) and continue? [y/N]" is answered y: the disk is the blank one
	note("boot", "waiting for the installer to boot")
	deadline := time.Now().Add(15 * time.Minute)
	for {
		if _, err := c.Wait(ctx, 20*time.Second, nil, alpineLogin); err == nil {
			break
		} else if !errors.Is(err, ErrTimeout) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the installer showed no login prompt on the serial console in 15 minutes - this ISO may not use the serial console (use a manual install and add console=ttyS0 at its boot menu). The console showed: %s", c.Tail(200))
		}
		_ = c.Send("")
	}
	note("login", "logging in")
	if err := c.Send("root"); err != nil {
		return err
	}
	if _, err := c.Wait(ctx, 60*time.Second, nil, alpineRoot); err != nil {
		return fmt.Errorf("no shell after logging in as root: %w - the console showed: %s", err, c.Tail(200))
	}
	if err := c.Send(AlpineAnswers(o)); err != nil {
		return err
	}
	if _, err := c.Wait(ctx, 30*time.Second, nil, alpineRoot); err != nil {
		return fmt.Errorf("the answer file was not written: %w - the console showed: %s", err, c.Tail(200))
	}
	note("install", "running setup-alpine (installing to the disk)")
	pw := []Answer{{Match: "New password:", Send: o.Password, secret: true}, {Match: "Retype password:", Send: o.Password, secret: true},
		{Match: "[no]", Send: "no"}} // "Setup a user? (enter a lower-case loginname, or 'no') [no]": an Alpine that asks although the answer file says none
	if err := c.Send("setup-alpine -f /tmp/ans; echo TWEND-$?"); err != nil {
		return err
	}
	i, err := c.Wait(ctx, 25*time.Minute, pw, alpineDone, alpineFailed)
	if err != nil {
		return fmt.Errorf("setup-alpine did not finish: %w - the console showed: %s", err, c.Tail(300))
	}
	if i == 1 {
		return fmt.Errorf("setup-alpine failed - the console showed: %s", c.Tail(400))
	}
	note("poweroff", "installed; powering off")
	return c.Send("poweroff")
}
