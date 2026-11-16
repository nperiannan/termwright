# Emulated Lab: a guide from the host to the first test

This guide takes you through the emulated lab in the order you do the work: prepare a Linux host, let Termwright set it up, get images, make an image from an ISO, build a topology, spread it over several hosts, send traffic, run a complete test - and then do the same by asking the AI agent. The [User Guide](USER_GUIDE.md#emulated-lab-real-vm-images-on-a-kvm-host) describes every screen in detail; this guide is the path through them.

**What the emulated lab is.** Termwright boots the network operating systems' own VM images (`.qcow2` and friends) with QEMU/KVM on a Linux server you reach over SSH. The real operating system runs, so its CLI, protocols, APIs and cloud agents behave as on hardware. Your computer only drives the host: Termwright runs on macOS, Windows or Linux and talks to the host over SSH; nothing of Termwright is installed there besides QEMU and a few small scripts in the lab's folder.

```
 your computer (Termwright)  ── SSH ──▶  KVM host 1 (QEMU/KVM)  ◀── UDP cables ──▶  KVM host 2
   canvas, consoles, traffic,             switches, routers, PCs,                     more switches
   reports, AI agent                       test ports, captures
```

---

## 1. What you need

### 1.1 The KVM host

| | Tested | Also works |
|---|---|---|
| Operating system | **Ubuntu 24.04 LTS and 22.04 LTS** (server or desktop), x86-64 | Debian 12/13, Fedora, RHEL / Rocky / Alma, openSUSE, Arch - *Set Up* installs QEMU from each one's own packages |
| CPU | x86-64 with hardware virtualization (Intel VT-x or AMD-V) | without it QEMU runs in software emulation: fine for microcontrollers and small Linux VMs, too slow for most switch images |
| Memory | 2 GB per switch for most images (some need 4-8 GB), plus 2 GB for the host | |
| Disk | 50 GB free or more: each image once, and each switch a thin layer of its own on top of it | |
| Access | SSH with a key or a password; a user with **password-less sudo** (needed for the host's network, routed management and test ports on network cards) | without sudo: management by port forwarding on the host, no bridging |

**Real hardware as the host.** Turn on virtualization in the BIOS/UEFI (*Intel Virtualization Technology* / *SVM Mode*), then check on the host:

```sh
egrep -c '(vmx|svm)' /proc/cpuinfo   # more than 0: the CPU can virtualize
ls -l /dev/kvm                        # present: the kvm module is loaded
```

**A virtual machine as the host.** The VM must get the CPU's virtualization from its hypervisor - *nested virtualization*. Turn it on for the VM in your hypervisor (it is usually called *expose hardware-assisted virtualization to the guest*, or the CPU type *host-passthrough* with nesting on) and check `/dev/kvm` inside the VM as above. Two more things matter in a VM:

- **Management on the host's network (bridged)** needs the hypervisor's virtual switch to pass frames from MAC addresses other than the VM's own (often called *promiscuous mode*, *MAC address changes* and *forged transmits*). Where you cannot allow that, use **The host's network, routed** (see 4.3): the switches then use the host's MAC address.
- Give the VM enough vCPUs and memory for all the switches it will run.

### 1.2 Your computer

- Termwright on macOS, Windows or Linux.
- An SSH session to each host in the Session Manager (*＋ New Session* → SSH2, the host's address, the user). Connect once, so the host's key is known and the login works.

### 1.3 Several hosts (optional)

- The hosts reach each other over **UDP** (the cables between their switches) - open it in host firewalls between them.
- Moving a running switch between hosts without downtime also needs **TCP 29400-29999** between them, and the same acceleration on both (KVM on both).

---

## 2. Add the host and set it up

1. **Lab → KVM Hosts** (or the cube button at the top right, then *Hosts*).
2. **＋ Add**: your SSH sessions are listed folder by folder. Tick the host's session and press **Check and Add**.
3. Termwright logs in and checks: KVM (`/dev/kvm`), QEMU (`qemu-system-x86_64`, `qemu-img`), libguestfs, free vCPU, memory and disk, password-less sudo and the NAT network. The host shows a badge: **ready**, **needs set up** or **N to fix**, and what each check found.
4. **Set Up QEMU/KVM…** fixes what is missing. It shows the commands first, asks for the sudo password when sudo needs one (it goes to sudo on the host only and is not saved), then installs QEMU and its tools from the distribution's packages, loads the KVM module when `/dev/kvm` is missing and adds your user to the `kvm` group - with a live log. The host is checked again at the end. The switches still run as your user, not as root.

On Ubuntu, Set Up does the equivalent of:

```sh
sudo apt-get install -y qemu-system-x86 qemu-utils libguestfs-tools
sudo modprobe kvm_intel || sudo modprobe kvm_amd
sudo usermod -aG kvm "$USER"
```

When the host shows **ready**, its vCPU, memory and disk appear with the ways its switches can be managed - the best one is marked.

---

## 3. Get images (appliances)

**Lab → Appliances** is the page of the images and what is made from them. The images are listed by family on the left, each with a state: *this computer*, *on the host*, *to download*, *downloading*, *copying*.

You can get an image in four ways:

1. **Your own image file** - *Add Image (.qcow2, .img, .vmdk)…*. Termwright recognises many images by their file name (their kind and version) and fills in their settings; check them in the image's cards.
2. **The public catalogue** - *Public catalogue*: ready settings for open-source routers, firewalls, routing suites, Linux switch OSes and small Linux hosts. **Add** puts an appliance's versions in the list, **Download All** fetches the files (the MD5 sums are checked, `.gz`, `.xz` and `.bz2` are unpacked). Images whose maker wants you to sign in have *Open the download page*: download the file there, then *Choose…* it.
3. **A GNS3 appliance file** - *Import GNS3 Appliance (.gns3a)…* reads the settings of every version in it; image files next to it are found by themselves, the others can be downloaded.
4. **Cloud images** of Linux distributions (Debian, Ubuntu, Rocky, AlmaLinux) in the public catalogue: Termwright writes the cloud-init disk they need (login `root` / `termwright`, serial console).

**An image's settings** (the cards on the right): *File* (and further disks, a CD-ROM, a BIOS), *Hardware* (vCPU, memory, network card model and number of ports, port names), *Console login* (what the console shows when it is ready, the login and password - kept in the system keychain), *First boot* (the day-0 commands typed on the console the first time, with `{hostname}`, `{mgmtIp}` and others) and *Management address* (the command that reads the address, the commands that turn DHCP on or set a fixed address).

**Copy to Host…** puts the image on a host before you need it; otherwise it is copied when the first switch from it starts (and checked by its hash).

---

## 4. Make a qcow2 image from an ISO

Some operating systems come only as an installer ISO. An ISO is not a disk you can boot as a switch: it has to be installed onto a disk first.

### 4.1 In Termwright: Install from ISO

1. **Appliances → Install from ISO…** (under the image list).
2. Choose the **ISO file**, how to install it (**automatic** for Alpine, **by hand** for anything else), the image's **name**, the **root password**, the **disk size** (the most the disk can grow to; the qcow2 file only takes what is written - 8 GB is plenty for most), **memory**, **vCPUs** and the **host**.
3. **Start**: the ISO is copied to the host (once; checked by its SHA-256) and the installer boots in QEMU with a blank disk and a text console. The install shows on the right of the page.
4. **Answer the installer** in that console: type a line and Enter, or use the key buttons (Enter, Esc, Tab, arrows, F2, F12). *Screen…* shows the VGA screen for installers that use it. An automatic Alpine install answers by itself; *Take Over by Hand* lets you go on yourself.
5. When the installer is done, **Finish** saves the disk as an image in the list. Termwright boots it once on a throwaway copy and waits up to 5 minutes for its login prompt: the install says **Verified** (with the time it took) or **Not verified** and why.
6. Set the image's console login and day-0 commands (section 3) and it is ready for switches.

### 4.2 By hand on the host (optional)

The same with QEMU's own tools, then *Add Image* the result:

```sh
qemu-img create -f qcow2 myos.qcow2 8G
qemu-system-x86_64 -enable-kvm -m 2048 -smp 2 -nographic \
  -cdrom myos-installer.iso -boot d \
  -drive file=myos.qcow2,if=virtio,format=qcow2 \
  -netdev user,id=n0 -device virtio-net-pci,netdev=n0
# install, power off, then compact it if you like:
qemu-img convert -O qcow2 -c myos.qcow2 myos-compact.qcow2
```

### 4.3 How the lab's switches are managed

The first port of every switch is its management port. Choose how it is reached in *New Topology* (or the lab's *Management*):

- **The host's network (bridged)** - the default when the host has password-less sudo. The switch is on the host's network and gets an address from your DHCP server (or a fixed one); your computer and cloud services reach it directly.
- **The host's network, routed** - for hosts where bridging does not work (a VM whose virtual switch drops other MAC addresses, a host on Wi-Fi): the switches get fixed addresses on the host's network and the host answers and forwards for them.
- **The host's NAT network** - the switches are behind the host (libvirt's `virbr0`); Termwright reaches them through the host.
- **Port forwarding on the host** - no root needed; each switch's SSH and console are forwarded through the host.

---

## 5. Build a topology on the canvas

1. **Lab → Emulated Lab**, then **Topology ▾ → New Topology…**.
2. Pick the devices on the left (grouped by operating system); each row gets its software version, how many, the names (`{n}` is the number: `access-{n}`) and its role.
3. Choose the **cabling**: leaf-spine, ring, line, full mesh, star or no links; the **host** (each with what it has free); the **management** (4.3); and **Start now**.
4. The switches appear on the canvas while they boot: *copying the image*, *booting*, *day-0 config 3/5*, *running*. The **Events** pane at the bottom lists every step, with the reason when one fails.

Working on the canvas:

- **Add Link**: click two switches - each click takes that switch's next free port (Alt-click to choose the port). Right-click a cable to **Pull** it (the link goes down on both switches), **Plug In** or remove it.
- **Console**: double-click a switch. **SSH**: right-click → SSH. Each topology has a folder in the Session Manager (*Emulated Lab → <topology>*) with an SSH session per switch (*Mgmt*) and a console session per switch (*Console*).
- **Configure…** sets vCPU, memory, disk, the persona (the model, serial numbers and MACs it acts as, for images that support it) and the day-0 commands.
- **Start All / Stop All**, **Restart**, **Back to Day 0** (a fresh disk, day 0 typed again) and **Delete** are on the toolbar and the right-click menu.
- **Fast start** (in the lab's bar, on by default): *Stop and Save State* saves a running switch's state, and its next start resumes it in seconds.
- **Topology ▾** keeps the lab under a name and opens saved ones again. The switches keep running on the host when Termwright closes.

---

## 6. Use several hosts

1. Add and set up each host as in section 2.
2. In **Add Switches** (or New Topology) choose another checked host for the new switches; the canvas can draw an area round each host (*Hosts → Areas*).
3. **Cables between hosts** are UDP between the hosts' addresses; a new cable between two hosts works once both of its switches restart.
4. **Move to Host** (right-click a switch) moves a switch:
   - **Live (No Downtime)** - while it runs (TCP 29400-29999 between the hosts, KVM on both);
   - **Save, Copy and Resume** (or **Copy Its Disk** when it is stopped) - its disk and state go along, about a minute off;
   - **From Its Image** - it boots there from day 0.
5. The **hosts** button in the lab's header shows each host's switches, vCPU, memory and load, its management and the cables between hosts.

---

## 7. Send traffic

The traffic generator is built in: test ports on free switch ports send and count frames; nothing extra to install on the hosts but python3.

1. **Lab → Traffic Generator** (or the Traffic tab of the lab).
2. **＋ Test Port**: *Where: Switch port* - the switch and a free port (it is like a tester's port cabled there) - or *Host network card* for a spare card of a host cabled to a **real** switch. Give it an address (`10.10.0.1/24`, or `dhcp`), and a gateway for routed streams.
3. **＋ L2 Stream / ＋ L3 Stream** - or **Quick Tests…**, which builds a stream or a test from a few questions: latency, jitter, loss under load, RFC 2544 throughput, bad packets, fuzzing, VLAN isolation, MTU, 802.1X clients, DHCP, VRRP and BFD failover, **TCP sessions** and a **blocked TCP port** (ACL).
4. A stream has its type (UDP, TCP SYN, ICMP, ARP, raw, custom frame, pcap replay or **TCP Session**), its size, rate (frames a second, Mbit/s or % of line rate), how long, VLAN, priority, DSCP and headers (QinQ, MPLS, GRE, VXLAN), and **what makes it pass**: loss and latency at most so much, or *should be dropped*.
5. **Start All**. The table shows each stream's frames sent and received, loss, latency and jitter live, with graphs; *Statistics* has every test port's counters; then **PASS** or **FAIL** with the reason.
6. **Report ▾** saves the run: **HTML**, **PDF** or an **Excel workbook** - the topology with the streams, the graphs, each stream's settings and the first frames sent and received, decoded.
7. **Engines…** chooses what sends the frames: the built-in engine (about 100,000 frames a second per host) or the **fast** one (a compiled helper Termwright copies to the hosts: about 420,000 frames a second of 64 bytes, 2.5 Gbit/s of 1518-byte frames on two vCPUs), and **TRex** servers for line rate on real hardware. An emulated switch forwards a few thousand frames a second, so test functions - forwarding, filtering, marking, learning, failover - in the lab, and speed on hardware.

---

## 8. A complete test: VLAN forwarding, an ACL and a failover

This example uses three switches of any image that supports VLANs and ACLs. The CLI lines depend on the operating system; the steps do not.

**The topology.** New Topology → three switches named `core-1`, `access-1`, `access-2`, cabling **Line** in that order (access-1 - core-1 - access-2), plus a second cable between access-1 and access-2 for the failover (Add Link). Start them and wait until all three are *running*.

**The configuration.** Open each switch's console (double-click) and:

1. Create VLAN 10 and put the inter-switch ports in it, tagged.
2. On access-1 and access-2, put port 5 in VLAN 10, untagged.
3. Turn on a loop-prevention protocol (spanning tree) on all three, so the second cable is a standby path.

**The test ports.** ＋ Test Port on **access-1 port 5**, address `10.10.0.1/24`; ＋ Test Port on **access-2 port 5**, address `10.10.0.2/24`.

**The streams.**

| Stream | From → to | Type | Rate and time | Passes when |
|---|---|---|---|---|
| `vlan10 a→b` | Test port 1 → Test port 2 | UDP, 512 bytes | 500 frames/s, 60 s | loss ≤ 0.5 % |
| `vlan10 b→a` | Test port 2 → Test port 1 | UDP, 512 bytes | 500 frames/s, 60 s | loss ≤ 0.5 % |
| `web` | Test port 1 → Test port 2 | TCP Session, port 80 | 200 sessions, 20 at once | failed ≤ 1 % |
| `ssh blocked` | Test port 1 → Test port 2 | TCP Session, port 22 | 20 sessions | should be refused (reset) - after the ACL below |

**Run 1 - forwarding.** Start the two UDP streams and `web`. All three should PASS: the frames cross the VLAN through core-1, the sessions connect.

**Run 2 - the ACL.** On access-2, add an ACL on port 5 that rejects TCP to port 22 (with a reset) and allows the rest. Start `ssh blocked`: it passes when every session is refused; `web` still connects.

**Run 3 - failover.** Start the UDP streams again and, about 20 seconds in, right-click the cable access-1 - core-1 → **Pull**. Spanning tree moves the traffic to the standby cable; the loss of each stream shows how long the outage was (frames lost ÷ 500 = seconds). For a measured outage, use a **Convergence** test instead (*Tests* → ＋ Test → Convergence: the stream, the cable to pull and when) - it reports the outage in milliseconds. **Plug In** the cable afterwards.

**The report.** Report ▾ → **PDF Report…** keeps the run with the topology, graphs and every stream's result.

---

## 9. The same with the AI agent

The AI assistant can do all of the above from plain requests, asking before it changes anything as its permissions say.

**Set it up once.**

1. **Options → AI Assistant**: choose the AI service (for example Claude Code, an API key, or a local model) and the model.
2. Open the AI panel (the sparkles button at the top right). In the box, choose the **Agent** mode, and under it the permissions - **Changes ask first** is recommended: read-only commands run by themselves, every change waits for you.
3. The tools under the wrench: keep **Emulated lab** ticked.

**Then ask, one step at a time, in the order of this guide.** For example:

1. *Check my KVM host kvm-1 and tell me what it needs to run switches.* - then *Set it up.* (you approve the commands; the sudo password is asked by Termwright, not the AI).
2. *Which images do I have? Download the public Alpine and FRR appliances and copy them to kvm-1.*
3. *Install the ISO in ~/Downloads/myos.iso as an image called myos, 8 GB disk, by hand - show me the installer's console.*
4. *Make a topology "vlan-lab" on kvm-1: core-1, access-1 and access-2 from image X in a line, plus a cable between access-1 and access-2. Start them and wait until they are ready.*
5. *On all three, create VLAN 10 tagged on the links and put port 5 of access-1 and access-2 in VLAN 10 untagged; turn on spanning tree.* - the agent shows each switch's commands and types them on the consoles after you approve.
6. *Put test ports on access-1 port 5 (10.10.0.1/24) and access-2 port 5 (10.10.0.2/24), run 500 pps UDP both ways for 60 seconds and tell me whether it passes.*
7. *Pull the cable between access-1 and core-1 20 seconds into the test and tell me how long the outage was.*
8. *Save the report as PDF.*

The agent works through the same tools as the screens - the topology, consoles, cables, test ports, streams and reports appear on the canvas and the Traffic page as it goes, and each step is in the chat (and in *Changes*, from where it can be reverted).

**From other AI apps and scripts.** *Help → Connect an AI App…* adds Termwright as an MCP connector to Claude Desktop, Claude Code, Cursor, VS Code and others: the same lab tools (`lab_add_switches`, `lab_power`, `lab_wait`, `lab_cable`, `lab_console`, `lab_traffic`, `lab_test` …) from those apps. The local API and the Python client do the same from scripts - see [API](API.md) and [Python](PYTHON.md).

---

## 10. When something does not work

| What you see | What to do |
|---|---|
| The host says *no /dev/kvm* | Turn on virtualization in the BIOS, or nested virtualization for a VM host (1.1); *Set Up* loads the module. Without it, switches run very slowly. |
| *Set Up* stops at sudo | Give the sudo password when asked, or allow password-less sudo for your user on the host. |
| A switch stays *booting* | Open its console (double-click) and the Events pane: the image may wait for an answer the image's *First boot* settings do not give, or need more memory. |
| No management address | Check the management choice (4.3); in a VM host, bridged needs the hypervisor's switch to allow other MAC addresses - or use *routed*. |
| A cable between hosts carries nothing | Restart both switches once; open UDP between the hosts in their firewalls. |
| Traffic loss at low rates | The switch is the limit: an emulated switch forwards a few thousand frames a second. Lower the rate, or give the switch more vCPUs. |
| The disk fills up | *Hosts → Images* shows what is on each host; remove images and topologies you no longer use. |

The Events pane, a switch's console and *Copy Log* in Set Up show what Termwright did and what the host answered - include them when you ask for help.
