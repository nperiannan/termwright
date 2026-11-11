import { Component, ElementRef, HostListener, OnDestroy, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { LabService } from '../core/lab.service';
import { Backend } from '../core/backend';
import { EmuDeviceType, EmuTopoInfo, LabDevice, LabLag, LabLink, LabModel } from '../core/models';
import { TabView, Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { LabConfigure, LabHosts, LabImages, LabNew } from './lab-pages';
import { TerminalView } from './terminal-view';

const BW = 104, BH = 46;
type Pal = { key: string; name: string; sub: string; group: string; ver: string; os: string; cls: string; dt?: EmuDeviceType; m?: LabModel };

/** The lab as a workspace tab: device palette, canvas with cables, the
 *  selected switch, the topology summary and the Events log. */
@Component({
  selector: 'app-lab-workspace',
  imports: [FormsModule, LabConfigure, LabHosts, LabImages, LabNew, TerminalView],
  template: `
  <div class="ws" (click)="menu.set(null); topos.set(null)" [style.grid-template-rows]="'auto minmax(0, 1fr) ' + dockH() + 'px'">
    <div class="tb">
      <b class="lname">{{ emu() ? tr('Emulated Lab') : tr('Simulated Lab') }}</b>
      @if (emu()) { <span class="topo" (click)="openTopos(); $event.stopPropagation()" [title]="tr('Topologies: save, open, new')"><span class="muted">{{ tr('Topology') }}</span> <b>{{ st()?.topology || tr('Untitled') }}</b> ▾</span>
        @if (topos(); as tl) {
          <div class="topos" (click)="$event.stopPropagation()">
            <div class="row2"><b>{{ tr('Topologies') }}</b><span class="sp"></span>
              <button type="button" class="sm" (click)="saveTopo()" [disabled]="!devs().length">{{ st()?.topology ? tr('Save As…') : tr('Save…') }}</button>
              <button type="button" class="sm" (click)="newTopo()">{{ tr('New Topology…') }}</button></div>
            @for (t of tl; track t.name) {
              <div class="ti" [class.cur]="t.open"><div class="tn"><b>{{ t.name }}</b><small class="muted">{{ tr('{0} switches', t.switches) }}@if (t.hosts?.length) { · {{ hostLabel(t.hosts![0]) }} } · {{ when(t.saved) }}</small></div>
                @if (t.open) { <span class="muted small">{{ tr('open') }}</span> }
                @else { <button type="button" class="sm" (click)="openTopo(t.name, false)">{{ tr('Open') }}</button><button type="button" class="sm primary" (click)="openTopo(t.name, true)" [disabled]="!t.switches">▶ {{ tr('Open and Start All') }}</button>
                  <button type="button" class="sm x" (click)="delTopo(t.name)" [title]="tr('Delete')">✕</button> }
              </div>
            } @empty { <p class="muted small">{{ tr('No saved topologies yet: Save… keeps this lab under a name.') }}</p> }
          </div>
        }
        <span class="chip" [title]="tr('KVM host')"><i class="d" [class.ok]="!!st()?.host"></i>{{ hostName() || tr('No host yet') }}@if (st()?.kvm) { · KVM }</span> }
      @else { <span class="chip" [title]="tr('The device pack the simulated switches come from')">{{ st()?.pack ? st()!.pack!.name + ' ' + st()!.pack!.version : tr('No device pack') }}</span>
        <button type="button" (click)="loadPack()">{{ st()?.pack ? tr('Load Another Pack…') : tr('Load Device Pack (.vm)…') }}</button> }
      <span class="sep"></span>
      <button type="button" class="primary" (click)="lab.start([])" [disabled]="!devs().length || lab.busy()">▶ {{ tr('Start All') }}</button>
      <button type="button" (click)="lab.stop([])" [disabled]="!running() || lab.busy()">■ {{ tr('Stop All') }}</button>
      @if (emu()) { <button type="button" (click)="sel() && lab.restart([sel()])" [disabled]="!selDev()?.running || lab.busy()" [title]="tr('Restart the selected switch')">⟳ {{ tr('Reload') }}</button> }
      <span class="sep"></span>
      <button type="button" [class.on]="linkMode()" (click)="toggleLink(); $event.stopPropagation()" [disabled]="devs().length < 2">⟷ {{ tr('Add Link') }}</button>
      @if (emu() && links().length) { <button type="button" (click)="lab.checkCabling()" [disabled]="lab.busy() || !running()" [title]="tr('Asks the running switches for their LLDP neighbours and marks each cable: as drawn, or not')">{{ tr('Check Cabling') }}</button> }
      <button type="button" (click)="consoleAll()" [disabled]="!running()">⌨ {{ emu() ? tr('Console All') : tr('Open All') }}</button>
      <span class="sep"></span>
      <button type="button" (click)="wizard()">＋ {{ devs().length ? tr('Add Switches') : tr('New Lab') }}</button>
      @if (emu()) {
        <button type="button" (click)="page.set('hosts')">{{ tr('Hosts') }}</button>
        <button type="button" (click)="page.set('images')">{{ tr('Images') }}</button>
        <button type="button" (click)="page.set('templates')">{{ tr('Templates') }}</button>
      } @else { <button type="button" (click)="dialog('list')">{{ tr('Switch List and Outputs') }}</button> }
      <span class="sp"></span>
      @if (emu() && devs().length) {
        <label class="chip fs" [title]="tr('On: Stop saves each running switch’s state into its disk, and Start resumes it in seconds (no boot, no day 0). A change to its persona, management, memory or image makes it boot again.')"><input type="checkbox" [checked]="!!st()?.fast" (change)="lab.setFast($any($event.target).checked)" [disabled]="lab.busy()" />{{ tr('Fast start') }}</label>
        <span class="chip mg" [title]="tr('How the switches’ first port (management) is connected; it applies when they start')">{{ tr('Management') }}
          @if (multi()) { <select class="hsel" [ngModel]="mg().host" (ngModelChange)="mgHost.set($event)" name="wmh" [title]="tr('Each host has its own management')">@for (u of hostUse(); track u.host) { <option [value]="u.host">{{ u.name }}</option> }</select> }
          <select [ngModel]="mg().mgmt" (ngModelChange)="setMgmt($event)" name="wmg" [disabled]="lab.busy()">
            <option value="cloud">{{ tr('The host’s network, routed (cloud)') }}</option>
            <option value="lan">{{ tr('The host’s network (bridged)') }}</option>
            <option value="nat">{{ tr('The host’s NAT network') }}</option>
            <option value="bridge">{{ tr('Lab bridge on the host') }}</option>
            <option value="forward">{{ tr('Port forwarding on the host') }}</option>
          </select>@if (mg().mgmt === 'cloud' || mg().mgmt === 'lan') { <button type="button" class="sm" (click)="openCloud(mg().mgmt); $event.stopPropagation()" [title]="tr('Addresses of the switches on the host’s network')">{{ mg().mgmt === 'lan' && !mg().lanFixed ? 'DHCP' : mg().mgmtNet.split('/')[0] }}…</button> }
          @if (cloudF(); as f) {
            <div class="cloudf" (click)="$event.stopPropagation()">
              @if (f.mode === 'cloud') {
                <b>{{ tr('The host’s network, routed (cloud)') }}</b>
                <p class="muted small">{{ tr('Like GNS3’s cloud: the switches get fixed addresses on the host’s network {0} and can reach and be reached from it (SSH, HTTP/HTTPS, FTP, TFTP, SCP…). The host answers for their addresses and forwards for them, so only the host’s own MAC address is seen there: nothing has to be allowed on a VMware or Hyper-V virtual switch, and Wi-Fi works.', f.net) }}</p>
              } @else {
                <b>{{ tr('The host’s network (bridged)') }}</b>
                <p class="muted small">{{ tr('The switches join the host’s network {0} as devices of their own, like GNS3’s cloud node: reached straight from it. A host that is itself a VM needs Promiscuous mode, MAC address changes and Forged transmits allowed on its port group.', f.net) }}</p>
                <div class="seg"><label><input type="radio" name="cfm" [value]="false" [(ngModel)]="f.fixed" /> {{ tr('From a DHCP server on the network') }}</label><label><input type="radio" name="cfm" [value]="true" [(ngModel)]="f.fixed" /> {{ tr('Fixed addresses (no DHCP there)') }}</label></div>
              }
              @if (f.mode === 'cloud' || f.fixed) {
                <label>{{ tr('First switch’s address') }}</label><input class="mono" [(ngModel)]="f.addr" name="cfa" placeholder="10.0.0.146" />
                <label>{{ tr('Gateway') }}</label><input class="mono" [(ngModel)]="f.gw" name="cfg" />
                <p class="muted small">{{ tr('The next switches take the addresses after it ({0} switches: up to {1}). Pick free addresses: no DHCP server may hand them out.', devs().length, lastAddr(f.addr)) }}</p>
              }
              <div class="row2"><span class="sp"></span><button type="button" (click)="cloudF.set(null); revertMgmt()">{{ tr('Cancel') }}</button><button type="button" class="primary" (click)="applyCloud()" [disabled]="(f.mode === 'cloud' || f.fixed) && !f.addr.trim()">{{ tr('Use') }}</button></div>
            </div>
          }</span>
      }
      <span class="chip"><span class="g">{{ tr('{0} running', counts().on) }}</span>@if (counts().boot) { · <span class="w">{{ tr('{0} booting', counts().boot) }}</span> }@if (counts().warn) { · <span class="w">{{ tr('{0} with a problem', counts().warn) }}</span> }@if (counts().bad) { · <span class="r">{{ tr('{0} failed', counts().bad) }}</span> } · {{ tr('{0} stopped', counts().off) }}</span>
    </div>

    <div class="main">
      @if (page() === 'new') { <app-lab-new [emu]="emu()" [preset]="preset()" (closed)="page.set(''); ghost.set(null)" (changed)="ghost.set($event)" (hostsPage)="page.set('hosts')" (made)="ui.refreshTree()" /> }
      <div class="pal">
        <input class="srch" [(ngModel)]="q" name="pq" [placeholder]="tr('Search devices')" spellcheck="false" />
        @for (g of groups(); track g.name) {
          <div class="grp"><span>{{ g.name }}</span><span>{{ g.ver }}</span></div>
          @for (p of g.items; track p.key) {
            <div class="dev" draggable="true" (dragstart)="dragPal($event, p)" (dblclick)="addAt(p, null)" [title]="tr('Drag onto the canvas, or double-click to add one')">
              <span class="ico" [class]="'ico ' + p.cls"></span><div><span>{{ p.name }}</span>@if (p.sub) { <small>{{ p.sub }}</small> }</div></div>
          }
        } @empty {
          @if (emu()) { <p class="muted small">{{ tr('No images yet: Images → Add Image.') }}</p> }
          @else { <p class="muted small">{{ tr('Load a device pack first.') }}</p><button type="button" class="primary" (click)="loadPack()">{{ tr('Load Device Pack (.vm)…') }}</button> }
        }
        <div class="hint">{{ tr('Drag a device onto the canvas to add it.') }}</div>
      </div>

      <div class="cv" #cv (dragover)="$event.preventDefault()" (drop)="dropPal($event, cv)" (pointermove)="move($event, cv)" (pointerup)="up()" (pointerleave)="up()"
           (contextmenu)="$event.preventDefault()">
        @if (linkMode()) { <div class="bar2">⟷ {{ linkText() }} <span class="k2">Esc</span> <a (click)="cancelLink(); $event.stopPropagation()">{{ tr('Cancel') }}</a></div> }
        @if (!devs().length && !ghosts().length) {
          <div class="empty"><b>{{ emu() ? tr('No switches yet') : tr('No simulated switches yet') }}</b><span>{{ tr('Drag a device from the left, or') }} <a (click)="wizard()">{{ tr('make a lab') }}</a>.</span></div>
        }
        <div class="inner" [style.width.px]="area().w * zoom()" [style.height.px]="area().h * zoom()">
          <div class="scale" [style.transform]="'scale(' + zoom() + ')'" [style.width.px]="area().w" [style.height.px]="area().h">
            @for (z of zones(); track z.host) {
              <div class="zone" [style.left.px]="z.x" [style.top.px]="z.y" [style.width.px]="z.w" [style.height.px]="z.h" [style.border-color]="z.c" [style.background]="z.c + '0f'">
                <div class="zh"><i [style.background]="z.c"></i><b>{{ z.name }}</b><span class="muted">{{ z.text }}</span></div></div>
            }
            <svg class="links" [attr.width]="area().w" [attr.height]="area().h">
              @for (k of lines(); track k.key) {
                <g class="lk" [class.tun]="k.tun" [class.down]="k.down" [class.warn]="k.warn" [class.selk]="selLink() === k.key" (click)="pickLink(k.link, $event)" (contextmenu)="linkMenu(k.link, $event)">
                  <title>{{ k.tip }}</title>
                  @for (m of k.paths; track $index) { <path class="hit" [attr.d]="m.d" /><path class="ln" [attr.d]="m.d" [class.up]="m.up" [class.dn]="m.down" [class.thick]="m.thick" /> }
                  @for (r of k.rings; track $index) { <circle class="ring" [attr.cx]="r.x" [attr.cy]="r.y" r="11" /> }
                  @for (lb of k.labels; track $index) { <rect class="pb" [class.lb]="lb.lag" [attr.x]="lb.x - lb.w / 2" [attr.y]="lb.y - 9" [attr.width]="lb.w" height="18" rx="5" /><text class="pt" [attr.x]="lb.x" [attr.y]="lb.y + 4">{{ lb.t }}</text> }
                </g>
              }
              @for (g of ghostLines(); track $index) { <line class="gl" [attr.x1]="g.x1" [attr.y1]="g.y1" [attr.x2]="g.x2" [attr.y2]="g.y2" /> }
              @if (rubber(); as r) { <line class="rb" [attr.x1]="r.x1" [attr.y1]="r.y1" [attr.x2]="r.x2" [attr.y2]="r.y2" /> }
            </svg>
            @for (g of ghosts(); track g.name) { <div class="node ghost" [style.left.px]="g.x - 75" [style.top.px]="g.y - BH / 2"><div class="nbox"><span class="ico"></span></div><div class="nn">{{ g.name }}</div><div class="nm">{{ tr('new') }}</div></div> }
            @for (d of devs(); track d.id) {
              <div class="node" [class.sel]="sel() === d.id" [class.pick]="linkA()?.id === d.id" [style.left.px]="pos(d).x - 75" [style.top.px]="pos(d).y - BH / 2"
                   (pointerdown)="grab($event, d, cv)" (click)="clickNode(d, $event)" (dblclick)="open(d)" (contextmenu)="nodeMenu(d, $event)" (mouseenter)="hov.set(d.id)" (mouseleave)="hov.set('')">
                <div class="nbox" [class.dim]="stateOf(d) === 'off'"><span class="ico" [class]="'ico ' + icoOf(d)"></span><i class="st" [class]="'st ' + stateOf(d)" [title]="tr(d.stateText || (d.running ? 'running' : 'stopped'))"></i>@if (warnOf(d)) { <i class="wn" [title]="warnOf(d)">!</i> }</div>
                <div class="nn">{{ d.hostname }}</div>
                <div class="nm">{{ d.persona?.slots?.[0]?.model || d.model }} · {{ subOf(d) }}</div>
                @if (d.personaError) { <span class="badge r" [title]="d.personaError">{{ tr('no persona') }}</span> }
                @else if (d.persona && d.personaOk) { <span class="badge">{{ tr('persona') }} ✓</span> }
                @else if (d.persona && d.personaInfo?.startsWith('written')) { <span class="badge" [title]="tr('The file is in its disk; the switch has not shown the model on its CLI yet')">{{ tr('persona') }} ?</span> }
                @else if (d.error) { <span class="badge r" [title]="d.error">{{ tr('problem') }}</span> }
              </div>
            }
            @if (hovDev(); as h) {
              <div class="ntip" [style.left.px]="pos(h).x + 82" [style.top.px]="pos(h).y - 34">
                <div class="th"><b>{{ h.hostname }}</b> <span [class]="stateOf(h)">● {{ tr(h.stateText || (h.running ? 'running' : 'stopped')) }}</span> <span class="muted">· {{ h.persona?.slots?.[0]?.model || h.model }}</span></div>
                <table>
                  @if (emu()) { <tr><td>{{ tr('Host') }}</td><td>{{ hname(hostOfDev(h)) }}@if (addrOfHost(hostOfDev(h)); as ha) { <span class="muted"> ({{ ha }})</span> }</td></tr>
                    <tr><td>{{ tr('Console') }}</td><td class="mono">telnet {{ hname(hostOfDev(h)) }}:{{ h.console }}</td></tr> }
                  <tr><td>{{ tr('Management') }}</td><td class="mono">{{ emu() ? (h.ssh || '-') : addrOf(h) + ':' + h.port }}</td></tr>
                  @if (emu() && h.running) { <tr><td>CPU</td><td>{{ (h.cpu ?? 0).toFixed(0) }} % {{ tr('of {0} vCPU', h.cpus ?? 1) }}</td></tr>
                    <tr><td>{{ tr('Memory') }}</td><td>{{ h.memUsed ? gb(h.memUsed) + ' / ' : '' }}{{ gb(h.ram) }}</td></tr> }
                  <tr><td>{{ tr('Serial') }} · MAC</td><td class="mono">{{ h.serial || '-' }} · {{ h.mac || '-' }}</td></tr>
                  <tr><td>{{ tr('Cables') }}</td><td>{{ cablesOf(h.id) }}</td></tr>
                </table>
              </div>
            }
          </div>
        </div>
        @if (portPick(); as pp) {
          <div class="ports" [style.left.px]="pp.x" [style.top.px]="pp.y" (click)="$event.stopPropagation()">
            <div class="ph"><b>{{ nameOf(pp.id) }}</b> · {{ tr('pick a port') }}</div>
            <div class="face">@for (p of pp.all; track p.name) { <button type="button" class="pt" [class.used]="!!p.peer" [disabled]="!!p.peer" [title]="p.peer ? p.name + ' → ' + p.peer : tr('free')" (click)="portChosen(p.name)">{{ p.short }}</button> }</div>
            <div class="lg"><span><i></i>{{ tr('free') }}</span><span><i class="u"></i>{{ tr('cabled (hover: to where)') }}</span><span class="sp"></span><button type="button" class="sm" (click)="portChosen(pp.ports[0])" [disabled]="!pp.ports.length">{{ tr('Next Free Port') }}</button></div>
          </div>
        }
        @if (menu(); as m) {
          <div class="menu" [style.left.px]="m.x" [style.top.px]="m.y" (click)="$event.stopPropagation()">
            @if (m.dev; as d) {
              <a (click)="mdo('start', d)" [class.off]="d.running">▶ {{ tr('Start') }}</a>
              <a (click)="mdo('stop', d)" [class.off]="!d.running" [title]="emu() && st()?.fast ? tr('Its running state is saved: it starts again in seconds') : ''">■ {{ emu() && st()?.fast ? tr('Stop and Save State') : tr('Stop') }}</a>
              @if (emu() && st()?.fast) { <a (click)="mdo('shutdown', d)" [class.off]="!d.running && !d.saved" [title]="tr('No saved state: it boots next time')">⏻ {{ tr('Shut Down') }}</a> }
              @if (emu()) { <a (click)="mdo('restart', d)" [class.off]="!d.running">⟳ {{ tr('Reload') }}</a> }
              <hr />
              @if (emu()) { <a (click)="mdo('console', d)" [class.off]="!d.running">{{ tr('Console') }} <kbd>telnet :{{ d.console }}</kbd></a> }
              <a (click)="mdo('ssh', d)" [class.off]="!d.running">{{ emu() ? 'SSH' : tr('Open Session') }} <kbd>{{ emu() ? d.ssh : '127.0.0.1:' + d.port }}</kbd></a>
              <hr />
              <a (click)="mdo('configure', d)">{{ tr('Configure…') }}</a>
              @if (emu()) { <a (click)="mdo('lags', d)" [class.off]="!d.running">{{ tr('Read Its LAGs') }}</a> }
              <a (click)="mdo('link', d)">{{ tr('Add Link from Here') }}</a>
              @if (emu()) { <a (click)="mdo('reset', d)">{{ tr('Back to Day 0') }}</a> }
              @if (emu() && otherHosts(d).length) { <hr /><span class="mh2">{{ tr('Move to Host') }}</span>@for (o of otherHosts(d); track o.session) { <a (click)="moveTo(d, o.session)" [class.off]="d.running" [title]="d.running ? tr('Stop it first') : tr('It starts there from its image (day 0); its cables to this host become UDP tunnels')">→ {{ o.name || hname(o.session) }}</a> } }
              <hr />
              <a class="r" (click)="mdo('remove', d)">{{ tr('Delete') }}</a>
            }
            @if (m.link; as k) {
              @if (emu()) { <a (click)="ldo('pull', k)">{{ k.down ? tr('Plug In') : tr('Pull') }}</a> }
              @if (emu() && pairOf(k); as pr) {
                <hr />
                <a (click)="lag(pr, 'lacp', 'both')">{{ tr('LAG the {0} cables to {1}: LACP on both', pr.n, nameOf(pr.b)) }}</a>
                <a (click)="lag(pr, 'static', 'both')">{{ tr('… static on both') }}</a>
                <a (click)="lag(pr, 'lacp', 'a')">{{ tr('… LAG on {0} only', nameOf(pr.a)) }}</a>
                <a (click)="lag(pr, 'lacp', 'b')">{{ tr('… LAG on {0} only', nameOf(pr.b)) }}</a>
                @if (k.lagA || k.lagB) { <a (click)="lag(pr, '', 'both')">{{ tr('Remove the LAG') }}</a> }
                @if (k.lagA?.source === 'user' || k.lagB?.source === 'user') { <a (click)="applyLag(k, false)" [title]="tr('Types the LAG on the switches’ consoles - you see the commands first')">{{ tr('Set the LAG on the Switches…') }}</a> }
                @if (k.lagA?.source === 'switch' || k.lagB?.source === 'switch') { <a (click)="applyLag(k, true)">{{ tr('Remove the LAG from the Switches…') }}</a> }
                <hr />
              }
              <a class="r" (click)="ldo('remove', k)">{{ tr('Remove the Cable') }}</a>
            }
          </div>
        }
        <div class="zoom"><button type="button" class="sm" (click)="zoomBy(-0.1)">−</button><button type="button" class="sm" (click)="zoom.set(1)">{{ (zoom() * 100).toFixed(0) }} %</button><button type="button" class="sm" (click)="zoomBy(0.1)">+</button><button type="button" class="sm" (click)="fit(cv)">{{ tr('Fit') }}</button></div>
      </div>

      <div class="side">
        @if (selDev(); as d) {
          <h3>{{ d.hostname }}</h3>
          <div class="card"><div class="kv">
            <span>{{ tr('Device') }}</span><b>{{ d.persona?.slots?.[0]?.model || d.model }}@if (d.version) { · {{ d.version }} }</b>
            <span>{{ tr('State') }}</span><span [class]="warnOf(d) ? 'w' : stateOf(d)">{{ tr(d.stateText || (d.running ? 'running' : 'stopped')) }}</span>
            @if (d.persona) { <span>{{ tr('Persona') }}</span><span [class.r]="!!d.personaError" [class.g]="!d.personaError && d.personaOk">{{ d.personaError || (d.personaOk ? '✓ ' + tr('in the switch') : d.personaInfo ? tr(d.personaInfo) : tr('not written yet')) }}</span> }
            @if (d.personaInfo) { <span></span><span class="mono xs">{{ d.personaInfo }}</span> }
            <span>{{ tr('Serial') }}</span><span class="mono">{{ d.serial || '-' }}</span>
            <span>MAC</span><span class="mono">{{ d.mac || '-' }}</span>
            <span>{{ tr('Management') }}</span><span class="mono">{{ emu() ? d.ssh : d.mgmtIp }}</span>
            @if (emu()) { <span>{{ tr('Console') }}</span><span class="mono">telnet :{{ d.console }}</span>
              <span>{{ tr('Resources') }}</span><span>{{ d.cpus }} vCPU · {{ gb(d.ram) }}@if (d.diskGb) { · {{ d.diskGb }} GB }@if (d.running && d.cpu !== undefined) { · CPU {{ d.cpu!.toFixed(0) }} % }</span> }
            @if (d.error) { <span>{{ tr('Problem') }}</span><span class="r">{{ d.error }}</span> }
          </div>
          <div class="btns">@if (emu()) { <button type="button" class="primary sm" (click)="console(d)" [disabled]="!d.running">{{ tr('Console') }}</button> }
            <button type="button" class="sm" (click)="ssh(d)" [disabled]="!d.running">{{ emu() ? 'SSH' : tr('Open Session') }}</button><button type="button" class="sm" (click)="configure(d)">{{ tr('Configure…') }}</button></div></div>
        }
        <h3>{{ tr('Topology summary') }}</h3>
        <div class="card tbl"><table>
          @for (d of devs(); track d.id) { <tr [class.on]="sel() === d.id" (click)="select(d)"><td><i class="d" [class]="'d ' + stateOf(d)"></i>{{ d.hostname }}</td><td class="mono">{{ emu() ? ':' + d.console : ':' + d.port }}</td><td class="mono">{{ addrOf(d) }}</td></tr> }
          @empty { <tr><td class="muted">{{ tr('No switches yet') }}</td></tr> }
        </table></div>
        @if (emu() && multi()) {
          @for (u of hostUse(); track u.host) {
            <h3><i class="hd" [style.background]="hcol(u.host)"></i>{{ tr('Host') }} · {{ u.name }}@if (u.main) { <span class="muted small"> · {{ tr('the lab’s host') }}</span> }</h3>
            <div class="card">
              <div class="mr"><span>{{ tr('{0} switches · {1} running', u.switches, u.running) }}</span><span class="muted">{{ mgName(u) }}</span></div>
              <div class="mr"><span>vCPU {{ u.vcpus }} / {{ u.cpus || '?' }}</span><span class="muted">{{ tr('load') }} {{ u.load }}</span></div><div class="meter"><i [style.width.%]="pct(u.vcpus, u.cpus)" [style.background]="hcol(u.host)"></i></div>
              <div class="mr"><span>RAM {{ gb(u.ram) }} / {{ gb(u.memTotal) }}</span><span class="muted">{{ tr('{0} free', gb(u.memFree)) }}</span></div><div class="meter"><i [style.width.%]="pct(u.memTotal - u.memFree, u.memTotal)" [style.background]="hcol(u.host)"></i></div>
            </div>
          }
          @if (tunnels(); as n) { <div class="card"><div class="mr"><b>{{ tr('Cables between hosts') }}</b><span class="muted">{{ n }}</span></div><div class="muted small">{{ tr('UDP tunnels: the hosts must reach each other on UDP 20000-29999') }}</div></div> }
        } @else if (emu() && st()?.usage; as u) {
          <h3>{{ tr('Host') }} · {{ hostName() }}</h3>
          <div class="card">
            <div class="mr"><span>vCPU {{ u.vcpus }} / {{ u.hostCpus || '?' }}</span><span class="muted">{{ tr('load') }} {{ u.load }}</span></div><div class="meter"><i [style.width.%]="pct(u.vcpus, u.hostCpus)"></i></div>
            <div class="mr"><span>RAM {{ gb(u.ram) }} / {{ gb(u.hostMemTotal) }}</span><span class="muted">{{ tr('{0} free', gb(u.hostMemFree)) }}</span></div><div class="meter"><i [style.width.%]="pct(u.hostMemTotal - u.hostMemFree, u.hostMemTotal)"></i></div>
          </div>
        }
      </div>
    </div>

    @switch (page()) {
      @case ('configure') { <app-lab-configure [devId]="cfgId()" [emu]="emu()" (closed)="page.set('')" (images)="page.set('images')" /> }
      @case ('images') { <app-lab-images start="images" (closed)="page.set('')" /> }
      @case ('templates') { <app-lab-images start="templates" (closed)="page.set('')" /> }
      @case ('hosts') { <app-lab-hosts (closed)="page.set('')" /> }
    }
    <div class="dock">
      <div class="grip" (pointerdown)="gripDown($event)" [title]="tr('Drag to make the pane taller or shorter')"></div>
      <div class="dtabs">
        <a [class.on]="dock() === 'events'" (click)="dock.set('events')">{{ tr('Events') }}@if (problems()) { <span class="cnt">{{ problems() }}</span> }</a>
        @if (emu()) { <a [class.on]="dock() === 'inv'" (click)="dock.set('inv')">{{ tr('Inventory') }}</a> }
        @if (emu() && selDev(); as d) { <a [class.on]="dock() === 'console'" (click)="dock.set('console'); loadTail()">{{ tr('{0} console', d.hostname) }}</a> }
        <span class="sp"></span><span class="muted small hint2">{{ dock() === 'inv' ? tr('Every switch under its host: console, management, what it uses now') : dock() === 'events' ? tr('Everything Termwright does to a switch shows here') : (dockTab() ? tr('Live console - day-0 and checks wait while it is connected') : tr('The end of its console; click it to type')) }}</span>
        @if (dock() === 'console' && dockTab(); as t) { <button type="button" class="sm" (click)="undock()" [title]="tr('Close the connection to its console')">{{ tr('Disconnect') }}</button> }
        @else { <button type="button" class="sm" (click)="copyDock()" [title]="tr('Copy to the clipboard')">{{ tr('Copy') }}</button> }
        @if (dock() === 'events' && emu()) { <button type="button" class="sm" (click)="clearEvents()">{{ tr('Clear') }}</button> }
        <button type="button" class="sm" (click)="dockH.set(dockH() > 260 ? 190 : 420)" [title]="tr('Taller or shorter')">{{ dockH() > 260 ? '▾' : '▴' }}</button>
      </div>
      @if (dock() === 'inv') {
        <div class="inv"><table>
          <tr><th>{{ tr('Switch') }}</th><th>{{ tr('State') }}</th><th>{{ tr('Console') }}</th><th>{{ tr('Management') }}</th><th>CPU</th><th>{{ tr('Memory') }}</th></tr>
          @for (g of inventory(); track g.host) {
            <tr class="h"><td colspan="4">@if (multi()) { <i class="hd" [style.background]="hcol(g.id)"></i> }<b>{{ g.host }}</b>@if (g.addr) { <span class="muted"> · {{ g.addr }}</span> }<span class="muted"> · {{ g.mgmt }} · {{ tr('{0} switches · {1} running', g.devs.length, g.running) }}</span></td>
              <td><span class="mtr"><i [style.width.%]="g.cpuPct"></i></span>{{ g.cpuText }}</td><td><span class="mtr"><i [style.width.%]="g.memPct"></i></span>{{ g.memText }}</td></tr>
            @for (d of g.devs; track d.id) {
              <tr [class.on]="sel() === d.id" (click)="select(d)"><td>{{ d.hostname }}</td><td [class]="stateOf(d)">● {{ tr(d.stateText || (d.running ? 'running' : 'stopped')) }}</td>
                <td class="mono">{{ g.host }}:{{ d.console }}</td><td class="mono">{{ d.ssh || '-' }}</td>
                <td>@if (d.running) { <span class="mtr"><i [style.width.%]="d.cpu ?? 0"></i></span>{{ (d.cpu ?? 0).toFixed(0) }} % }</td>
                <td>@if (d.running) { <span class="mtr"><i [style.width.%]="pct(d.memUsed ?? 0, d.ram ?? 0)"></i></span>{{ d.memUsed ? gb(d.memUsed) + ' / ' : '' }}{{ gb(d.ram) }} }</td></tr>
            }
          }
        </table></div>
      } @else if (dock() === 'events') {
        <div class="ev">
          @for (e of events(); track $index) { <div [class]="e.level"><span class="t">{{ time(e.at) }}</span><b (click)="selectId(e.vm)">{{ e.name }}</b><span>{{ e.text }}</span></div> }
          @empty { <div><span class="t"></span><b></b><span class="muted">{{ emu() ? tr('Nothing yet - start a switch.') : tr('Simulated switches start at once; their state is on the canvas.') }}</span></div> }
        </div>
      } @else if (dockTab(); as t) {
        <div class="dterm"><app-terminal-view [tab]="t" [visible]="visible() && dock() === 'console'" /></div>
      } @else {
        <div class="tailw" (click)="typeHere()" [title]="tr('Click to connect and type')"><pre class="tail">{{ tail() || tr('No console output yet.') }}</pre>
          @if (conTab(); as ct) { <div class="typ"><span class="muted small">{{ tr('Its console is open in a tab') }}</span> <button type="button" class="sm" (click)="tabs.activeId.set(ct.id); $event.stopPropagation()">{{ tr('Go to the Tab') }}</button></div> }
          @else if (selDev()?.running) { <button type="button" class="primary sm typ" (click)="typeHere(); $event.stopPropagation()">⌨ {{ tr('Type here') }}</button> }</div>
      }
    </div>
  </div>`,
  styles: [`
    :host { display: block; height: 100%; }
    .ws { height: 100%; display: grid; background: var(--bg); color: var(--text); font-size: 13px; }
    .tb { position: relative; display: flex; flex-wrap: wrap; align-items: center; gap: 6px; padding: 8px 12px; border-bottom: 1px solid var(--line); background: var(--surface); white-space: nowrap;
      button { height: 30px; white-space: nowrap; } button.on { background: var(--accent-soft); border-color: var(--accent); color: var(--accent); } }
      .lname { margin-right: 4px; font-size: 14px; } .sep { width: 1px; height: 22px; background: var(--line); margin: 0 4px; flex: none; } .sp { flex: 1; }
    .chip { display: inline-flex; align-items: center; gap: 6px; height: 30px; padding: 0 10px; border-radius: 8px; background: var(--surface-2); border: 1px solid var(--line); white-space: nowrap; }
    .chip.mg select { height: 24px; font-size: 12px; }
    .g, .on { color: var(--success, #1fa45a); } .w, .boot { color: var(--warning, #e39b17); } .r, .bad { color: var(--danger); }
    i.d { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: var(--muted); margin-right: 6px; flex: none; }
    i.d.ok, i.d.on { background: var(--success, #1fa45a); } i.d.boot { background: var(--warning, #e39b17); } i.d.bad, i.d.off { background: var(--danger); }
    .main { display: grid; grid-template-columns: 230px 1fr 300px; min-height: 0; }
    .pal { border-right: 1px solid var(--line); background: var(--surface); padding: 10px; overflow: auto; }
    .srch { width: 100%; height: 30px; margin-bottom: 6px; }
    .grp { font-size: 11px; font-weight: 600; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; margin: 12px 2px 6px; display: flex; justify-content: space-between; }
    .dev { display: flex; align-items: center; gap: 9px; padding: 6px 7px; border-radius: 8px; border: 1px solid transparent; cursor: grab; user-select: none;
      &:hover { background: var(--surface-2); border-color: var(--line); } small { display: block; color: var(--muted); font-size: 11px; } }
    .ico { width: 34px; height: 22px; border-radius: 5px; background: linear-gradient(#3b4658, #2a3242); position: relative; flex: none; display: inline-block; }
    .ico::after { content: ""; position: absolute; left: 5px; right: 5px; top: 8px; height: 5px; background: repeating-linear-gradient(90deg, #8fd18f 0 3px, transparent 3px 5px); }
    .ico.v::after { background: repeating-linear-gradient(90deg, #8fb8ff 0 3px, transparent 3px 5px); }
    .ico.l { background: linear-gradient(#e2731f, #b9531a); } .ico.l::after { background: none; content: ">_"; color: #fff; font: 700 10px monospace; top: 4px; left: 8px; }
    .ico.s { background: linear-gradient(#6c5fd6, #4c41b0); }
    .hint { margin-top: 12px; padding: 9px; border: 1px dashed var(--line); border-radius: 8px; color: var(--muted); font-size: 12px; }
    .cv { position: relative; overflow: auto; background-color: var(--bg); background-image: radial-gradient(var(--line) 1.2px, transparent 1.2px); background-size: 18px 18px; }
    .inner { position: relative; } .scale { position: absolute; left: 0; top: 0; transform-origin: 0 0; }
    .empty { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; color: var(--muted); pointer-events: none;
      b { font-size: 15px; color: var(--text); } a { pointer-events: auto; color: var(--accent); cursor: pointer; } }
    svg.links { position: absolute; left: 0; top: 0; overflow: visible; }
    .lk { cursor: pointer; } .lk .hit { stroke: transparent; stroke-width: 12; } .lk .ln { stroke: var(--muted); stroke-width: 2.2; stroke-dasharray: 2 5; } .lk .ln.up { stroke: var(--success, #1fa45a); stroke-dasharray: none; }
    .lk.down .ln { stroke: var(--danger); stroke-dasharray: 6 5; } .lk.selk .ln { stroke-width: 3.5; }
    .topo { display: inline-flex; gap: 6px; align-items: center; padding: 4px 10px; border: 1px solid var(--accent); border-radius: 8px; cursor: pointer; background: var(--accent-soft); white-space: nowrap; max-width: 260px; overflow: hidden; text-overflow: ellipsis; }
    .topos { position: absolute; top: 46px; left: 12px; z-index: 30; width: 520px; max-height: 60vh; overflow: auto; padding: 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); box-shadow: 0 10px 30px rgba(0,0,0,.35);
      .row2 { display: flex; gap: 8px; align-items: center; margin-bottom: 10px; } .sp { flex: 1; }
      .ti { display: flex; gap: 6px; align-items: center; padding: 8px 10px; border: 1px solid var(--line); border-radius: 8px; margin-bottom: 6px; &.cur { border-color: var(--accent); }
        .tn { flex: 1; min-width: 0; display: flex; flex-direction: column; } .x { padding: 2px 7px; } } }
    .chip.fs { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; margin-right: 6px; }
    .chip.fs input { margin: 0; }
    .zone { position: absolute; border: 1.5px dashed; border-radius: 14px; pointer-events: none; }
    .zone .zh { display: flex; gap: 7px; align-items: center; padding: 6px 10px; font-size: 11.5px; white-space: nowrap; }
    .zone .zh i, .hd { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 6px; }
    .zone .zh i { margin: 0; }
    .lk.tun .ln:not(.dn) { stroke: #a46cf0; stroke-dasharray: 7 5; }
    .hsel { margin-right: 4px; }
    .menu .mh2 { display: block; padding: 4px 12px 2px; font-size: 11px; color: var(--muted); }
    .ntip { position: absolute; z-index: 9; min-width: 250px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); box-shadow: 0 10px 30px rgba(0,0,0,.35); pointer-events: none; font-size: 12px;
      .th { margin-bottom: 6px; white-space: nowrap; } table { border-collapse: collapse; } td { padding: 2px 10px 2px 0; white-space: nowrap; } td:first-child { color: var(--muted); } }
    .inv { overflow: auto; padding: 0 14px 10px; table { width: 100%; border-collapse: collapse; } th, td { text-align: left; padding: 5px 10px; border-bottom: 1px solid var(--line); font-size: 12px; white-space: nowrap; }
      th { color: var(--muted); font-weight: 500; position: sticky; top: 0; background: var(--surface); } tr.h td { background: var(--surface-2); } tr.on td { background: var(--accent-soft); } tr:not(.h) { cursor: pointer; }
      .mtr { display: inline-block; width: 60px; height: 5px; border-radius: 3px; background: var(--line); vertical-align: middle; margin-right: 6px; i { display: block; height: 100%; border-radius: 3px; background: var(--accent); } } }
    .lk .ln { fill: none; } .lk .hit { fill: none; } .lk .ln.dn { stroke: var(--danger); stroke-dasharray: 6 5; }
    .lk .ln.thick { stroke-width: 7; stroke-linecap: round; } .lk.selk .ln.thick { stroke-width: 9; }
    .lk.warn .ln.thick { stroke: var(--warning, #e39b17); }
    .lk .ring { fill: var(--bg); stroke: var(--text); stroke-width: 2; } .lk.warn .ring { stroke: var(--warning, #e39b17); }
    .pb.lb { stroke: var(--accent); } .lk.warn .pb.lb { stroke: var(--warning, #e39b17); }
    .pb { fill: var(--surface); stroke: var(--line); } .pt { font: 600 10px var(--mono, monospace); fill: var(--muted); text-anchor: middle; }
    .lk.down .pb { stroke: var(--danger); } .lk.down .pt { fill: var(--danger); }
    .gl { stroke: var(--accent); stroke-width: 1.6; stroke-dasharray: 5 5; opacity: .7; } .node.ghost { opacity: .55; pointer-events: none; .nbox { border-style: dashed; border-color: var(--accent); } }
    .main { position: relative; }
    .rb { stroke: var(--accent); stroke-width: 2.5; stroke-dasharray: 8 5; }
    .node { position: absolute; width: 150px; text-align: center; cursor: pointer; user-select: none; }
    .nbox { margin: 0 auto; width: ${BW}px; height: ${BH}px; border-radius: 10px; background: var(--surface); border: 1.5px solid var(--line); box-shadow: 0 2px 6px rgba(0,0,0,.06); display: flex; align-items: center; justify-content: center; position: relative;
      .ico { width: 64px; height: 26px; } &.dim { opacity: .6; } }
    .node.sel .nbox { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); } .node.pick .nbox { border-color: var(--accent); border-style: dashed; }
    i.st { position: absolute; top: -5px; right: -5px; width: 13px; height: 13px; border-radius: 50%; border: 2px solid var(--surface); background: var(--muted); }
    i.st.on { background: var(--success, #1fa45a); } i.st.boot { background: var(--warning, #e39b17); } i.st.bad, i.st.off { background: var(--danger); }
    .chip.mg { position: relative; }
    .cloudf { position: absolute; top: calc(100% + 6px); right: 0; z-index: 30; width: 380px; padding: 14px; border: 1px solid var(--border); border-radius: 10px; background: var(--surface); box-shadow: 0 10px 30px rgba(0,0,0,.35); display: grid; grid-template-columns: auto 1fr; gap: 8px 10px; align-items: center; text-align: left; white-space: normal;
      b, p, .row2, .seg { grid-column: 1 / -1; margin: 0; } .seg { display: flex; flex-direction: column; gap: 4px; label { display: flex; gap: 6px; align-items: center; } input { width: auto; } } input { width: 100%; } .row2 { display: flex; gap: 8px; } .sp { flex: 1; } }
    i.wn { position: absolute; top: -6px; left: -6px; width: 15px; height: 15px; border-radius: 50%; border: 2px solid var(--surface); background: var(--warning, #e39b17); color: #1d2330; font: 800 10px/11px var(--sans, sans-serif); font-style: normal; text-align: center; cursor: help; }
    .nn { font-weight: 600; margin-top: 5px; } .nm { font-size: 11px; color: var(--muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .badge { display: inline-block; margin-top: 3px; font-size: 10.5px; padding: 1px 7px; border-radius: 9px; background: var(--surface-2); border: 1px solid var(--line); }
    .badge.r { color: var(--danger); border-color: var(--danger); }
    .bar2 { position: sticky; top: 10px; margin: 10px auto 0; width: max-content; z-index: 6; display: flex; align-items: center; gap: 10px; padding: 7px 12px; border-radius: 10px; background: var(--accent); color: #fff; font-weight: 500;
      .k2 { background: rgba(255,255,255,.2); border-radius: 6px; padding: 1px 7px; font-size: 12px; } a { color: #fff; text-decoration: underline; cursor: pointer; } }
    .face { display: grid; grid-template-columns: repeat(auto-fill, minmax(30px, 1fr)); gap: 3px; padding: 8px; border-radius: 9px; background: linear-gradient(#323b4b, #262d3a); max-height: 200px; overflow: auto; }
    .pt { height: 22px; min-width: 0; padding: 0; border: 0; border-radius: 3px; background: #55607a; color: #e8ecf5; font: 600 9.5px var(--mono, monospace); cursor: pointer; &:hover:not(:disabled) { background: #fff; color: #1d2330; } &.used { background: #1fa45a; cursor: not-allowed; opacity: 1; } }
    .lg { display: flex; align-items: center; gap: 12px; margin-top: 8px; font-size: 11.5px; color: var(--muted); i { display: inline-block; width: 10px; height: 10px; border-radius: 2px; background: #55607a; margin-right: 4px; vertical-align: -1px; } i.u { background: #1fa45a; } }
    .ports { position: absolute; z-index: 7; width: 420px; background: var(--surface); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 10px 30px rgba(0,0,0,.16); padding: 10px;
      .ph { margin-bottom: 8px; } .pl { display: flex; flex-wrap: wrap; gap: 4px; max-height: 180px; overflow: auto; button { min-width: 44px; font-family: var(--mono, monospace); } } }
    .menu { position: absolute; z-index: 8; width: 260px; background: var(--surface); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 10px 30px rgba(0,0,0,.16); padding: 5px;
      a { display: flex; justify-content: space-between; padding: 6px 10px; border-radius: 6px; cursor: pointer; &:hover { background: var(--accent-soft); } &.off { opacity: .45; pointer-events: none; } }
      hr { border: 0; border-top: 1px solid var(--line); margin: 4px 0; } kbd { color: var(--muted); font: 11px var(--mono, monospace); max-width: 120px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } }
    .zoom { position: sticky; float: right; right: 12px; bottom: 12px; top: calc(100% - 44px); display: flex; gap: 4px; margin: -44px 12px 0 0; z-index: 5; }
    .side { border-left: 1px solid var(--line); background: var(--surface); padding: 12px; overflow: auto;
      h3 { margin: 4px 0 8px; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; color: var(--muted); } }
    .card { border: 1px solid var(--line); border-radius: 10px; padding: 10px; margin-bottom: 12px; background: var(--surface); }
    .kv { display: grid; grid-template-columns: 88px minmax(0, 1fr); gap: 4px 8px; font-size: 12px; > span:nth-child(odd) { color: var(--muted); } span { overflow-wrap: anywhere; } }
    .xs { font-size: 11px; color: var(--muted); } .btns { display: flex; gap: 6px; margin-top: 10px; flex-wrap: wrap; }
    .tbl { padding: 4px 8px; } table { width: 100%; border-collapse: collapse; font-size: 12px; } td { padding: 4px 2px; border-bottom: 1px solid var(--line); cursor: pointer; } tr:last-child td { border: 0; } tr.on td { color: var(--accent); }
    .mr { display: flex; justify-content: space-between; font-size: 12px; } .meter { height: 6px; border-radius: 4px; background: var(--surface-2); overflow: hidden; margin: 4px 0 8px; i { display: block; height: 100%; background: var(--accent); } }
    .dock { border-top: 1px solid var(--line); background: var(--surface); display: grid; grid-template-rows: 5px 34px minmax(0, 1fr); min-height: 0; }
    .grip { cursor: ns-resize; background: transparent; &:hover { background: var(--accent-soft); } }
    .dtabs button { margin-left: 4px; } .hint2 { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .ev div { user-select: text; } .tail { user-select: text; }
    .dtabs { display: flex; align-items: center; gap: 2px; padding: 0 10px; border-bottom: 1px solid var(--line);
      a { padding: 6px 12px; color: var(--muted); font-weight: 500; cursor: pointer; } a.on { color: var(--text); box-shadow: inset 0 -2px var(--accent); }
      .cnt { margin-left: 6px; background: var(--danger); color: #fff; border-radius: 9px; padding: 0 6px; font-size: 11px; } }
    .ev { padding: 6px 14px; overflow: auto; div { display: grid; grid-template-columns: 64px 120px 1fr; gap: 10px; padding: 2px 0; font-size: 12px; } .t { color: var(--muted); } b { cursor: pointer; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
      .ok span:last-child { color: var(--success, #1fa45a); } .error span:last-child { color: var(--danger); } .warn span:last-child { color: var(--warning, #e39b17); } }
    .tail { margin: 0; padding: 8px 14px; overflow: auto; font: 11.5px/1.45 var(--mono, monospace); white-space: pre-wrap; }
    .tailw { position: relative; min-height: 0; overflow: hidden; display: flex; cursor: text; .tail { flex: 1; } .typ { position: absolute; right: 14px; bottom: 10px; } }
    .dterm { position: relative; min-height: 0; overflow: hidden; padding: 4px 6px; display: flex; > * { flex: 1; min-width: 0; } }
  `],
})
export class LabWorkspace implements OnDestroy {
  readonly tab = input.required<TabView>();
  readonly visible = input(true);
  protected lab = inject(LabService);
  private backend = inject(Backend);
  protected tabs = inject(Tabs);
  protected ui = inject(Ui);
  private host = inject<ElementRef<HTMLElement>>(ElementRef);
  protected tr = tr;
  protected BH = BH;
  protected q = '';
  protected emu = computed(() => this.tab().labKind === 'emu');
  protected st = computed(() => (this.emu() ? this.lab.emu() : this.lab.sim()));
  protected devs = computed(() => this.st()?.devices ?? []);
  protected links = computed(() => this.st()?.links ?? []);
  protected sel = signal('');
  protected selDev = computed(() => this.devs().find(d => d.id === this.sel()) ?? null);
  protected selLink = signal('');
  protected menu = signal<{ x: number; y: number; dev?: LabDevice; link?: LabLink } | null>(null);
  protected dock = signal<'events' | 'console' | 'inv'>('events');
  protected hov = signal('');
  protected topos = signal<EmuTopoInfo[] | null>(null);
  async openTopos() { if (this.topos()) { this.topos.set(null); return; } try { this.topos.set(await this.backend.emuTopologies()); } catch (e) { this.ui.error(e); } }
  private async reTopos() { try { this.topos.set(await this.backend.emuTopologies()); } catch { /* ignore */ } }
  async saveTopo() { const n = await this.ui.input(tr('Save Topology'), tr('Name'), this.st()?.topology || ''); if (n && await this.lab.saveTopology(n)) await this.reTopos(); }
  async newTopo() {
    const n = await this.ui.input(tr('New Topology'), tr('Name')); if (!n) return;
    if (this.running() && !(await this.ui.confirm(tr('New Topology'), tr('The running switches of this topology are stopped first; it stays saved.'), tr('Stop and Continue')))) return;
    if (await this.lab.newTopology(n)) { this.topos.set(null); void this.ui.refreshTree(); }
  }
  async openTopo(name: string, start: boolean) {
    if (this.running() && !(await this.ui.confirm(tr('Open {0}', name), tr('The running switches of this topology are stopped first; it stays saved.'), tr('Stop and Open')))) return;
    if (await this.lab.openTopology(name, start)) { this.topos.set(null); void this.ui.refreshTree(); }
  }
  async delTopo(name: string) { if (await this.ui.confirm(tr('Delete {0}', name), tr('Forget this saved topology? Its sessions stay in the Session Manager.'), tr('Delete'), true)) { try { await this.backend.emuDeleteTopology(name); } catch (e) { this.ui.error(e); } await this.reTopos(); } }
  hostLabel(p: string) { return p.split('/').pop() ?? p; }
  when(iso: string) { const d = new Date(iso); return isNaN(+d) ? '' : d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }); }
  protected hovDev = computed(() => this.devs().find(d => d.id === this.hov()) ?? null);
  cablesOf(id: string) {
    const ls = this.links().filter(k => k.a === id || k.b === id), lag = ls.filter(k => (k.a === id ? k.lagA : k.lagB)).length;
    return ls.length + (lag ? ' (' + tr('{0} in a LAG', lag) + ')' : '');
  }
  /** The switches under their host, with what the host and each switch use now. */
  protected inventory = computed(() => {
    const v = this.st(), u = v?.usage, devs = this.devs();
    if (this.multi()) return this.hostUse().map(h => {
      const ds = devs.filter(d => this.hostOfDev(d) === h.host), used = h.memTotal - h.memFree;
      return { id: h.host, host: h.name, addr: (h.addr || '').split('/')[0], mgmt: this.mgName(h), devs: ds, running: ds.filter(d => d.running).length,
        cpuPct: this.pct(h.cores, h.cpus), cpuText: h.cores.toFixed(1) + ' / ' + (h.cpus || '?') + ' ' + tr('cores'), memPct: this.pct(used, h.memTotal), memText: this.gb(used) + ' / ' + this.gb(h.memTotal) };
    });
    const used = (u?.hostMemTotal ?? 0) - (u?.hostMemFree ?? 0);
    return [{ id: v?.host ?? '', host: this.hostName(), addr: v?.hostInfo?.lanAddr?.split('/')[0] ?? '', mgmt: this.mgName({ mgmt: v?.mgmt ?? '', mgmtNet: v?.mgmtNet ?? '', lanFixed: v?.lanFixed }), devs, running: devs.filter(d => d.running).length,
      cpuPct: this.pct(u?.cores ?? 0, u?.hostCpus ?? 0), cpuText: (u?.cores ?? 0).toFixed(1) + ' / ' + (u?.hostCpus ?? '?') + ' ' + tr('cores'),
      memPct: this.pct(used, u?.hostMemTotal ?? 0), memText: this.gb(used) + ' / ' + this.gb(u?.hostMemTotal) }];
  });
  /** How a host's switches are managed, in a few words. */
  mgName(h: { mgmt: string; mgmtNet: string; lanFixed?: boolean }) {
    const a = (h.mgmtNet ?? '').split('/')[0];
    const mg: Record<string, string> = { lan: h.lanFixed ? tr('bridged, fixed {0}+', a) : tr('bridged, DHCP'), cloud: tr('routed, {0}+', a), nat: tr('NAT network'), bridge: tr('lab bridge'), forward: tr('port forwarding') };
    return mg[h.mgmt] ?? '';
  }
  protected hostUse = computed(() => this.st()?.hostUse ?? []);
  protected multi = computed(() => this.hostUse().length > 1);
  hostOfDev(d: LabDevice) { return d.host || this.st()?.host || ''; }
  hname(h: string) { return h.split('/').pop() ?? h; }
  addrOfHost(h: string) { const u = this.hostUse().find(x => x.host === h); return (u?.addr || (h === this.st()?.host ? this.st()?.hostInfo?.lanAddr ?? '' : '')).split('/')[0]; }
  hcol(h: string) { const i = this.hostUse().findIndex(u => u.host === h); return ['#3b8cf6', '#a46cf0', '#1fb5a5', '#e3842a', '#d65db1', '#8fb339'][Math.max(0, i) % 6]; }
  /** Hosts the switch could move to: the checked hosts without problems. */
  otherHosts(d: LabDevice) { return (this.st()?.hosts ?? []).filter(h => h.session !== this.hostOfDev(d) && !(h.info.problems ?? []).length); }
  async moveTo(d: LabDevice, host: string) {
    if (d.running) return;
    this.menu.set(null);
    if (await this.ui.confirm(tr('Move {0} to {1}', d.hostname, this.hname(host)), tr('It starts on that host from its image (day 0, its saved configuration is not taken along) with that host’s management. Its cables to switches on other hosts become UDP tunnels.'), tr('Move'))) await this.lab.moveVM(d.id, host);
  }
  protected tunnels = computed(() => this.lines().filter(k => k.tun).length);
  /** Each host as an area round its switches (only when the lab is on more than one host). */
  protected zones = computed(() => {
    if (!this.multi()) return [];
    this.moved();
    const out: { host: string; name: string; text: string; c: string; x: number; y: number; w: number; h: number }[] = [];
    for (const u of this.hostUse()) {
      const ps = this.devs().filter(d => this.hostOfDev(d) === u.host).map(d => this.pos(d));
      if (!ps.length) continue;
      const x0 = Math.min(...ps.map(p => p.x)) - 105, y0 = Math.min(...ps.map(p => p.y)) - 66, x1 = Math.max(...ps.map(p => p.x)) + 105, y1 = Math.max(...ps.map(p => p.y)) + 62;
      const text = [this.addrOfHost(u.host), this.mgName(u), u.vcpus + ' / ' + (u.cpus || '?') + ' vCPU', this.gb(u.ram) + ' / ' + this.gb(u.memTotal)].filter(Boolean).join(' · ');
      out.push({ host: u.host, name: u.name, text, c: this.hcol(u.host), x: Math.max(4, x0), y: Math.max(4, y0), w: x1 - Math.max(4, x0), h: y1 - Math.max(4, y0) });
    }
    return out;
  });
  /** The host whose management the chip shows ('' = the lab's own). */
  protected mgHost = signal('');
  protected mg = computed(() => {
    const v = this.st(), main = v?.host ?? '', h = this.mgHost() && this.hostUse().some(u => u.host === this.mgHost()) ? this.mgHost() : main;
    const u = this.hostUse().find(x => x.host === h), info = h === main ? v?.hostInfo : (v?.hosts ?? []).find(x => x.session === h)?.info;
    return { host: h, main: h === main, mgmt: u?.mgmt ?? v?.mgmt ?? '', mgmtNet: u?.mgmtNet ?? v?.mgmtNet ?? '', cloudGw: u?.cloudGw ?? v?.cloudGw ?? '', lanFixed: u ? !!u.lanFixed : !!v?.lanFixed, lanAddr: info?.lanAddr ?? '', gateway: info?.gateway ?? '' };
  });
  protected tail = signal('');
  protected zoom = signal(1);
  protected linkMode = signal(false);
  protected linkA = signal<{ id: string; port: string } | null>(null);
  protected portPick = signal<{ id: string; ports: string[]; all: { name: string; short: string; peer: string }[]; x: number; y: number; side: 'a' | 'b' } | null>(null);
  protected page = signal<'' | 'configure' | 'images' | 'templates' | 'hosts' | 'new'>('');
  protected cfgId = signal('');
  protected dockH = signal(this.savedDockH());
  private savedDockH() { try { return Math.min(700, Math.max(90, +(localStorage.getItem('tw.lab.dockH') ?? '') || 190)); } catch { return 190; } }
  gripDown(ev: PointerEvent) {
    ev.preventDefault();
    const y0 = ev.clientY, h0 = this.dockH();
    const mv = (e: PointerEvent) => this.dockH.set(Math.min(Math.round(window.innerHeight * 0.75), Math.max(90, h0 + (y0 - e.clientY))));
    const up = () => { window.removeEventListener('pointermove', mv); window.removeEventListener('pointerup', up); try { localStorage.setItem('tw.lab.dockH', String(this.dockH())); } catch { /* not kept */ } };
    window.addEventListener('pointermove', mv); window.addEventListener('pointerup', up);
  }
  async copyDock() {
    const text = this.dock() === 'inv' ? ['host,switch,state,console,management,cpu %,memory MB,ram MB', ...this.inventory().flatMap(g => g.devs.map(d => [g.host, d.hostname, d.state || (d.running ? 'running' : 'stopped'), g.host + ':' + d.console, d.ssh ?? '', (d.cpu ?? 0).toFixed(0), d.memUsed ?? '', d.ram ?? ''].join(',')))].join('\n') : this.dock() === 'events' ? [...(this.st()?.events ?? [])].map(e => this.time(e.at) + '  ' + e.name + '  ' + e.level.toUpperCase() + '  ' + e.text).join('\n') : this.tail();
    try { await navigator.clipboard.writeText(text); this.ui.toast(tr('Copied'), 'success'); } catch { this.ui.error(tr('Could not copy')); }
  }
  async clearEvents() { try { this.lab.emu.set(await this.backend.emuClearEvents()); } catch { /* ignore */ } }
  protected preset = signal('');
  protected ghost = signal<{ names: string[]; cable: string } | null>(null);
  protected ghosts = computed(() => {
    const g = this.ghost(); if (!g) return [];
    let maxY = 0; for (const d of this.devs()) maxY = Math.max(maxY, this.pos(d).y);
    const y0 = this.devs().length ? maxY + 150 : 100;
    const spine = g.cable === 'leaf-spine' && !this.devs().length;
    return g.names.map((name, i) => {
      if (spine && i < 2) return { name, x: 300 + i * 260, y: y0 };
      const k = spine ? i - 2 : i, per = 6;
      return { name, x: 120 + (k % per) * 170, y: y0 + (spine ? 150 : 0) + Math.floor(k / per) * 150 };
    });
  });
  protected ghostLines = computed(() => {
    const g = this.ghost(), gs = this.ghosts(); if (!g || !gs.length) return [];
    const out: { x1: number; y1: number; x2: number; y2: number }[] = [];
    const j = (a: { x: number; y: number }, b: { x: number; y: number }) => out.push({ x1: a.x, y1: a.y, x2: b.x, y2: b.y });
    const old = this.devs();
    if (g.cable === 'leaf-spine') {
      const cores = old.length >= 2 ? [this.pos(old[0]), this.pos(old[1])] : gs.slice(0, 2);
      if (!old.length && gs.length > 1) j(gs[0], gs[1]);
      for (const l of old.length >= 2 ? gs : gs.slice(2)) for (const c of cores) j(c, l);
    } else if (g.cable === 'chain' || g.cable === 'ring') {
      const seq = [...(old.length ? [this.pos(old[old.length - 1])] : []), ...gs];
      for (let k = 0; k + 1 < seq.length; k++) j(seq[k], seq[k + 1]);
      if (g.cable === 'ring' && seq.length > 2) j(seq[seq.length - 1], seq[0]);
    }
    return out;
  });
  protected mouse = signal<{ x: number; y: number } | null>(null);
  private moved = signal(new Map<string, { x: number; y: number }>());
  private dragging: { id: string; dx: number; dy: number; moved: boolean } | null = null;
  private palDrag: Pal | null = null;
  private timer: ReturnType<typeof setInterval>;

  constructor() {
    effect(() => { if (this.visible()) { this.lab.kind.set(this.emu() ? 'emu' : 'sim'); this.lab.watch('ws-' + this.tab().id, true); void this.lab.refresh(); } else this.lab.watch('ws-' + this.tab().id, false); });
    this.timer = setInterval(() => { if (this.visible() && this.emu() && this.dock() === 'console') void this.loadTail(); }, 3000);
  }
  @HostListener('document:keydown.escape') onEsc() { if (!this.visible()) return; if (this.cloudF()) { this.cloudF.set(null); this.revertMgmt(); return; } if (this.page()) { this.page.set(''); this.ghost.set(null); } else { this.cancelLink(); this.menu.set(null); } }
  ngOnDestroy() { clearInterval(this.timer); this.lab.watch('ws-' + this.tab().id, false); for (const id of Object.values(this.docked())) void this.tabs.close(id); }

  protected hostName = computed(() => (this.st()?.host ?? '').split('/').pop() ?? '');
  protected running = computed(() => this.devs().filter(d => d.running).length);
  protected counts = computed(() => {
    const c = { on: 0, boot: 0, bad: 0, off: 0, warn: 0 };
    for (const d of this.devs()) { c[this.stateOf(d)]++; if (this.warnOf(d)) c.warn++; }
    return c;
  });
  protected events = computed(() => [...(this.st()?.events ?? [])].reverse());
  protected problems = computed(() => (this.st()?.events ?? []).filter(e => e.level === 'error').length);
  stateOf(d: LabDevice): 'on' | 'boot' | 'bad' | 'off' {
    if (!this.emu()) return d.running ? 'on' : 'off';
    // green: up (a problem shows as a "!" beside it), orange: on its way, red: down or failed
    if (d.state === 'failed') return 'bad';
    if (d.state === 'running') return 'on';
    if (d.state === 'stopped' || !d.state) return 'off';
    return 'boot';
  }
  /** The problem of a switch that is up ("" = none): it stays green, with a "!". */
  warnOf(d: LabDevice) { return this.emu() && d.running && d.state !== 'failed' ? (d.personaError || d.error || '') : ''; }
  subOf(d: LabDevice) {
    if (!this.emu()) return d.running ? d.mgmtIp.split('/')[0] : tr('stopped');
    return d.state === 'running' ? this.addrOf(d) : tr(d.stateText || 'stopped');
  }
  addrOf(d: LabDevice) { return this.emu() ? (d.mgmtIp ? d.mgmtIp.split('/')[0] : d.running ? 'DHCP…' : '-') : d.mgmtIp.split('/')[0]; }
  icoOf(d: LabDevice) { const o = (d.os || '').toLowerCase(); return !this.emu() ? 's' : /voss|fabric/.test(o) ? 'v' : /linux|ubuntu|vm/.test(o) && !/exos/.test(o) ? 'l' : ''; }
  nameOf(id: string) { return this.devs().find(d => d.id === id)?.hostname ?? id; }
  gb(mb?: number) { return !mb ? '-' : mb >= 1024 ? (mb / 1024).toFixed(mb % 1024 ? 1 : 0) + ' GB' : mb + ' MB'; }
  pct(a: number, b: number) { return b ? Math.min(100, (a / b) * 100) : 0; }
  time(at: string) { const t = new Date(at); return isNaN(+t) ? '' : t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }); }

  // palette
  protected groups = computed(() => {
    const items: Pal[] = [];
    if (this.emu()) {
      for (const dt of this.st()?.deviceTypes ?? []) {
        if (!dt.images?.length) continue;
        const im = (this.st()?.images ?? []).find(i => i.id === dt.images[0]);
        items.push({ key: dt.key, name: dt.model || im?.name || dt.kind, sub: dt.modules?.length ? (dt.modName || 'VIM') + ' ' + dt.modules.map(m => m.replace(/^\d+-VIM-/, '')).join(' / ') : (dt.model ? '' : im?.fileName ?? ''),
          group: dt.osName || dt.kind || tr('Other images'), ver: im?.version ?? '', os: dt.kind, cls: /voss|fabric/i.test(dt.kind + dt.osName + (im?.fileName ?? '') + (im?.deviceType ?? '')) ? 'v' : /linux|ubuntu|debian|centos|alpine|cloudimg/i.test(dt.kind + (im?.fileName ?? '') + (im?.deviceType ?? '')) ? 'l' : '', dt });
      }
    } else {
      for (const m of this.st()?.models ?? []) items.push({ key: m.sku, name: m.sku, sub: m.os.map(o => this.lab.osShort(o)).join(' / '), group: m.family, ver: '', os: m.os[0], cls: 's', m });
    }
    const q = this.q.trim().toLowerCase();
    const out: { name: string; ver: string; items: Pal[] }[] = [];
    for (const p of items.filter(p => !q || (p.name + ' ' + p.sub + ' ' + p.group).toLowerCase().includes(q))) {
      let g = out.find(x => x.name === p.group);
      if (!g) out.push(g = { name: p.group, ver: p.ver, items: [] });
      g.items.push(p);
    }
    return out;
  });
  dragPal(ev: DragEvent, p: Pal) { this.palDrag = p; ev.dataTransfer?.setData('text/plain', p.key); }
  dropPal(ev: DragEvent, cv: HTMLElement) {
    ev.preventDefault();
    const p = this.palDrag; this.palDrag = null;
    if (p) void this.addAt(p, this.toCanvas(ev, cv));
  }
  async addAt(p: Pal, at: { x: number; y: number } | null) {
    const before = new Set(this.devs().map(d => d.id));
    let ok = false;
    if (this.emu()) {
      const e = this.lab.emu();
      if (!this.devs().length) {
        // a new lab: straight away on the only host (or the lab's), else the New Lab panel with this device
        const good = (e?.hosts ?? []).filter(h => !(h.info.problems ?? []).length);
        const host = good.find(h => h.session === e?.host)?.session ?? (good.length === 1 ? good[0].session : '');
        if (!host) { this.preset.set(p.key); this.page.set('new'); return; }
        const s0 = await this.backend.emuSuggest(p.dt?.model || p.dt?.kind || '').catch(() => null);
        const n0 = (p.dt?.model || p.name).toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') + '-{n}';
        {
          const made = await this.lab.addEmu({ host, groups: [{ image: p.dt!.images[0], model: p.dt!.model, module: '', count: 1, names: n0, serialStart: s0?.serial ?? '', macStart: s0?.mac ?? '', cpus: p.dt!.cpus, ram: p.dt!.ram }], cabling: 'none', mgmt: '', mgmtNet: '', start: false });
          if (!made) return;
        }
        const fresh0 = this.devs().find(d => !before.has(d.id));
        if (fresh0) { const q = at ?? { x: 300, y: 200 }; this.moved.update(m => new Map(m).set(fresh0.id, q)); this.lab.place(fresh0.id, q.x, q.y); this.sel.set(fresh0.id); }
        await this.ui.refreshTree();
        return;
      }
      if (!e?.host) { this.preset.set(p.key); this.page.set('new'); return; }
      const s = await this.backend.emuSuggest(p.dt?.model || p.dt?.kind || '').catch(() => null);
      const names = (p.dt?.model || p.name).toLowerCase().replace(/[^a-z0-9]+/g, '-') + '-{n}';
      ok = await this.lab.addEmu({ host: e.host, groups: [{ image: p.dt!.images[0], model: p.dt!.model, module: '', count: 1, names, serialStart: s?.serial ?? '', macStart: s?.mac ?? '', cpus: p.dt!.cpus, ram: p.dt!.ram }], cabling: 'none', mgmt: '', mgmtNet: '', start: false });
    } else {
      if (!this.lab.sim()?.pack) { this.dialog('list'); return; }
      ok = await this.lab.add({ groups: [{ os: p.os, model: p.name, count: 1, names: '' }], mgmtNet: '', cabling: 'none', password: '', start: false });
    }
    if (!ok) return;
    const fresh = this.devs().find(d => !before.has(d.id));
    if (fresh) {
      const q = at ?? { x: 120 + (this.devs().length % 6) * 170, y: 100 + Math.floor(this.devs().length / 6) * 150 };
      this.moved.update(m => new Map(m).set(fresh.id, q)); this.lab.place(fresh.id, q.x, q.y); this.sel.set(fresh.id);
    }
    await this.ui.refreshTree();
  }

  // canvas
  pos(d: LabDevice) {
    const m = this.moved().get(d.id); if (m) return m;
    if (d.x || d.y) return { x: d.x + 75, y: d.y + BH / 2 };
    const i = this.devs().indexOf(d); return { x: 120 + (i % 6) * 170, y: 100 + Math.floor(i / 6) * 150 };
  }
  protected area = computed(() => {
    this.moved();
    let w = 900, h = 600;
    for (const d of this.devs()) { const p = this.pos(d); w = Math.max(w, p.x + 200); h = Math.max(h, p.y + 160); }
    return { w, h };
  });
  /**
   * The cables, drawn so that several between the same two switches are all
   * seen: side by side, fanning out from each switch. Cables in a LAG are one
   * thicker track with a ring at each switch that has the LAG; on a switch
   * without it they fan out to their own ports (one ring: a LAG on one side).
   */
  protected lines = computed(() => {
    this.moved();
    type Lk = LabLink & { key: string };
    const pairs = new Map<string, { p: LabDevice; q: LabDevice; ls: { k: Lk; pp: string; qp: string; lp: LabLag | null; lq: LabLag | null }[] }>();
    for (const k0 of this.links()) {
      const a = this.devs().find(d => d.id === k0.a), b = this.devs().find(d => d.id === k0.b);
      if (!a || !b) continue;
      const k = { ...k0, key: k0.a + '/' + k0.aPort };
      const flip = a.id > b.id, id = flip ? b.id + '|' + a.id : a.id + '|' + b.id;
      if (!pairs.has(id)) pairs.set(id, { p: flip ? b : a, q: flip ? a : b, ls: [] });
      pairs.get(id)!.ls.push(flip ? { k, pp: k.bPort, qp: k.aPort, lp: k.lagB ?? null, lq: k.lagA ?? null } : { k, pp: k.aPort, qp: k.bPort, lp: k.lagA ?? null, lq: k.lagB ?? null });
    }
    const out: { key: string; link: Lk; tip: string; tun: boolean; down: boolean; warn: boolean; paths: { d: string; up: boolean; down: boolean; thick: boolean }[]; rings: { x: number; y: number }[]; labels: { x: number; y: number; t: string; w: number; lag: boolean }[] }[] = [];
    const P = (v: { x: number; y: number }) => v.x.toFixed(1) + ' ' + v.y.toFixed(1);
    const lw = (t: string) => t.length * 6.2 + 10;
    for (const { p, q, ls } of pairs.values()) {
      const pa = this.pos(p), pb = this.pos(q), tun = this.multi() && this.hostOfDev(p) !== this.hostOfDev(q);
      const A = this.edge(pa, pb), B = this.edge(pb, pa);
      const L = Math.hypot(B.x - A.x, B.y - A.y) || 1, u = { x: (B.x - A.x) / L, y: (B.y - A.y) / L }, n = { x: -u.y, y: u.x };
      const at = (o: { x: number; y: number }, along: number, side: number) => ({ x: o.x + u.x * along + n.x * side, y: o.y + u.y * along + n.y * side });
      // tracks: a LAG (by its names at both ends) is one, every other cable its own
      const tracks: (typeof ls)[] = [];
      const byLag = new Map<string, typeof ls>();
      for (const l of ls) {
        if (!l.lp && !l.lq) { tracks.push([l]); continue; }
        const g = (l.lp?.name ?? '') + '|' + (l.lq?.name ?? '');
        if (!byLag.has(g)) { byLag.set(g, []); tracks.push(byLag.get(g)!); }
        byLag.get(g)!.push(l);
      }
      const fan = Math.min(56, L * 0.22), gap = 22;
      // a curve from s to e that leaves s and reaches e along the cable's direction, offset by side
      const curve = (s: { x: number; y: number }, e: { x: number; y: number }, s1: { x: number; y: number }, e1: { x: number; y: number }) =>
        'M' + P(s) + ' C' + P(at(s, fan * 0.55, 0)) + ' ' + P(at(s1, -fan * 0.45, 0)) + ' ' + P(s1) + ' L' + P(e1) + ' C' + P(at(e1, fan * 0.45, 0)) + ' ' + P(at(e, -fan * 0.55, 0)) + ' ' + P(e);
      tracks.forEach((tk, i) => {
        const o = (i - (tracks.length - 1) / 2) * gap;
        const first = tk[0], up = tk.every(l => !!l.k.up), down = tk.every(l => !!l.k.down);
        const item = { key: first.k.key, link: first.k, tip: '', tun, down, warn: false, paths: [] as { d: string; up: boolean; down: boolean; thick: boolean }[], rings: [] as { x: number; y: number }[], labels: [] as { x: number; y: number; t: string; w: number; lag: boolean }[] };
        const S = at(A, fan, o), E = at(B, -fan, o);
        if (tk.length === 1 && !first.lp && !first.lq) { // a single cable
          const l = first;
          item.paths.push({ d: o === 0 ? 'M' + P(A) + ' L' + P(B) : curve(A, B, S, E), up: !!l.k.up, down: !!l.k.down, thick: false });
          const lp = at(A, Math.min(L * 0.2, fan + 14), o), lq = at(B, -Math.min(L * 0.2, fan + 14), o);
          item.labels.push({ ...lp, t: l.pp, w: lw(l.pp), lag: false }, { ...lq, t: l.qp, w: lw(l.qp), lag: false });
          item.tip = p.hostname + ' ' + l.pp + ' ⇄ ' + q.hostname + ' ' + l.qp + (l.k.lldp ? ' · LLDP ' + l.k.lldp : '');
          item.warn = !!l.k.lldp && !l.k.lldp.startsWith('ok');
        } else { // a LAG: a thick track, a ring where it is set; single cables fan out where it is not
          const lp = first.lp, lq = first.lq;
          item.warn = !lp || !lq || (lp.mode !== lq.mode);
          const s0 = lp ? S : at(A, L * 0.42, o), e0 = lq ? E : at(B, -L * 0.42, o);
          item.paths.push({ d: 'M' + P(s0) + ' L' + P(e0), up, down, thick: true });
          const side = (end: 'p' | 'q') => {
            const o0 = end === 'p' ? A : B, ring = end === 'p' ? S : E, lag = end === 'p' ? lp : lq, dir = end === 'p' ? 1 : -1;
            if (lag) {
              item.rings.push(ring);
              tk.forEach((l, j) => { const st = at(o0, 0, (j - (tk.length - 1) / 2) * 5); item.paths.push({ d: 'M' + P(st) + ' L' + P(ring), up: !!l.k.up, down: !!l.k.down, thick: false }); });
              const ports = tk.map(l => end === 'p' ? l.pp : l.qp).join(',');
              const t = (lag.mode === 'static' ? 'static ' : 'LACP ') + ports;
              item.labels.push({ ...at(ring, dir * 8, (o >= 0 ? 1 : -1) * 22), t, w: lw(t), lag: true });
            } else { // single cables from the end of the track to their ports
              const from = end === 'p' ? s0 : e0;
              tk.forEach((l, j) => {
                const so = o + (j - (tk.length - 1) / 2) * gap * 0.8, mid = at(o0, dir * fan, so);
                item.paths.push({ d: end === 'p' ? 'M' + P(o0) + ' C' + P(at(o0, fan * 0.5, 0)) + ' ' + P(at(mid, -fan * 0.4, 0)) + ' ' + P(mid) + ' L' + P(from)
                  : 'M' + P(from) + ' L' + P(mid) + ' C' + P(at(mid, fan * 0.4, 0)) + ' ' + P(at(o0, -fan * 0.5, 0)) + ' ' + P(o0), up: !!l.k.up, down: !!l.k.down, thick: false });
                const t = end === 'p' ? l.pp : l.qp;
                item.labels.push({ ...at(o0, dir * Math.min(L * 0.2, fan + 14), so), t, w: lw(t), lag: false });
              });
            }
          };
          side('p'); side('q');
          const mid = at(A, L / 2, o), t = 'LAG ×' + tk.length;
          item.labels.push({ ...at(mid, 0, (o >= 0 ? 1 : -1) * 14), t, w: lw(t), lag: true });
          item.tip = (lp ? p.hostname + ' ' + lp.name + ' (' + lp.mode + ')' : p.hostname + ' no LAG') + ' ⇄ ' + (lq ? q.hostname + ' ' + lq.name + ' (' + lq.mode + ')' : q.hostname + ' no LAG') + ' · ' + tk.map(l => l.pp + '-' + l.qp).join(', ')
            + (!lp || !lq ? ' - ' + tr('the LAG is set on one side only') : lp.mode !== lq.mode ? ' - ' + tr('static on one side, LACP on the other') : '');
        }
        if (tun) item.tip = tr('UDP tunnel {0} ⇄ {1}', this.hname(this.hostOfDev(p)), this.hname(this.hostOfDev(q))) + ' · ' + item.tip;
        out.push(item);
      });
    }
    return out;
  });
  private edge(p: { x: number; y: number }, q: { x: number; y: number }) {
    const dx = q.x - p.x, dy = q.y - p.y; const s = Math.min(Math.abs((BW / 2 + 4) / (dx || 1e-6)), Math.abs((BH / 2 + 4) / (dy || 1e-6)));
    return { x: p.x + dx * Math.min(s, 1), y: p.y + dy * Math.min(s, 1) };
  }
  protected rubber = computed(() => {
    const a = this.linkA(), m = this.mouse(); if (!a || !m || this.portPick()) return null;
    const d = this.devs().find(x => x.id === a.id); if (!d) return null;
    const p = this.pos(d); return { x1: p.x, y1: p.y, x2: m.x, y2: m.y };
  });
  private toCanvas(ev: MouseEvent, cv: HTMLElement) {
    const r = cv.getBoundingClientRect();
    return { x: (ev.clientX - r.left + cv.scrollLeft) / this.zoom(), y: (ev.clientY - r.top + cv.scrollTop) / this.zoom() };
  }
  grab(ev: PointerEvent, d: LabDevice, cv: HTMLElement) {
    if (ev.button !== 0 || this.linkMode()) return;
    const p = this.toCanvas(ev, cv), q = this.pos(d);
    this.dragging = { id: d.id, dx: p.x - q.x, dy: p.y - q.y, moved: false };
  }
  move(ev: PointerEvent, cv: HTMLElement) {
    const p = this.toCanvas(ev, cv);
    if (this.linkMode()) this.mouse.set(p);
    const g = this.dragging; if (!g) return;
    g.moved = true;
    this.moved.update(m => new Map(m).set(g.id, { x: Math.max(80, p.x - g.dx), y: Math.max(40, p.y - g.dy) }));
  }
  up() {
    const g = this.dragging; this.dragging = null;
    if (g?.moved) { const p = this.moved().get(g.id)!; this.lab.place(g.id, p.x - 75, p.y - BH / 2); }
  }
  zoomBy(d: number) { this.zoom.set(Math.min(2, Math.max(0.4, +(this.zoom() + d).toFixed(1)))); }
  fit(cv: HTMLElement) { const a = this.area(); this.zoom.set(Math.max(0.4, Math.min(1.5, +Math.min(cv.clientWidth / a.w, cv.clientHeight / a.h).toFixed(2)))); }
  select(d: LabDevice) { this.sel.set(d.id); this.tail.set(''); if (this.dock() === 'console') void this.loadTail(); }
  selectId(id: string) { const d = this.devs().find(x => x.id === id); if (d) this.select(d); }
  clickNode(d: LabDevice, ev: MouseEvent) {
    ev.stopPropagation(); this.menu.set(null);
    if (this.linkMode()) { void this.linkClick(d, ev); return; }
    this.select(d);
  }
  open(d: LabDevice) { if (this.linkMode()) return; if (this.emu()) this.console(d); else this.ssh(d); }
  nodeMenu(d: LabDevice, ev: MouseEvent) { ev.preventDefault(); ev.stopPropagation(); this.select(d); this.menu.set({ ...this.menuAt(ev), dev: d }); }
  linkMenu(k: LabLink, ev: MouseEvent) { ev.preventDefault(); ev.stopPropagation(); this.selLink.set(k.a + '/' + k.aPort); this.menu.set({ ...this.menuAt(ev), link: k }); }
  pickLink(k: LabLink, ev: MouseEvent) { ev.stopPropagation(); this.selLink.set(k.a + '/' + k.aPort); }
  private menuAt(ev: MouseEvent) {
    const cv = (ev.target as HTMLElement).closest('.cv') as HTMLElement, r = cv.getBoundingClientRect();
    return { x: Math.min(ev.clientX - r.left + cv.scrollLeft, cv.scrollLeft + cv.clientWidth - 230), y: Math.min(ev.clientY - r.top + cv.scrollTop, cv.scrollTop + cv.clientHeight - 330) };
  }
  mdo(what: string, d: LabDevice) {
    this.menu.set(null);
    switch (what) {
      case 'start': void this.lab.start([d.id]); break;
      case 'stop': void this.lab.stop([d.id]); break;
      case 'shutdown': void this.lab.shutDown([d.id]); break;
      case 'restart': void this.lab.restart([d.id]); break;
      case 'console': this.console(d); break;
      case 'ssh': this.ssh(d); break;
      case 'configure': this.configure(d); break;
      case 'lags': void this.lab.readLags([d.id]); break;
      case 'link': this.startLinkFrom(d); break;
      case 'reset': void this.reset(d); break;
      case 'remove': void this.remove(d); break;
    }
  }
  /** The cables between the two switches of a cable, and their ports at each end. */
  pairOf(k: LabLink) {
    const ls = this.links().filter(x => (x.a === k.a && x.b === k.b) || (x.a === k.b && x.b === k.a));
    const aPorts = ls.map(x => x.a === k.a ? x.aPort : x.bPort), bPorts = ls.map(x => x.a === k.a ? x.bPort : x.aPort);
    const lagA = ls.map(x => x.a === k.a ? x.lagA : x.lagB).find(g => !!g)?.name ?? '', lagB = ls.map(x => x.a === k.a ? x.lagB : x.lagA).find(g => !!g)?.name ?? '';
    return ls.length > 1 ? { a: k.a, b: k.b, aPorts, bPorts, lagA, lagB, n: ls.length } : null;
  }
  /** A LAG of the cables between two switches on one end or both ('' mode: removed). */
  async lag(pr: { a: string; b: string; aPorts: string[]; bPorts: string[]; lagA: string; lagB: string }, mode: string, where: 'a' | 'b' | 'both') {
    this.menu.set(null);
    const ends = where === 'both' ? (['a', 'b'] as const) : [where];
    for (const e of ['a', 'b'] as const) {
      const vm = e === 'a' ? pr.a : pr.b, ports = e === 'a' ? pr.aPorts : pr.bPorts, name = e === 'a' ? pr.lagA : pr.lagB;
      if (mode && (ends as readonly string[]).includes(e)) await this.lab.setLag(vm, name, mode, ports);
      else if (name) await this.lab.setLag(vm, name, '', []); // not on this end (or removed)
    }
  }
  /** Types the LAG(s) of a cable's ends on the switches (or removes them there), after showing the commands. */
  async applyLag(k: LabLink, remove: boolean) {
    this.menu.set(null);
    const ends: [string, LabLag][] = [];
    for (const [vm, g] of [[k.a, k.lagA], [k.b, k.lagB]] as [string, LabLag | null | undefined][]) if (g && (remove ? g.source === 'switch' : g.source === 'user')) ends.push([vm, g]);
    const items: string[] = [], todo: [string, string][] = [];
    for (const [vm, g] of ends) {
      try { const c = await this.backend.emuLagCommands(vm, g.name, remove); items.push(this.nameOf(vm) + ': ' + c.join(' · ')); todo.push([vm, g.name]); }
      catch (e) { items.push(this.nameOf(vm) + ': ' + tr('cannot - {0}', String((e as Error)?.message ?? e))); }
    }
    if (!todo.length) { await this.ui.choose(remove ? tr('Remove the LAG from the Switches') : tr('Set the LAG on the Switches'), tr('Nothing can be typed:'), items, [{ label: tr('Close'), value: '' }]); return; }
    const go = await this.ui.choose(remove ? tr('Remove the LAG from the Switches') : tr('Set the LAG on the Switches'),
      tr('These commands are typed on each switch’s console (close its console tab first); then its LAGs are read back.'), items,
      [{ label: tr('Cancel'), value: '' }, { label: remove ? tr('Remove') : tr('Set the LAG'), value: 'go', primary: true }], remove);
    if (go !== 'go') return;
    for (const [vm, name] of todo) await this.lab.applyLag(vm, name, remove);
  }
  ldo(what: string, k: LabLink) { this.menu.set(null); void (what === 'pull' ? this.lab.setLink(k.a, k.aPort, !!k.down) : this.lab.disconnect(k.a, k.aPort)); }

  // links: first switch → its port → second switch → its port
  toggleLink() { if (this.linkMode()) this.cancelLink(); else { this.linkMode.set(true); this.linkA.set(null); } }
  cancelLink() { this.linkMode.set(false); this.linkA.set(null); this.portPick.set(null); this.mouse.set(null); }
  startLinkFrom(d: LabDevice) { this.linkMode.set(true); this.linkA.set(null); void this.askPorts(d, 'a', null); }
  protected linkText() {
    const a = this.linkA();
    if (!a) return tr('Add Link · 1 of 2: click the first switch');
    if (!a.port) return tr('Add Link · pick a port on {0}', this.nameOf(a.id));
    return tr('Add Link · 2 of 2: click the switch at the other end of {0} {1}', this.nameOf(a.id), a.port);
  }
  private async linkClick(d: LabDevice, ev: MouseEvent) {
    const a = this.linkA();
    if (!a || !a.port) { await this.askPorts(d, 'a', ev); return; }
    if (a.id === d.id) return;
    await this.askPorts(d, 'b', ev);
  }
  private async askPorts(d: LabDevice, side: 'a' | 'b', ev: MouseEvent | null) {
    if (side === 'a') this.linkA.set({ id: d.id, port: '' });
    const ports = await this.lab.freePorts(d.id);
    const p = this.pos(d);
    const peerOf = new Map<string, string>();
    for (const k of this.links()) { if (k.a === d.id) peerOf.set(k.aPort, this.nameOf(k.b) + ' ' + k.bPort); if (k.b === d.id) peerOf.set(k.bPort, this.nameOf(k.a) + ' ' + k.aPort); }
    const names = [...new Set([...ports, ...peerOf.keys()])].sort((x, y) => x.localeCompare(y, undefined, { numeric: true }));
    const all = names.map(n => ({ name: n, short: n.split(/[:/]/).pop() ?? n, peer: peerOf.get(n) ?? '' }));
    this.portPick.set({ id: d.id, ports, all, side, x: Math.max(8, p.x * this.zoom() - 210), y: (p.y + BH / 2 + 46) * this.zoom() });
    void ev;
  }
  async portChosen(port: string) {
    const pp = this.portPick(); if (!pp) return;
    this.portPick.set(null);
    if (pp.side === 'a') { this.linkA.set({ id: pp.id, port }); return; }
    const a = this.linkA()!;
    if (await this.lab.connect({ a: a.id, aPort: a.port, b: pp.id, bPort: port })) {
      this.linkA.set(null);
      if (this.emu() && (this.devs().find(d => d.id === a.id)?.running || this.devs().find(d => d.id === pp.id)?.running)) this.ui.toast(tr('The cable works when both switches start next (Reload)'), 'info');
    }
  }

  // actions
  async console(d: LabDevice) {
    const p = this.lab.consoleOf(d.id); if (!p) return;
    const dk = this.docked()[d.id];
    if (dk) { this.docked.update(m => { const n = { ...m }; delete n[d.id]; return n; }); await this.tabs.close(dk); } // a console takes one connection
    void this.tabs.openSession(p);
  }
  ssh(d: LabDevice) { const p = this.lab.sessionOf(d.id); if (p) void this.tabs.openSession(p); }
  consoleAll() { for (const d of this.devs().filter(x => x.running)) (this.emu() ? this.console(d) : this.ssh(d)); }
  configure(d: LabDevice) { this.cfgId.set(d.id); this.page.set('configure'); }
  dialog(tab: string, focus = '') { this.lab.kind.set(this.emu() ? 'emu' : 'sim'); this.lab.dialogTab.set({ tab, focus }); this.ui.open('lab'); }
  loadPack() { this.lab.kind.set('sim'); this.lab.dialogTab.set({ tab: 'list', focus: '', action: 'pack' }); this.ui.open('lab'); }
  wizard() { this.preset.set(''); this.page.set('new'); }
  async reset(d: LabDevice) { if (await this.ui.confirm(tr('Back to Day 0'), tr('Throw away the disk of {0} and boot it as new? Its configuration is lost; the day-0 commands are typed again.', d.hostname), tr('Back to Day 0'), true)) await this.lab.reset([d.id]); }
  async remove(d: LabDevice) {
    const msg = this.emu() ? tr('Remove {0} emulated switches? They stop, their disks on the host and their sessions are deleted.', 1) : tr('Remove {0} simulated switches and their sessions? Their configuration is lost.', 1);
    if (await this.ui.confirm(tr('Remove switches'), msg, tr('Remove'), true) && await this.lab.remove([d.id], true)) { if (this.sel() === d.id) this.sel.set(''); }
  }
  /** Live consoles in the pane, by switch. */
  private docked = signal<Record<string, string>>({});
  protected dockTab = computed(() => { const id = this.docked()[this.sel()]; return id ? this.tabs.get(id) : null; });
  /** The switch's console open in a tab of its own: the console takes one connection, so the pane cannot have it too. */
  protected conTab = computed(() => { const d = this.selDev(); const p = d ? this.lab.consoleOf(d.id) : ''; return p ? this.tabs.tabs().find(t => !t.docked && t.session?.path === p && t.state !== 'disconnected' && t.state !== 'error') ?? null : null; });
  async typeHere() {
    const d = this.selDev(); if (!d || !d.running || this.dockTab()) return;
    if (this.conTab()) { this.ui.toast(tr('Its console is open in a tab - close that tab to type here (a console takes one connection).'), 'error'); return; }
    const p = this.lab.consoleOf(d.id); if (!p) return;
    const id = await this.tabs.openDocked(p);
    if (id) this.docked.update(m => ({ ...m, [d.id]: id }));
    setTimeout(() => document.querySelector<HTMLElement>('app-lab-workspace .dterm textarea')?.focus(), 400);
  }
  async undock() {
    const id = this.docked()[this.sel()]; if (!id) return;
    this.docked.update(m => { const n = { ...m }; delete n[this.sel()]; return n; });
    await this.tabs.close(id);
    void this.loadTail();
  }
  async loadTail() { const id = this.sel(); if (id && this.emu()) { const t = await this.lab.consoleTail(id); if (this.sel() === id) this.tail.set(t); } }
  protected cloudF = signal<{ mode: string; fixed: boolean; addr: string; gw: string; net: string } | null>(null);
  openCloud(mode: string) {
    const v = this.mg();
    const net = v.lanAddr;
    const had = v.mgmt === 'cloud' || (v.mgmt === 'lan' && v.lanFixed); // fixed addresses already
    this.cloudF.set({ mode, fixed: mode === 'cloud' || had, addr: had ? v.mgmtNet.split('/')[0] : '', gw: had ? v.cloudGw : v.gateway, net: net ? net.replace(/\.\d+\//, '.0/') : tr('of the host') });
  }
  lastAddr(a: string) {
    const m = /^(\d+)\.(\d+)\.(\d+)\.(\d+)$/.exec(a.trim()); const n = Math.max(this.devs().length, 1);
    if (!m) return '…';
    const x = ((+m[1] << 24) >>> 0) + (+m[2] << 16) + (+m[3] << 8) + +m[4] + n - 1;
    return [x >>> 24 & 255, x >>> 16 & 255, x >>> 8 & 255, x & 255].join('.');
  }
  async applyCloud() {
    const f = this.cloudF(); if (!f) return;
    this.cloudF.set(null);
    await this.setMgmt(f.mode, f.mode === 'cloud' || f.fixed ? (f.addr.trim() + ' ' + f.gw.trim()).trim() : '');
  }
  /** Shows the lab's management in the menu again (a change not made). */
  protected revertMgmt() { const el = this.host.nativeElement.querySelector<HTMLSelectElement>('select[name=wmg]'); if (el) el.value = this.mg().mgmt; }
  async setMgmt(m: string, net: string | null = null) {
    const g = this.mg(), was = g.mgmt, set = (m: string, n: string) => g.main ? this.lab.setMgmt(m, n) : this.lab.setHostMgmt(g.host, m, n);
    if ((m === 'cloud' || m === 'lan') && net === null) { this.revertMgmt(); this.openCloud(m); return; } // their addresses first
    if (m === was && m !== 'cloud' && m !== 'lan') return;
    net = net ?? '';
    const text: Record<string, string> = {
      lan: net ? tr('The switches join the host’s network with fixed addresses from {0} on.', net.split(' ')[0]) : tr('The switches join the host’s network and get addresses from its DHCP server.'),
      nat: tr('The switches get addresses from the host’s NAT network (DHCP, like GNS3’s NAT node) and are reached through the host.'),
      bridge: tr('The switches get fixed addresses on a bridge of their own on the host, reached through the host.'),
      forward: tr('Each switch’s SSH is forwarded to a port on the host (no root needed).'),
      cloud: tr('The switches get the addresses from {0} on the host’s network, typed on their consoles at each start, and are reached directly.', net.split(' ')[0]),
    };
    const up = this.devs().filter(d => d.running && this.hostOfDev(d) === g.host).map(d => d.id);
    if (!up.length) {
      if (await this.ui.confirm(tr('Change management'), text[m] + ' ' + tr('Their sessions are updated; it applies when the switches start.'), tr('Change'))) await set(m, net);
      else this.revertMgmt();
      return;
    }
    // running switches are stopped for it and started again
    if (!(await this.ui.confirm(tr('Change management'), text[m] + ' ' + tr('{0} switches are running: they are stopped, and started again with it (their configuration stays). Their sessions are updated.', up.length), tr('Stop, Change and Start')))) {
      this.revertMgmt();
      return;
    }
    if (!(await this.lab.stop(up))) { this.revertMgmt(); return; }
    if (!(await set(m, net))) this.revertMgmt(); // they start again as they were
    await this.lab.start(up);
  }
}
