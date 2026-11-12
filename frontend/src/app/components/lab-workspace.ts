import { Component, ElementRef, HostListener, OnDestroy, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { LabService } from '../core/lab.service';
import { Backend } from '../core/backend';
import { EmuDeviceType, EmuHostUse, EmuTemplate, EmuTopoInfo, LabDevice, LabLag, LabLink, LabModel } from '../core/models';
import { TabView, Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { LabConfigure, LabHosts, LabImages, LabNew } from './lab-pages';
import { TerminalView } from './terminal-view';
import { DevIcon, DevRole, ICON_STYLES, IconStyle, ROLES, iconReach, roleOf } from './dev-icon';

const BW = 104, BH = 64;
type Pal = { key: string; name: string; sub: string; group: string; ver: string; os: string; cls: string; dt?: EmuDeviceType; m?: LabModel; tpl?: EmuTemplate };

/** The lab as a workspace tab: device palette, canvas with cables, the
 *  selected switch, the topology summary and the Events log. */
@Component({
  selector: 'app-lab-workspace',
  imports: [FormsModule, LabConfigure, LabHosts, LabImages, LabNew, TerminalView, DevIcon],
  template: `
  <div class="ws" (click)="menu.set(null); topos.set(null); mgOpen.set(false)" [style.grid-template-rows]="'auto minmax(0, 1fr) ' + dockH() + 'px 26px'">
    <div class="tb">
      <b class="lname">{{ emu() ? tr('Emulated Lab') : tr('Simulated Lab') }}</b>
      @if (emu()) { <span class="topo" (click)="openTopos(); $event.stopPropagation()" [title]="tr('Topologies: save, open, new')"><span class="muted">{{ tr('Topology') }}</span> <b>{{ st()?.topology || tr('Untitled') }}</b> ▾</span>
        @if (topos(); as tl) {
          <div class="topos" (click)="$event.stopPropagation()">
            <div class="row2"><b>{{ tr('Topologies') }}</b><span class="sp"></span>
              <button type="button" class="sm" (click)="saveTopo()" [disabled]="!devs().length">{{ st()?.topology ? tr('Save As…') : tr('Save…') }}</button>
              <button type="button" class="sm" (click)="newTopo()">{{ tr('New Topology…') }}</button><button type="button" class="sm" (click)="importTopo()" [title]="tr('Add a topology from a .twlab file')">{{ tr('Import…') }}</button></div>
            @if (remap(); as rm) {
              <div class="remap"><b>{{ tr('Open {0} on your hosts', rm.name) }}</b>
                <p class="muted small">{{ tr('This topology ran on hosts that are not yours (or not checked). Choose where each one’s switches go; on another host they start from their images.') }}</p>
                @for (r of rm.rows; track r.from) {
                  <div class="rmr"><span class="mono">{{ hostLabel(r.from) }}</span><span>→</span>
                    <select [ngModel]="r.to" (ngModelChange)="r.to = $event" [name]="'rm' + $index">@for (h of hostChoices(); track h.session) { <option [value]="h.session">{{ h.label }}</option> }</select></div>
                }
                <div class="row2"><span class="sp"></span><button type="button" class="sm" (click)="remap.set(null)">{{ tr('Cancel') }}</button><button type="button" class="sm primary" (click)="openRemapped()" [disabled]="!hostChoices().length">{{ tr('Open') }}</button></div>
              </div>
            }
            @for (t of tl; track t.name) {
              <div class="ti" [class.cur]="t.open"><div class="tn"><b>{{ t.name }}</b><small class="muted">{{ tr('{0} switches', t.switches) }}@if (t.hosts?.length) { · {{ hostLabel(t.hosts![0]) }} } · {{ when(t.saved) }}</small></div>
                <button type="button" class="sm x" (click)="dupTopo(t.name)" [title]="tr('Duplicate')">⧉</button><button type="button" class="sm x" (click)="renTopo(t.name)" [title]="tr('Rename')">✎</button><button type="button" class="sm x" (click)="exportTopo(t.name)" [title]="tr('Export (.twlab)')">⇪</button>
                @if (t.open) { <span class="muted small">{{ tr('open') }}</span> }
                @else { <button type="button" class="sm" (click)="openTopo(t.name, false)">{{ tr('Open') }}</button><button type="button" class="sm primary" (click)="openTopo(t.name, true)" [disabled]="!t.switches">▶ {{ tr('Open and Start All') }}</button>
                  <button type="button" class="sm x" (click)="delTopo(t.name)" [title]="tr('Delete')">✕</button> }
              </div>
            } @empty { <p class="muted small">{{ tr('No saved topologies yet: Save… keeps this lab under a name.') }}</p> }
          </div>
        }
        @if (!st()?.host) { <span class="chip" [title]="tr('KVM host')"><i class="d"></i>{{ tr('No host yet') }}</span> } }
      @else { <span class="chip" [title]="tr('The device pack the simulated switches come from')">{{ st()?.pack ? st()!.pack!.name + ' ' + st()!.pack!.version : tr('No device pack') }}</span>
        <button type="button" (click)="loadPack()">{{ st()?.pack ? tr('Load Another Pack…') : tr('Load Device Pack (.vm)…') }}</button> }
      @if (emu()) {
        <nav class="ptabs">
          <a [class.on]="page() !== 'hosts' && page() !== 'images' && page() !== 'templates'" (click)="page.set('')">{{ tr('Canvas') }}</a>
          <a [class.on]="page() === 'hosts'" (click)="page.set('hosts')">{{ tr('Hosts') }}@if ((st()?.hosts ?? []).length) { <small>{{ st()!.hosts!.length }}</small> }</a>
          <a [class.on]="page() === 'images'" (click)="page.set('images')">{{ tr('Images') }}@if ((st()?.images ?? []).length) { <small>{{ st()!.images!.length }}</small> }</a>
          <a [class.on]="page() === 'templates'" (click)="page.set('templates')">{{ tr('Templates') }}@if ((st()?.templates ?? []).length) { <small>{{ st()!.templates!.length }}</small> }</a>
        </nav>
      }
      @if (devs().length) {
        <span class="health" [title]="tr('{0} running', counts().on) + ' · ' + tr('{0} booting', counts().boot) + ' · ' + tr('{0} with a problem', counts().warn + counts().bad) + ' · ' + tr('{0} stopped', counts().off)">
          <span class="hbar"><i class="on" [style.flex]="counts().on"></i><i class="boot" [style.flex]="counts().boot"></i><i class="bad" [style.flex]="counts().warn + counts().bad"></i><i class="off" [style.flex]="counts().off"></i></span>
          <span class="htx"><span class="g">{{ tr('{0} up', counts().on) }}</span>@if (counts().boot) { <span class="w">· {{ tr('{0} booting', counts().boot) }}</span> }@if (counts().warn + counts().bad) { <span class="r">· {{ tr('{0} with a problem', counts().warn + counts().bad) }}</span> }@if (counts().off) { <span class="muted">· {{ tr('{0} off', counts().off) }}</span> }</span>
        </span>
      }
      <span class="sp"></span>
      @if (emu() && devs().length) {
        <span class="mgw">
          <button type="button" class="mgb" (click)="mgOpen.set(!mgOpen()); $event.stopPropagation()" [title]="tr('The hosts of this topology: their use, and how the switches’ first port (management) is connected')">@if (multi()) { @for (r of mgRows(); track r.host) { <i class="hd" [style.background]="r.c"></i> } }<span class="mgt">{{ mgSummary() }}</span> ▾</button>
          @if (mgOpen()) {
            <div class="mgp" (click)="$event.stopPropagation()">
              <b>{{ tr('Hosts of this topology') }}</b>
              <p class="muted small">{{ tr('What each host gives the switches, and how their first port (management) is connected. A management change applies when the switches start; running ones are stopped and started again.') }}</p>
              @for (r of mgRows(); track r.host) {
                <div class="mgr"><span class="mgn">@if (multi()) { <i class="hd" [style.background]="r.c"></i> }{{ r.name }}<small class="muted">{{ r.kvm }}</small></span>
                  <select [attr.data-h]="r.host" [ngModel]="r.mgmt" (ngModelChange)="mgHost.set(r.host); setMgmt($event)" [disabled]="lab.busy()">
                    <option value="cloud">{{ tr('The host’s network, routed (cloud)') }}</option>
                    <option value="lan">{{ tr('The host’s network (bridged)') }}</option>
                    <option value="nat">{{ tr('The host’s NAT network') }}</option>
                    <option value="bridge">{{ tr('Lab bridge on the host') }}</option>
                    <option value="forward">{{ tr('Port forwarding on the host') }}</option>
                  </select>
                  @if (r.mgmt === 'cloud' || r.mgmt === 'lan') { <button type="button" class="sm" (click)="mgHost.set(r.host); openCloud(r.mgmt)" [title]="tr('Addresses of the switches on the host’s network')">{{ r.addr }}…</button> } @else { <span></span> }
                </div>
                @if (r.cpus) {
                  <div class="hu"><span>{{ tr('{0} switches · {1} running', r.switches, r.running) }}</span>
                    <span>vCPU {{ r.vcpus }} / {{ r.cpus }}<span class="meter"><i [style.width.%]="pct(r.vcpus, r.cpus)" [style.background]="r.c"></i></span></span>
                    <span>RAM {{ gb(r.ram) }} / {{ gb(r.memTotal) }}<span class="meter"><i [style.width.%]="pct(r.memTotal - r.memFree, r.memTotal)" [style.background]="r.c"></i></span></span>
                    <span class="muted">{{ tr('load') }} {{ r.load }} · {{ tr('{0} free', gb(r.memFree)) }}</span></div>
                }
              }
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
          }
              @if (multi()) { <div class="mgt2"><b>{{ tr('Cables between hosts') }}</b> <span class="muted">{{ tunnels() }} · {{ tr('UDP tunnels: the hosts must reach each other on UDP 20000-29999') }}</span>
                @for (r of hostLinks(); track $index) { <div class="small" [class.okt]="r.ok" [class.badt]="!r.ok">{{ r.ok ? '✓' : '✗' }} {{ r.from }} → {{ r.to }}@if (!r.ok) { <span class="muted"> · {{ r.text }}</span> }</div> }</div> }
              <div class="row2"><button type="button" class="sm" (click)="wizard(); mgOpen.set(false)" [title]="tr('Add Switches: pick another host for them')">+ {{ tr('Add a Host to this Topology') }}</button>
                @if (multi()) { <button type="button" class="sm" (click)="checkTunnels()" [disabled]="checkingT()">{{ checkingT() ? tr('Checking…') : tr('Check UDP Between Hosts') }}</button> }
                <span class="sp"></span><button type="button" class="sm" (click)="page.set('hosts'); mgOpen.set(false)">{{ tr('All Hosts…') }}</button></div>
            </div>
          }
        </span>
      }
      @if (!emu()) { <button type="button" (click)="dialog('list')">{{ tr('Switch List and Outputs') }}</button> }
      <button type="button" class="primary" (click)="lab.start([])" [disabled]="!devs().length || lab.busy()">▶ {{ tr('Start All') }}</button>
      <button type="button" (click)="lab.stop([])" [disabled]="!running() || lab.busy()">■ {{ tr('Stop All') }}</button>
    </div>

    <div class="main">
      @if (page() === 'new') { <app-lab-new [emu]="emu()" [preset]="preset()" (closed)="page.set(''); ghost.set(null)" (changed)="ghost.set($event)" (hostsPage)="page.set('hosts')" (made)="ui.refreshTree()" /> }
      <div class="pal">
        <input class="srch" [(ngModel)]="q" name="pq" [placeholder]="tr('Search devices')" spellcheck="false" />
        @for (g of groups(); track g.name) {
          <div class="grp"><span>{{ g.name }}</span><span>{{ g.ver }}</span></div>
          @for (p of g.items; track p.key) {
            <div class="dev" draggable="true" (dragstart)="dragPal($event, p)" (dblclick)="addAt(p, null)" [title]="tr('Drag onto the canvas, or double-click to add one')">
              <app-dev-icon class="pico" [role]="palRole(p)" [look]="defLook()" [size]="46" state="idle" /><div><span>{{ p.name }}</span>@if (p.sub) { <small>{{ p.sub }}</small> }</div></div>
          }
        } @empty {
          @if (emu()) { <p class="muted small">{{ tr('No images yet: Images → Add Image.') }}</p> }
          @else { <p class="muted small">{{ tr('Load a device pack first.') }}</p><button type="button" class="primary" (click)="loadPack()">{{ tr('Load Device Pack (.vm)…') }}</button> }
        }
        <div class="hint">{{ tr('Drag a device onto the canvas to add it.') }}</div>
      </div>

      <div class="cv" #cv (scroll)="onScroll(cv)" (dragover)="$event.preventDefault()" (drop)="dropPal($event, cv)" (pointermove)="move($event, cv)" (pointerup)="up()" (pointerleave)="up()"
           (contextmenu)="$event.preventDefault()">
        @if (linkMode()) { <div class="bar2">⟷ {{ linkText() }} <span class="k2">Esc</span> <a (click)="cancelLink(); $event.stopPropagation()">{{ tr('Cancel') }}</a></div> }
        @if (!devs().length && !ghosts().length) {
          <div class="empty"><b>{{ emu() ? tr('No switches yet') : tr('No simulated switches yet') }}</b><span>{{ tr('Drag a device from the left, or') }} <a (click)="wizard()">{{ tr('make a lab') }}</a>.</span>
            @if (emu()) { <span class="muted small">{{ tr('How devices are drawn:') }}</span><div class="lookcards">@for (st of looks; track st.k) { <a [class.on]="defLook() === st.k" (click)="setDefLook(st.k)"><app-dev-icon role="router" [look]="st.k" [size]="54" state="idle" /><span>{{ tr(st.name) }}</span></a> }</div> }</div>
        }
        <div class="inner" [style.width.px]="area().w * zoom()" [style.height.px]="area().h * zoom()">
          <div class="scale" [style.transform]="'scale(' + zoom() + ')'" [style.width.px]="area().w" [style.height.px]="area().h">
            @for (z of (hostMode() === 'areas' ? zones() : []); track z.host) {
              <div class="zone" [style.left.px]="z.x" [style.top.px]="z.y" [style.width.px]="z.w" [style.height.px]="z.h" [style.border-color]="z.c" [style.background]="z.c + '0f'">
                <div class="zh"><i [style.background]="z.c"></i><b>{{ z.name }}</b><span class="muted">{{ z.text }}</span></div></div>
            }
            <svg class="links" [attr.width]="area().w" [attr.height]="area().h">
              @for (k of lines(); track k.key) {
                <g class="lk" [class.dimk]="linkDim(k.link)" [class.tun]="k.tun" [class.down]="k.down" [class.warn]="k.warn" [class.selk]="selLink() === k.key" (click)="pickLink(k.link, $event)" (contextmenu)="linkMenu(k.link, $event)">
                  <title>{{ k.tip }}</title>
                  @for (m of k.paths; track $index) { <path class="hit" [attr.d]="m.d" /><path class="ln" [attr.d]="m.d" [class.up]="m.up" [class.dn]="m.down" [class.thick]="m.thick" /> }
                  @for (r of k.rings; track $index) { <circle class="ring" [attr.cx]="r.x" [attr.cy]="r.y" r="11" /> }
                  @for (lb of k.labels; track $index) { @if (lb.lag || showPorts()) { <rect class="pb" [class.lb]="lb.lag" [attr.x]="lb.x - lb.w / 2" [attr.y]="lb.y - 9" [attr.width]="lb.w" height="18" rx="5" /><text class="pt" [attr.x]="lb.x" [attr.y]="lb.y + 4">{{ lb.t }}</text> } }
                </g>
              }
              @for (g of ghostLines(); track $index) { <line class="gl" [attr.x1]="g.x1" [attr.y1]="g.y1" [attr.x2]="g.x2" [attr.y2]="g.y2" /> }
              @if (rubber(); as r) { <line class="rb" [attr.x1]="r.x1" [attr.y1]="r.y1" [attr.x2]="r.x2" [attr.y2]="r.y2" /> }
            </svg>
            @for (g of ghosts(); track g.name) { <div class="node ghost" [style.left.px]="g.x - 75" [style.top.px]="g.y - BH / 2"><div class="nbox"><app-dev-icon role="access" [look]="defLook()" [size]="100" state="idle" /></div><div class="nn">{{ g.name }}</div><div class="nm">{{ tr('new') }}</div></div> }
            @for (d of devs(); track d.id) {
              <div class="node" [class.dimn]="dimOf(d)" [class.sel]="sel() === d.id" [class.pick]="linkA()?.id === d.id" [style.left.px]="pos(d).x - 75" [style.top.px]="pos(d).y - BH / 2"
                   (pointerdown)="grab($event, d, cv)" (click)="clickNode(d, $event)" (dblclick)="open(d)" (contextmenu)="nodeMenu(d, $event)" (mouseenter)="hov.set(d.id)" (mouseleave)="hov.set('')">
                <div class="nbox" [class.dim]="stateOf(d) === 'off'"><app-dev-icon [role]="roleOfDev(d)" [look]="lookOf(d)" [state]="iconState(d)" [size]="100" /><i class="st" [class]="'st ' + stateOf(d)" [title]="tr(d.stateText || (d.running ? 'running' : 'stopped'))"></i>@if (warnOf(d)) { <i class="wn" [title]="warnOf(d)">!</i> }</div>
                <div class="nn">{{ d.hostname }}</div>
                <div class="nm">{{ d.persona?.slots?.[0]?.model || d.model }}@if (showAddr()) { · {{ subOf(d) }} }</div>
                @if (hostMode() === 'tags') { <div class="htg"><i [style.background]="hcol(hostOfDev(d))"></i>{{ hname(hostOfDev(d)) }}</div> }
                @if (d.uplinks?.length) { <span class="badge up" [title]="tr('On the host’s network: {0}', d.uplinks!.join(', '))">⇡ {{ d.uplinks!.join(',') }}</span> }
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
              @if (emu()) { <a (click)="uplink(d)" [title]="tr('Like a cloud node on a data port: the port joins the host’s network (applies when the switch starts)')">{{ tr('Port on the Host’s Network…') }}</a>
                @for (u of d.uplinks ?? []; track u) { <a (click)="setUplink(d, u, false)">{{ tr('Take {0} off the Host’s Network', u) }}</a> } }
              @if (emu()) { <a (click)="mdo('reset', d)">{{ tr('Back to Day 0') }}</a> }
              @if (emu()) { <hr /><span class="mh2" [title]="tr('For every {0} in this topology, new ones too', kindOf(d))">{{ tr('Draw As') }} · <small>{{ tr('all {0}', kindOf(d)) }}</small></span><div class="roles">@for (r of roles; track r.k) { <a [class.on]="roleOfDev(d) === r.k" (click)="setRole(d, r.k)" [title]="tr(r.name)"><app-dev-icon [role]="r.k" [look]="lookOf(d)" [size]="34" state="idle" /></a> }</div>
                <div class="roles looks">@for (st of looks; track st.k) { <a [class.on]="lookOf(d) === st.k" (click)="setLook(d, st.k)" [title]="tr(st.name)"><app-dev-icon [role]="roleOfDev(d)" [look]="st.k" [size]="34" state="idle" /></a> }</div> }
              @if (emu() && otherHosts(d).length) { <hr /><span class="mh2">{{ tr('Move to Host') }}</span>@for (o of otherHosts(d); track o.session) { <a (click)="moveTo(d, o.session)" [title]="d.running ? tr('Live: it keeps running while it moves') : tr('It takes its disk along: its configuration and day 0 stay')">→ {{ o.name || hname(o.session) }}</a> } }
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
      </div>
      @if (page() !== 'new') { <div class="tdw"><div class="tdock" (click)="$event.stopPropagation()">
        <button type="button" (click)="wizard()" [title]="devs().length ? tr('Add switches to this lab') : tr('Make a lab')">＋<span>{{ devs().length ? tr('Add Switches') : tr('New Lab') }}</span></button>
        <button type="button" [class.on]="linkMode()" (click)="toggleLink(); $event.stopPropagation()" [disabled]="devs().length < 2" [title]="tr('Draw a cable between two switches')">⟷<span>{{ tr('Add Link') }}</span></button>
        <i class="dsep"></i>
        @if (emu()) { <button type="button" (click)="sel() && lab.restart([sel()])" [disabled]="!selDev()?.running || lab.busy()" [title]="selDev() ? tr('Restart {0}', selDev()!.hostname) : tr('Select a running switch to restart it')">⟳<span>{{ tr('Restart') }}</span></button> }
        <button type="button" (click)="consoleAll()" [disabled]="!running()" [title]="tr('Open the console of every running switch')">⌨<span>{{ emu() ? tr('Console All') : tr('Open All') }}</span></button>
        @if (emu() && links().length) { <button type="button" (click)="lab.checkCabling()" [disabled]="lab.busy() || !running()" [title]="tr('Asks the running switches for their LLDP neighbours and marks each cable: as drawn, or not')">✓<span>{{ tr('Check Cabling') }}</span></button> }
        <i class="dsep"></i>
        <button type="button" class="z" (click)="zoomBy(-0.1)" [attr.aria-label]="tr('Zoom out')">−</button><button type="button" class="z pct" (click)="zoom.set(1)" [title]="tr('Back to 100 %')">{{ (zoom() * 100).toFixed(0) }} %</button><button type="button" class="z" (click)="zoomBy(0.1)" [attr.aria-label]="tr('Zoom in')">+</button>
        @if (emu()) { <select class="looksel" [ngModel]="defLook()" (ngModelChange)="setDefLook($event)" name="looksel" [title]="tr('How devices are drawn (Draw As on a switch changes its kind only)')">@for (st of looks; track st.k) { <option [value]="st.k">{{ tr(st.name) }}</option> }</select> }
      </div></div> }
      @if (page() !== 'new' && devs().length) {
        <div class="vbar" (click)="$event.stopPropagation()">
          <button type="button" class="sm" (click)="fit(cv)" [title]="tr('The whole topology in view')">{{ tr('Fit') }}</button><button type="button" class="sm" (click)="fill(cv)" [title]="tr('As wide as the canvas')">{{ tr('Fill') }}</button><button type="button" class="sm" (click)="zoom.set(1)">100 %</button>
          <i class="dsep"></i>
          @if (multi()) { <span class="muted small">{{ tr('Hosts') }}</span><select class="sm" [ngModel]="hostView()" (ngModelChange)="setHostView($event)" name="hview" [title]="tr('How each switch’s host is shown')"><option value="auto">{{ tr('Auto') }}</option><option value="areas">{{ tr('Areas') }}</option><option value="tags">{{ tr('Tags') }}</option><option value="off">{{ tr('Off') }}</option></select>
            <button type="button" class="sm" (click)="arrangeByHost()" [title]="tr('Put each host’s switches together')">⊞ {{ tr('Arrange by Host') }}</button><i class="dsep"></i> }
          <button type="button" class="sm" [class.on]="showPorts()" (click)="flip(showPorts, 'tw.lab.ports')" [title]="tr('Port names on the cables')">{{ tr('Ports') }}</button>
          <button type="button" class="sm" [class.on]="showAddr()" (click)="flip(showAddr, 'tw.lab.addr')" [title]="tr('Addresses under the switches')">{{ tr('Addresses') }}</button>
          <button type="button" class="sm" [class.on]="onlyProblems()" (click)="onlyProblems.set(!onlyProblems())" [title]="tr('Dim everything that is fine')">{{ tr('Only problems') }}</button>
          <button type="button" class="sm" [class.on]="miniOn()" (click)="flip(miniOn, 'tw.lab.mini')" [title]="tr('A small map of the whole topology')">{{ tr('Map') }}</button>
        </div>
        @if (hostMode() === 'tags') {
          <div class="hleg" (click)="$event.stopPropagation()"><span class="muted small">{{ tr('Hosts · click one to focus') }}</span>
            @for (r of mgRows(); track r.host) { <a [class.on]="focusHost() === r.host" [class.off]="!!focusHost() && focusHost() !== r.host" (click)="focusHost.set(focusHost() === r.host ? '' : r.host)"><i [style.background]="r.c"></i><b>{{ r.name }}</b><span class="muted">{{ r.kvm }} · {{ r.switches }}</span></a> }
          </div>
        }
        @if (miniOn(); as _) { @if (mini(); as mm) {
          <div class="mini" (click)="miniGo($event, cv)" [title]="tr('Click to go there')">
            <svg [attr.width]="mm.w" [attr.height]="mm.h">
              @for (z of mm.zones; track z.host) { <rect [attr.x]="z.x" [attr.y]="z.y" [attr.width]="z.w" [attr.height]="z.h" rx="3" fill="none" [attr.stroke]="z.c" stroke-width="1" opacity=".7" /> }
              @for (p of mm.dots; track p.id) { <rect [attr.x]="p.x - 3" [attr.y]="p.y - 2" width="6" height="4" rx="1" [attr.fill]="p.c" /> }
              <rect class="vp" [attr.x]="mm.vx" [attr.y]="mm.vy" [attr.width]="mm.vw" [attr.height]="mm.vh" rx="2" />
            </svg>
          </div>
        } }
      }

      <div class="side">
        @if (selDev(); as d) {
          <h3>{{ d.hostname }}</h3>
          <div class="card"><div class="kv">
            <span>{{ tr('Device') }}</span><b>{{ d.persona?.slots?.[0]?.model || d.model }}@if (d.version) { · {{ d.version }} }</b>
            <span>{{ tr('State') }}</span><span [class]="warnOf(d) ? 'w' : stateOf(d)">{{ tr(d.stateText || (d.running ? 'running' : 'stopped')) }}</span>
            @if (d.persona) { <span>{{ tr('Persona') }}</span>
              @if (d.personaError) { <span class="r"><span class="clamp" [title]="d.personaError + '\n\n' + tr('All of it is in the Events log.')">{{ brief(d.personaError) }}</span><a class="evl" (click)="dock.set('events')">{{ tr('Details in Events') }} ›</a></span> }
              @else { <span [class.g]="d.personaOk">{{ d.personaOk ? '✓ ' + tr('in the switch') : d.personaInfo ? tr(brief(d.personaInfo)) : tr('not written yet') }}</span> } }
            @if (d.personaInfo && !d.personaError) { <span></span><span class="mono xs clamp" [title]="d.personaInfo">{{ d.personaInfo }}</span> }
            <span>{{ tr('Serial') }}</span><span class="mono">{{ d.serial || '-' }}</span>
            <span>MAC</span><span class="mono">{{ d.mac || '-' }}</span>
            <span>{{ tr('Management') }}</span><span class="mono">{{ emu() ? d.ssh : d.mgmtIp }}</span>
            @if (emu()) { <span>{{ tr('Console') }}</span><span class="mono">telnet :{{ d.console }}</span>
              <span>{{ tr('Resources') }}</span><span>{{ d.cpus }} vCPU · {{ gb(d.ram) }}@if (d.diskGb) { · {{ d.diskGb }} GB }@if (d.running && d.cpu !== undefined) { · CPU {{ d.cpu!.toFixed(0) }} % }</span> }
            @if (d.error) { <span>{{ tr('Problem') }}</span><span class="r"><span class="clamp" [title]="d.error + '\n\n' + tr('All of it is in the Events log.')">{{ brief(d.error) }}</span><a class="evl" (click)="dock.set('events')">{{ tr('Details in Events') }} ›</a>@if (d.running && d.error.startsWith('Day-0')) { <a class="evl" (click)="lab.day0Again(d.id)" [title]="tr('Its disk and configuration stay')">{{ tr('Type Day 0 Again') }}</a> }</span> }
          </div>
          <div class="btns">@if (emu()) { <button type="button" class="primary sm" (click)="console(d)" [disabled]="!d.running">{{ tr('Console') }}</button> }
            <button type="button" class="sm" (click)="ssh(d)" [disabled]="!d.running">{{ emu() ? 'SSH' : tr('Open Session') }}</button><button type="button" class="sm" (click)="configure(d)">{{ tr('Configure…') }}</button></div></div>
        }
        <h3>{{ tr('Topology summary') }}</h3>
        <div class="card tbl"><table>
          @for (d of devs(); track d.id) { <tr [class.on]="sel() === d.id" (click)="select(d)"><td><i class="d" [class]="'d ' + stateOf(d)"></i>{{ d.hostname }}</td><td class="mono">{{ emu() ? ':' + d.console : ':' + d.port }}</td><td class="mono">{{ addrOf(d) }}</td></tr> }
          @empty { <tr><td class="muted">{{ tr('No switches yet') }}</td></tr> }
        </table></div>
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
        @if (dock() === 'inv' && inventory().length > 1) { <button type="button" class="sm" (click)="flipAll()">{{ invClosed().size === inventory().length ? tr('Expand All') : tr('Collapse All') }}</button> }
        @else { <button type="button" class="sm" (click)="copyDock()" [title]="tr('Copy to the clipboard')">{{ tr('Copy') }}</button> }
        @if (dock() === 'events' && emu()) { <button type="button" class="sm" (click)="clearEvents()">{{ tr('Clear') }}</button> }
        <button type="button" class="sm" (click)="dockH.set(dockH() > 260 ? 190 : 420)" [title]="tr('Taller or shorter')">{{ dockH() > 260 ? '▾' : '▴' }}</button>
      </div>
      @if (dock() === 'inv') {
        <div class="inv"><table>
          <tr><th>{{ tr('Switch') }}</th><th>{{ tr('State') }}</th><th>{{ tr('Console') }}</th><th>{{ tr('Management') }}</th><th>CPU</th><th>{{ tr('Memory') }}</th></tr>
          @for (g of inventory(); track g.host) {
            <tbody>
            <tr class="h" (click)="flipHost(g.id)" [title]="invClosed().has(g.id) ? tr('Show its switches') : tr('Hide its switches')">
              <td colspan="4"><span class="chev">{{ invClosed().has(g.id) ? '▸' : '▾' }}</span><i class="hd" [style.background]="hcol(g.id)"></i><b>{{ g.host }}</b>
                @if (g.kvm) { <span class="pill">{{ g.kvm }}</span> }@if (g.addr) { <span class="muted">{{ g.addr }}</span> }<span class="muted"> · {{ g.mgmt }}</span>
                <span class="pill">{{ tr('{0} switches · {1} running', g.devs.length, g.running) }}</span></td>
              <td><span class="mtr"><i [style.width.%]="g.cpuPct"></i></span>{{ g.cpuText }}</td><td><span class="mtr"><i [style.width.%]="g.memPct"></i></span>{{ g.memText }}</td></tr>
            @if (!invClosed().has(g.id)) {
              @for (d of g.devs; track d.id) {
                <tr class="d" [class.on]="sel() === d.id" (click)="select(d)"><td><span class="guide" [style.border-color]="hcol(g.id)"></span>{{ d.hostname }}</td><td [class]="stateOf(d)">● {{ tr(d.stateText || (d.running ? 'running' : 'stopped')) }}</td>
                  <td class="mono" [title]="g.host + ':' + d.console">:{{ d.console }}</td><td class="mono">{{ d.ssh || '-' }}</td>
                  <td>@if (d.running) { <span class="mtr"><i [style.width.%]="d.cpu ?? 0"></i></span>{{ (d.cpu ?? 0).toFixed(0) }} % }</td>
                  <td>@if (d.running) { <span class="mtr"><i [style.width.%]="pct(d.memUsed ?? 0, d.ram ?? 0)"></i></span>{{ d.memUsed ? gb(d.memUsed) + ' / ' : '' }}{{ gb(d.ram) }} }</td></tr>
              } @empty { <tr class="d"><td colspan="6" class="muted"><span class="guide" [style.border-color]="hcol(g.id)"></span>{{ tr('No switches on this host') }}</td></tr> }
            }
            </tbody>
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
    <div class="sbar">
      @if (emu()) {
        <span class="si top">⬢ {{ st()?.topology || tr('Untitled') }}</span>
        @for (r of mgRows(); track r.host) { <a class="si" (click)="mgOpen.set(true); $event.stopPropagation()" [title]="r.name + ' · ' + r.long"><i class="hd" [style.background]="r.c"></i>{{ r.name }}<span class="muted">{{ r.kvm }} · {{ r.short }}</span></a> }
        @if (busyLine(); as b) { <a class="si job" (click)="dock.set('events')" [title]="tr('See the Events')"><i class="spin"></i>{{ b }}</a> }
        <span class="sp"></span>
        @if ((st()?.downloadQueue ?? []).length) { <a class="si" (click)="page.set('images')">↓ {{ tr('{0} images downloading or waiting', st()!.downloadQueue!.length) }}</a> }
        <span class="si">{{ tr('{0} images', (st()?.images ?? []).length) }}</span>
        @if (totals(); as t) { <span class="si">vCPU {{ t.v }} / {{ t.c }} · RAM {{ gb(t.r) }} / {{ gb(t.m) }}</span> }
        <label class="si fs" [title]="tr('On: Stop saves each running switch’s state into its disk, and Start resumes it in seconds (no boot, no day 0). A change to its persona, management, memory or image makes it boot again.') + (st()?.usage?.saved ? ' ' + tr('Saved states now: about {0} on the hosts’ disks.', gb(st()!.usage!.saved)) : '')"><input type="checkbox" [checked]="!!st()?.fast" (change)="lab.setFast($any($event.target).checked)" [disabled]="lab.busy()" />{{ tr('Fast start') }}@if (st()?.usage?.saved) { <span class="muted">· {{ gb(st()!.usage!.saved) }}</span> }</label>
      } @else {
        <span class="si top">{{ st()?.pack ? st()!.pack!.name + ' ' + st()!.pack!.version : tr('No device pack') }}</span>
        <span class="sp"></span><span class="si">{{ tr('{0} running', counts().on) }} · {{ tr('{0} stopped', counts().off) }}</span>
      }
    </div>
  </div>`,
  styles: [`
    :host { display: block; height: 100%; }
    .ws { height: 100%; display: grid; background: var(--bg); color: var(--text); font-size: 13px; }
    .tb { position: relative; display: flex; flex-wrap: nowrap; min-width: 0; align-items: center; gap: 6px; padding: 8px 12px; border-bottom: 1px solid var(--line); background: var(--surface); white-space: nowrap;
      button { height: 30px; white-space: nowrap; } button.on { background: var(--accent-soft); border-color: var(--accent); color: var(--accent); } }
      .lname { margin-right: 4px; font-size: 14px; } .sep { width: 1px; height: 22px; background: var(--line); margin: 0 4px; flex: none; } .sp { flex: 1; }
    .tb { container-type: inline-size; } .tb > button { flex: none; } .tb > .topo { flex: 0 1 auto; min-width: 80px; } .lname { flex: none; }
    @container (max-width: 1050px) { .lname, .mgl { display: none; } .health .htx { display: none; } }
    .htx > span + span { margin-left: 4px; }
    @container (max-width: 820px) { .ptabs a { padding: 0 7px; } .ptabs small { display: none; } }
    .vbar { position: absolute; top: 10px; right: 312px; z-index: 6; display: flex; align-items: center; gap: 4px; padding: 4px; border-radius: 10px; background: var(--surface); border: 1px solid var(--border, var(--line)); box-shadow: 0 6px 18px rgba(0,0,0,.25); max-width: calc(100% - 560px); overflow-x: auto;
      button { white-space: nowrap; flex: none; } button.on { background: var(--accent-soft); border-color: var(--accent); color: var(--accent); } select { height: 26px; font-size: 12px; flex: none; } > span { white-space: nowrap; } }
    .hleg { position: absolute; top: 58px; left: 242px; z-index: 6; display: flex; flex-direction: column; gap: 4px; max-height: 50%; overflow: auto;
      a { display: flex; align-items: center; gap: 7px; padding: 5px 9px; border-radius: 8px; border: 1px solid var(--line); background: var(--surface); cursor: pointer; font-size: 12px; white-space: nowrap; }
      a.on { border-color: var(--accent); } a.off { opacity: .5; } i { width: 8px; height: 8px; border-radius: 50%; display: inline-block; } }
    .mini { position: absolute; right: 312px; bottom: 66px; z-index: 6; padding: 6px; border-radius: 10px; background: var(--surface); border: 1px solid var(--border, var(--line)); box-shadow: 0 6px 18px rgba(0,0,0,.25); cursor: pointer; line-height: 0;
      svg { background: var(--bg); border-radius: 6px; } .vp { fill: color-mix(in srgb, var(--accent) 12%, transparent); stroke: var(--accent); stroke-width: 1.2; } }
    .node.dimn { opacity: .28; } .lk.dimk { opacity: .18; }
    .htg { display: inline-flex; align-items: center; gap: 4px; font-size: 10.5px; color: var(--muted); i { width: 6px; height: 6px; border-radius: 50%; display: inline-block; } }
    .ptabs { display: flex; align-self: stretch; margin: -8px 0 -8px 4px; flex: none;
      a { display: flex; align-items: center; gap: 5px; padding: 0 11px; border-bottom: 2px solid transparent; color: var(--muted); cursor: pointer; } a.on { color: var(--text); border-color: var(--accent); } small { font-size: 11px; color: var(--muted); } }
    .health { flex: 0 1 auto; display: inline-flex; align-items: center; gap: 5px; height: 30px; padding: 0 10px; border-radius: 8px; border: 1px solid var(--line); background: var(--surface-2); font-size: 12px; min-width: 0; overflow: hidden; }
    .hbar { display: flex; width: 60px; height: 6px; border-radius: 3px; overflow: hidden; background: var(--line); flex: none; margin-right: 3px;
      i { display: block; } i.on { background: var(--success, #1fa45a); } i.boot { background: var(--warning, #e39b17); } i.bad { background: var(--danger); } i.off { background: var(--muted); opacity: .5; } }
    .mgw { position: relative; min-width: 90px; flex: 0 1 auto; } .htx { overflow: hidden; text-overflow: ellipsis; }
    .si.fs { cursor: pointer; input { margin: 0; } } .mgb { max-width: 300px; width: 100%; display: inline-flex; align-items: center; gap: 6px; } .mgt { overflow: hidden; text-overflow: ellipsis; }
    .mgp { position: absolute; top: calc(100% + 6px); right: 0; z-index: 30; width: 540px; padding: 14px; border: 1px solid var(--border); border-radius: 10px; background: var(--surface); box-shadow: 0 10px 30px rgba(0,0,0,.35); white-space: normal; display: grid; gap: 8px;
      p { margin: 0; } .cloudf { position: static; width: auto; box-shadow: none; margin-top: 4px; } }
    .mgr { display: grid; grid-template-columns: 140px minmax(0, 1fr) auto; gap: 8px; align-items: center; select { width: 100%; } }
    .hu { grid-column: 1 / -1; display: grid; grid-template-columns: 1.1fr 1fr 1fr 1fr; gap: 10px; align-items: center; font-size: 11.5px; margin: -2px 0 6px; padding-bottom: 8px; border-bottom: 1px dashed var(--line);
      > span { display: flex; flex-direction: column; gap: 3px; } .meter { display: block; margin: 0; } }
    .mgt2 { display: grid; gap: 4px; font-size: 12px; } .mgp .row2 { display: flex; gap: 8px; align-items: center; }
    .mgn { display: flex; align-items: center; gap: 4px; overflow: hidden; small { margin-left: 4px; } }
    .tdw { position: absolute; left: 230px; right: 300px; bottom: 14px; display: flex; justify-content: center; pointer-events: none; z-index: 6; container-type: inline-size; }
    .tdock { pointer-events: auto; display: flex; align-items: center; gap: 2px; padding: 5px; border-radius: 12px; background: var(--surface); border: 1px solid var(--border, var(--line)); box-shadow: 0 10px 28px rgba(0,0,0,.35); max-width: calc(100% - 24px); overflow-x: auto;
      button { height: 32px; display: inline-flex; align-items: center; gap: 6px; padding: 0 10px; border: 0; background: none; border-radius: 8px; white-space: nowrap; font-size: 12.5px; }
      button:hover:not(:disabled) { background: var(--surface-2); } button.on { background: var(--accent-soft); color: var(--accent); } button.z { padding: 0 8px; } .pct { min-width: 54px; justify-content: center; color: var(--muted); } .looksel { margin-left: 4px; } }
    .dsep { width: 1px; height: 20px; background: var(--line); margin: 0 3px; flex: none; }
    @container (max-width: 620px) { .tdock button span { display: none; } }
    .sbar { display: flex; align-items: center; min-width: 0; overflow: hidden; border-top: 1px solid var(--line); background: var(--surface); font-size: 11.5px; .sp { flex: 1; } }
    .si { display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 10px; border-right: 1px solid var(--line); white-space: nowrap; color: var(--text); text-decoration: none; flex: none; .hd { margin: 0; } }
    a.si { cursor: pointer; } a.si:hover { background: var(--surface-2); } .si.top { background: var(--accent); color: #fff; border: 0; } .si.job { flex: 0 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; }
    .spin { width: 9px; height: 9px; border-radius: 50%; border: 2px solid var(--accent); border-right-color: transparent; animation: twspin .8s linear infinite; flex: none; }
    @keyframes twspin { to { transform: rotate(360deg); } } @media (prefers-reduced-motion: reduce) { .spin { animation: none; } }
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
    .topos { position: absolute; top: 46px; left: 12px; z-index: 30; width: 600px; max-width: calc(100vw - 40px); white-space: normal; max-height: 60vh; overflow: auto; padding: 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); box-shadow: 0 10px 30px rgba(0,0,0,.35);
      .row2 { display: flex; gap: 8px; align-items: center; margin-bottom: 10px; } .sp { flex: 1; }
      .ti { display: flex; gap: 6px; align-items: center; padding: 8px 10px; border: 1px solid var(--line); border-radius: 8px; margin-bottom: 6px; &.cur { border-color: var(--accent); }
        .tn { flex: 1; min-width: 0; display: flex; flex-direction: column; small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } } .x { padding: 2px 6px; } } }
    .chip.fs { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; margin-right: 6px; }
    .chip.fs input { margin: 0; }
    .zone { position: absolute; border: 1.5px dashed; border-radius: 14px; pointer-events: none; }
    .zone .zh { display: flex; gap: 7px; align-items: center; padding: 6px 10px; font-size: 11.5px; white-space: nowrap; }
    .zone .zh i, .hd { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 6px; }
    .zone .zh i { margin: 0; }
    .lk.tun .ln:not(.dn) { stroke: #a46cf0; stroke-dasharray: 7 5; }
    .hsel { margin-right: 4px; }
    .pico { flex: none; margin: -4px 0; }
    .okt { color: var(--success, #1fa45a); } .badt { color: var(--warning, #e39b17); } .addh { width: 100%; margin-top: 6px; }
    .remap { border: 1px solid var(--accent); border-radius: 10px; padding: 10px; margin: 6px 0 8px; background: var(--accent-soft); display: grid; gap: 6px; }
    .remap p { margin: 0; } .rmr { display: grid; grid-template-columns: minmax(0, 1fr) auto minmax(0, 2fr); gap: 8px; align-items: center; } .rmr select { min-width: 0; width: 100%; } .remap .row2 { margin: 2px 0 0; }
    .badge.up { border-color: var(--accent); color: var(--accent); }
    .menu .roles.looks { grid-template-columns: repeat(5, 40px); border-top: 1px dashed var(--line); padding-top: 6px; }
    .looksel { margin-left: 6px; font-size: 12px; max-width: 170px; }
    .lookcards { display: flex; flex-wrap: wrap; gap: 8px; justify-content: center; margin-top: 4px; pointer-events: auto;
      a { display: flex; flex-direction: column; align-items: center; gap: 2px; width: 92px; padding: 6px; border: 1px solid var(--line); border-radius: 10px; font-size: 11px; cursor: pointer; text-align: center; }
      a.on { border-color: var(--accent); background: var(--accent-soft); } }
    .menu .roles { display: grid; grid-template-columns: repeat(4, 40px); gap: 2px; padding: 2px 8px 6px; }
    .menu .roles a { padding: 2px; border-radius: 6px; text-align: center; } .menu .roles a.on { background: var(--accent-soft); outline: 1px solid var(--accent); }
    .menu .mh2 { display: block; padding: 4px 12px 2px; font-size: 11px; color: var(--muted); }
    .ntip { position: absolute; z-index: 9; min-width: 250px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); box-shadow: 0 10px 30px rgba(0,0,0,.35); pointer-events: none; font-size: 12px;
      .th { margin-bottom: 6px; white-space: nowrap; } table { border-collapse: collapse; } td { padding: 2px 10px 2px 0; white-space: nowrap; } td:first-child { color: var(--muted); } }
    .inv { overflow: auto; padding: 0 14px 10px; table { width: 100%; border-collapse: collapse; } th, td { text-align: left; padding: 5px 10px; border-bottom: 1px solid var(--line); font-size: 12px; white-space: nowrap; }
      th { color: var(--muted); font-weight: 500; position: sticky; top: 0; background: var(--surface); z-index: 1; }
      tr.h { cursor: pointer; } tr.h td { background: var(--surface-2); font-size: 12.5px; padding-top: 7px; padding-bottom: 7px; border-top: 6px solid var(--surface); } tr.h:hover td { background: color-mix(in srgb, var(--accent) 8%, var(--surface-2)); }
      tr.h .hd { margin: 0 6px 0 2px; } tr.h b { margin-right: 6px; } .chev { display: inline-block; width: 14px; color: var(--muted); }
      .pill { display: inline-block; margin: 0 6px 0 2px; padding: 0 7px; border-radius: 9px; border: 1px solid var(--line); font-size: 11px; color: var(--muted); }
      tr.d { cursor: pointer; } tr.d td:first-child { padding-left: 28px; position: relative; } .guide { position: absolute; left: 16px; top: 0; bottom: 0; border-left: 2px solid; opacity: .6; }
      tr.on td { background: var(--accent-soft); }
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
    .nbox { margin: 0 auto; width: ${BW}px; height: ${BH}px; border-radius: 12px; border: 1.5px solid transparent; display: flex; align-items: flex-end; justify-content: center; position: relative;
      app-dev-icon { margin-bottom: -6px; } }
    .node:hover .nbox { border-color: var(--line); }
    .node.sel .nbox { border-color: var(--accent); border-style: dashed; background: var(--accent-soft); } .node.pick .nbox { border-color: var(--accent); border-style: dashed; }
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
    .clamp { display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; } .evl { display: block; margin-top: 2px; font-size: 11.5px; color: var(--accent); cursor: pointer; }
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
    const t = this.topos()?.find(x => x.name === name), mine = new Set((this.st()?.hosts ?? []).filter(h => !(h.info.problems ?? []).length).map(h => h.session));
    const away = (t?.hosts ?? []).filter(h => !mine.has(h));
    if (away.length) { // its hosts are not all here: choose where their switches go
      const best = this.hostChoices()[0]?.session ?? '';
      this.remap.set({ name, start, rows: (t?.hosts ?? []).map(h => ({ from: h, to: mine.has(h) ? h : best })) });
      return;
    }
    if (this.running() && !(await this.ui.confirm(tr('Open {0}', name), tr('The running switches of this topology are stopped first; it stays saved.'), tr('Stop and Open')))) return;
    if (await this.lab.openTopology(name, start)) { this.topos.set(null); void this.ui.refreshTree(); }
  }
  protected remap = signal<{ name: string; start: boolean; rows: { from: string; to: string }[] } | null>(null);
  /** Your checked hosts, most free memory first, with what they have. */
  protected hostChoices = computed(() => (this.st()?.hosts ?? []).filter(h => !(h.info.problems ?? []).length).sort((a, b) => (b.info.memFree || 0) - (a.info.memFree || 0))
    .map(h => ({ session: h.session, label: (h.name || this.hname(h.session)) + ' · ' + (h.info.cpus ?? '?') + ' vCPU · ' + this.gb(h.info.memFree) + ' ' + tr('free') + ' · ' + this.gb(h.info.diskFree) + ' ' + tr('disk') })));
  async openRemapped() {
    const rm = this.remap(); if (!rm) return;
    const map: Record<string, string> = {};
    for (const r of rm.rows) if (r.to && r.to !== r.from) map[r.from] = r.to;
    if (this.running() && !(await this.ui.confirm(tr('Open {0}', rm.name), tr('The running switches of this topology are stopped first; it stays saved.'), tr('Stop and Open')))) return;
    if (await this.lab.openTopology(rm.name, rm.start, map)) { this.remap.set(null); this.topos.set(null); void this.ui.refreshTree(); }
  }
  async dupTopo(name: string) {
    const n = await this.ui.input(tr('Duplicate {0}', name), tr('Name of the copy (a lab of its own: its switches boot from their images)'), name + ' copy'); if (!n) return;
    try { this.topos.set(await this.backend.emuDuplicateTopology(name, n)); } catch (e) { this.ui.error(e); }
  }
  async renTopo(name: string) {
    const n = await this.ui.input(tr('Rename {0}', name), tr('Name'), name); if (!n || n === name) return;
    try { this.topos.set(await this.backend.emuRenameTopology(name, n)); await this.lab.refresh(); } catch (e) { this.ui.error(e); }
  }
  async exportTopo(name: string) {
    try { const p = await this.backend.chooseSaveFile(tr('Export Topology'), name + '.twlab', [{ displayName: 'Termwright lab topology', pattern: '*.twlab' }]); if (!p) return; await this.backend.emuExportTopology(name, p); this.ui.toast(tr('Exported to {0}', p)); } catch (e) { this.ui.error(e); }
  }
  async importTopo() {
    try { const p = await this.backend.chooseOpenFile(tr('Import Topology'), [{ displayName: 'Termwright lab topology', pattern: '*.twlab;*.json' }]); if (!p) return; this.topos.set(await this.backend.emuImportTopology(p)); } catch (e) { this.ui.error(e); }
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
  /** Hosts whose switches the Inventory hides (all shown at first). */
  protected invClosed = signal(new Set<string>());
  flipHost(id: string) { const n = new Set(this.invClosed()); n.has(id) ? n.delete(id) : n.add(id); this.invClosed.set(n); }
  flipAll() { this.invClosed.set(this.invClosed().size === this.inventory().length ? new Set() : new Set(this.inventory().map(g => g.id))); }
  protected inventory = computed(() => {
    const v = this.st(), u = v?.usage, devs = this.devs();
    if (this.multi()) return this.hostUse().map(h => {
      const ds = devs.filter(d => this.hostOfDev(d) === h.host), used = h.memTotal - h.memFree;
      return { id: h.host, host: h.name, kvm: this.mgRows().find(r => r.host === h.host)?.kvm ?? '', addr: (h.addr || '').split('/')[0], mgmt: this.mgName(h), devs: ds, running: ds.filter(d => d.running).length,
        cpuPct: this.pct(h.cores, h.cpus), cpuText: h.cores.toFixed(1) + ' / ' + (h.cpus || '?') + ' ' + tr('cores'), memPct: this.pct(used, h.memTotal), memText: this.gb(used) + ' / ' + this.gb(h.memTotal) };
    });
    const used = (u?.hostMemTotal ?? 0) - (u?.hostMemFree ?? 0);
    return [{ id: v?.host ?? '', host: this.hostName(), kvm: v?.kvm ? 'KVM' : tr('no KVM'), addr: v?.hostInfo?.lanAddr?.split('/')[0] ?? '', mgmt: this.mgName({ mgmt: v?.mgmt ?? '', mgmtNet: v?.mgmtNet ?? '', lanFixed: v?.lanFixed }), devs, running: devs.filter(d => d.running).length,
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
  /** Moves a switch to another host: live (running, no downtime), with its disk (saved state and configuration), or from its image. */
  async moveTo(d: LabDevice, host: string) {
    this.menu.set(null);
    const to = this.hname(host);
    const items = d.running
      ? [tr('Live: it keeps running, its memory and disk go across; a ping or two may be lost'), tr('Save, Copy and Resume: it is saved, its disk is copied and it resumes there (a minute or so off)'), tr('From Its Image: it boots there from day 0, its configuration stays behind')]
      : [tr('Copy Its Disk: its configuration (and saved state) go along'), tr('From Its Image: it boots there from day 0, its configuration stays behind')];
    const opts = d.running
      ? [{ label: tr('Live (No Downtime)'), value: 'live', primary: true }, { label: tr('Save, Copy and Resume'), value: 'copy' }, { label: tr('From Its Image'), value: 'image' }]
      : [{ label: tr('Copy Its Disk'), value: 'copy', primary: true }, { label: tr('From Its Image'), value: 'image' }];
    const how = await this.ui.choose(tr('Move {0} to {1}', d.hostname, to), tr('Its cables to switches on other hosts become UDP tunnels.'), items, opts);
    if (!how) return;
    if (how === 'image') await this.lab.moveVM(d.id, host);
    else await this.lab.migrate(d.id, host, how === 'live');
  }
  protected tunnels = computed(() => this.lines().filter(k => k.tun).length);
  protected hostLinks = signal<{ from: string; to: string; ok: boolean; text: string }[]>([]);
  protected checkingT = signal(false);
  async checkTunnels() { this.checkingT.set(true); try { this.hostLinks.set(await this.backend.emuCheckTunnels()); } catch (e) { this.ui.error(e); } finally { this.checkingT.set(false); } }
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
  protected mgOpen = signal(false);
  // the view of the topology: how hosts show, what labels show, the map
  private lsGet(k: string, d: string) { try { return localStorage.getItem(k) ?? d; } catch { return d; } }
  private lsPut(k: string, v: string) { try { localStorage.setItem(k, v); } catch { /* not kept */ } }
  protected hostView = signal<'auto' | 'areas' | 'tags' | 'off'>(this.lsGet('tw.lab.hostView', 'auto') as 'auto');
  protected showPorts = signal(this.lsGet('tw.lab.ports', '1') === '1');
  protected showAddr = signal(this.lsGet('tw.lab.addr', '1') === '1');
  protected miniOn = signal(this.lsGet('tw.lab.mini', '1') === '1');
  protected onlyProblems = signal(false);
  protected focusHost = signal('');
  protected scrollAt = signal({ x: 0, y: 0, w: 900, h: 600 });
  /** Areas round each host's switches for a few hosts, colour tags when there are more. */
  protected hostMode = computed(() => !this.multi() ? 'off' : this.hostView() === 'auto' ? (this.hostUse().length <= 3 ? 'areas' : 'tags') : this.hostView());
  setHostView(v: 'auto' | 'areas' | 'tags' | 'off') { this.hostView.set(v); this.lsPut('tw.lab.hostView', v); if (v !== 'tags') this.focusHost.set(''); }
  flip(sig: { (): boolean; set(v: boolean): void }, key: string) { sig.set(!sig()); this.lsPut(key, sig() ? '1' : '0'); }
  problem(d: LabDevice) { const s = this.stateOf(d); return s === 'bad' || s === 'boot' || !!this.warnOf(d) || !!d.personaError; }
  dimOf(d: LabDevice) { return (!!this.focusHost() && this.hostOfDev(d) !== this.focusHost()) || (this.onlyProblems() && !this.problem(d)); }
  linkDim(k: LabLink) {
    const a = this.devs().find(x => x.id === k.a), b = this.devs().find(x => x.id === k.b);
    if (this.focusHost() && !(a && this.hostOfDev(a) === this.focusHost()) && !(b && this.hostOfDev(b) === this.focusHost())) return true;
    return this.onlyProblems() && !(k.up === false || (a && this.problem(a)) || (b && this.problem(b)));
  }
  fill(cv: HTMLElement) { const a = this.area(); this.zoom.set(Math.max(0.4, Math.min(2, +(cv.clientWidth / a.w).toFixed(2)))); }
  onScroll(cv: HTMLElement) { this.scrollAt.set({ x: cv.scrollLeft, y: cv.scrollTop, w: cv.clientWidth, h: cv.clientHeight }); }
  /** The small map: every switch as a dot in its host's colour, the hosts' areas, and the part in view. */
  protected mini = computed(() => {
    const a = this.area(), z = this.zoom(), v = this.scrollAt(), W = 180, s = W / a.w, H = Math.max(60, Math.min(130, a.h * s));
    const k = Math.min(s, H / a.h);
    this.moved();
    return { w: W, h: H,
      dots: this.devs().map(d => { const p = this.pos(d); return { id: d.id, x: p.x * k, y: p.y * k, c: this.multi() ? this.hcol(this.hostOfDev(d)) : this.problem(d) ? '#f07a3a' : '#8f94a0' }; }),
      zones: this.hostMode() === 'areas' ? this.zones().map(q => ({ host: q.host, x: q.x * k, y: q.y * k, w: q.w * k, h: q.h * k, c: q.c })) : [],
      vx: v.x / z * k, vy: v.y / z * k, vw: Math.min(W, v.w / z * k), vh: Math.min(H, v.h / z * k) };
  });
  miniGo(ev: MouseEvent, cv: HTMLElement) {
    const r = (ev.currentTarget as HTMLElement).getBoundingClientRect(), a = this.area(), k = Math.min(180 / a.w, (this.mini().h) / a.h);
    cv.scrollTo({ left: ((ev.clientX - r.left - 6) / k) * this.zoom() - cv.clientWidth / 2, top: ((ev.clientY - r.top - 6) / k) * this.zoom() - cv.clientHeight / 2, behavior: 'smooth' });
  }
  /** Each host's switches together, hosts side by side (three to a row). */
  async arrangeByHost() {
    if (!(await this.ui.confirm(tr('Arrange by Host'), tr('Each host’s switches are put together, the hosts side by side. Cables stay as they are.'), tr('Arrange')))) return;
    const hosts = this.hostUse().map(u => u.host); let bx = 0, by = 0, rowH = 0, col = 0;
    const m = new Map(this.moved());
    for (const h of hosts) {
      const ds = this.devs().filter(d => this.hostOfDev(d) === h); if (!ds.length) continue;
      const cols = Math.max(1, Math.ceil(Math.sqrt(ds.length))), rows = Math.ceil(ds.length / cols);
      ds.forEach((d, i) => { const p = { x: 140 + bx + (i % cols) * 180, y: 130 + by + Math.floor(i / cols) * 160 }; m.set(d.id, p); this.lab.place(d.id, p.x - 75, p.y - BH / 2); });
      bx += cols * 180 + 120; rowH = Math.max(rowH, rows * 160); col++;
      if (col === 3) { col = 0; bx = 0; by += rowH + 110; rowH = 0; }
    }
    this.moved.set(m);
  }
  /** Each host's management, for the header menu and the status bar. */
  protected mgRows = computed(() => {
    const v = this.st(), main = v?.host ?? '';
    const short: Record<string, string> = { cloud: tr('routed'), lan: tr('bridged'), nat: 'NAT', bridge: tr('lab bridge'), forward: tr('port forwarding') };
    const rows = this.hostUse().length ? this.hostUse() : [{ host: main, name: this.hostName(), mgmt: v?.mgmt ?? '', mgmtNet: v?.mgmtNet ?? '', lanFixed: !!v?.lanFixed } as EmuHostUse];
    return rows.map(u => {
      const info = u.host === main ? v?.hostInfo : (v?.hosts ?? []).find(x => x.session === u.host)?.info;
      const kvm = (u.host === main ? !!v?.kvm : info?.kvm === 'yes') ? 'KVM' : tr('no KVM');
      const us = this.st()?.usage, one = !this.hostUse().length;
      return { host: u.host, name: u.name || this.hname(u.host), c: this.hcol(u.host), mgmt: u.mgmt, kvm,
        switches: one ? this.devs().length : u.switches, running: one ? this.running() : u.running, vcpus: one ? us?.vcpus ?? 0 : u.vcpus, cpus: one ? us?.hostCpus ?? 0 : u.cpus,
        ram: one ? us?.ram ?? 0 : u.ram, memTotal: one ? us?.hostMemTotal ?? 0 : u.memTotal, memFree: one ? us?.hostMemFree ?? 0 : u.memFree, load: one ? us?.load ?? 0 : u.load, short: short[u.mgmt] ?? u.mgmt, long: this.mgName(u),
        addr: u.mgmt === 'lan' && !u.lanFixed ? 'DHCP' : (u.mgmtNet || '').split('/')[0] || tr('Addresses') };
    });
  });
  protected mgSummary = computed(() => { const r = this.mgRows(); if (!r.length) return ''; if (r.length === 1) return r[0].name + ' · ' + r[0].short; const t = this.totals(); return tr('{0} hosts', r.length) + (t?.c ? ' · ' + tr('{0} % vCPU', Math.round(100 * t.v / t.c)) : ''); });
  /** What is going on now, for the status bar: the first busy switch (and how many more). */
  protected busyLine = computed(() => {
    const b = this.devs().filter(d => /moving|starting|booting|saving|copying|resuming|stopping/i.test(d.stateText || d.state || ''));
    return b.length ? b[0].hostname + ': ' + tr(b[0].stateText || b[0].state || '') + (b.length > 1 ? ' · ' + tr('{0} more', b.length - 1) : '') : '';
  });
  protected totals = computed(() => {
    const u = this.hostUse(); if (!u.length) return null;
    return { v: u.reduce((a, x) => a + (x.vcpus || 0), 0), c: u.reduce((a, x) => a + (x.cpus || 0), 0), r: u.reduce((a, x) => a + (x.ram || 0), 0), m: u.reduce((a, x) => a + (x.memTotal || 0), 0) };
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
  protected readonly roles = ROLES;
  protected readonly looks = ICON_STYLES;
  protected defLook = computed(() => (this.st()?.iconStyle || 'box') as IconStyle);
  lookOf(d: LabDevice): IconStyle { return (d.iconStyle || this.defLook()) as IconStyle; }
  kindOf(d: LabDevice) { return d.persona?.slots?.[0]?.model || d.model || tr('this image'); }
  async setLook(d: LabDevice, k: IconStyle) { this.menu.set(null); try { await this.backend.emuSetIconStyle(d.id, k === this.defLook() ? '' : k); await this.lab.refresh(); } catch (e) { this.ui.error(e); } }
  async setDefLook(k: string) { try { await this.backend.emuSetIconStyle('', k); await this.lab.refresh(); } catch (e) { this.ui.error(e); } }
  async uplink(d: LabDevice) {
    this.menu.set(null);
    let free: string[] = [];
    try { free = (await this.backend.emuFreePorts(d.id)) ?? []; } catch { /* none */ }
    const p = await this.ui.input(tr('Port on the Host’s Network'), tr('Data port of {0} to put on the host’s network (free: {1})', d.hostname, free.slice(0, 12).join(', ') + (free.length > 12 ? '…' : '')), free[free.length - 1] ?? '');
    if (p) await this.setUplink(d, p.trim(), true);
  }
  async setUplink(d: LabDevice, port: string, on: boolean) {
    this.menu.set(null);
    try { await this.backend.emuSetUplink(d.id, port, on); await this.lab.refresh(); this.ui.toast(on ? tr('{0} {1} goes on the host’s network when it starts (or restarts)', d.hostname, port) : tr('{0} {1} comes off the host’s network when it restarts', d.hostname, port)); } catch (e) { this.ui.error(e); }
  }
  palRole(p: Pal): DevRole { return p.cls === 'l' ? 'server' : roleOf((p.tpl?.model || '') + ' ' + p.name, p.os); }
  async setRole(d: LabDevice, r: DevRole) { this.menu.set(null); try { await this.backend.emuSetRole(d.id, r === roleOf(d.persona?.slots?.[0]?.model || d.model, d.os, d.image ?? '') ? '' : r); await this.lab.refresh(); } catch (e) { this.ui.error(e); } }
  /** What the switch is drawn as: the role chosen in Configure…, else one from its model. */
  roleOfDev(d: LabDevice): DevRole { return (d.role as DevRole) || roleOf(d.persona?.slots?.[0]?.model || d.model, d.os, d.image ?? ''); }
  iconState(d: LabDevice): 'run' | 'boot' | 'off' | 'idle' { const s = this.stateOf(d); return s === 'on' ? 'run' : s === 'boot' ? 'boot' : 'off'; }
  stateOf(d: LabDevice): 'on' | 'boot' | 'bad' | 'off' {
    if (!this.emu()) return d.running ? 'on' : 'off';
    // green: up (a problem shows as a "!" beside it), orange: on its way, red: down or failed
    if (d.state === 'failed') return 'bad';
    if (d.state === 'running') return 'on';
    if (d.state === 'stopped' || !d.state) return 'off';
    return 'boot';
  }
  /** The problem of a switch that is up ("" = none): it stays green, with a "!". */
  /** A message cut to its first part: what happened, not the switch's output after it (the whole text is in the tooltip and the Events log). */
  brief(t: string) {
    let s = t.replace(/\s*\([^()]*\)/g, '').replace(/:\s*"[^"]*"\s+(does|did|is|was|has)\b/, ': it $1').replace(/\s+-\s+(it printed|see|back to|read|the switch printed|the console showed).*$/i, '');
    const cut = s.search(/\s+-\s+|;\s/); if (cut > 30) s = s.slice(0, cut);
    s = s.trim(); return s ? s[0].toUpperCase() + s.slice(1) : t;
  }
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
      // templates (an image with resources and a persona model) are device types too
      for (const t of this.st()?.templates ?? []) {
        const im = (this.st()?.images ?? []).find(i => i.id === t.image);
        if (!im) continue;
        const dt = { key: 'tpl:' + t.id, model: t.model, kind: im.name, osName: '', images: [im.id], cpus: t.cpus, ram: t.ram } as unknown as EmuDeviceType;
        items.push({ key: dt.key, name: t.name, sub: [t.model, t.module, (t.cpus || im.cpus) + ' vCPU', this.gb(t.ram || im.ram)].filter(Boolean).join(' · '), group: tr('Templates'), ver: im.version ?? '', os: im.deviceType ?? im.name, cls: '', dt, tpl: t });
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
          const made = await this.lab.addEmu({ host, groups: [{ image: p.dt!.images[0], model: p.dt!.model, module: p.tpl?.module ?? '', template: p.tpl?.id, diskGb: p.tpl?.diskGb, count: 1, names: n0, serialStart: s0?.serial ?? '', macStart: s0?.mac ?? '', cpus: p.dt!.cpus, ram: p.dt!.ram }], cabling: 'none', mgmt: '', mgmtNet: '', start: false });
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
      ok = await this.lab.addEmu({ host: e.host, groups: [{ image: p.dt!.images[0], model: p.dt!.model, module: p.tpl?.module ?? '', template: p.tpl?.id, diskGb: p.tpl?.diskGb, count: 1, names, serialStart: s?.serial ?? '', macStart: s?.mac ?? '', cpus: p.dt!.cpus, ram: p.dt!.ram }], cabling: 'none', mgmt: '', mgmtNet: '', start: false });
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
      // the layout (fan, labels, rings) keeps to the boxes; the cables go on to where the drawings are
      const A0 = this.edge(pa, pb), B0 = this.edge(pb, pa), A1 = this.edge(pa, pb, p), B1 = this.edge(pb, pa, q);
      const out1 = (c: { x: number; y: number }, x: { x: number; y: number }, y: { x: number; y: number }) => Math.hypot(x.x - c.x, x.y - c.y) >= Math.hypot(y.x - c.x, y.y - c.y);
      const A = out1(pa, A1, A0) ? A1 : A0, B = out1(pb, B1, B0) ? B1 : B0;
      const dA = Math.hypot(A.x - A1.x, A.y - A1.y), dB = Math.hypot(B.x - B1.x, B.y - B1.y);
      const L = Math.hypot(B.x - A.x, B.y - A.y) || 1, u = { x: (B.x - A.x) / L, y: (B.y - A.y) / L }, n = { x: -u.y, y: u.x };
      const reach = (d: string) => {
        const m = /^M(-?[\d.]+) (-?[\d.]+)/.exec(d), z = /(-?[\d.]+) (-?[\d.]+)$/.exec(d);
        if (m && dA) { const x = +m[1], y = +m[2]; if (Math.abs((x - A.x) * u.x + (y - A.y) * u.y) < .6) d = 'M' + P({ x: x - u.x * dA, y: y - u.y * dA }) + ' L' + d.slice(1); }
        if (z && dB) { const x = +z[1], y = +z[2]; if (Math.abs((x - B.x) * u.x + (y - B.y) * u.y) < .6) d += ' L' + P({ x: x + u.x * dB, y: y + u.y * dB }); }
        return d;
      };
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
        item.paths.forEach(x => x.d = reach(x.d));
        if (tun) item.tip = tr('UDP tunnel {0} ⇄ {1}', this.hname(this.hostOfDev(p)), this.hname(this.hostOfDev(q))) + ' · ' + item.tip;
        out.push(item);
      });
    }
    return out;
  });
  /** Where a cable from p towards q meets the device drawn at p: its outline (measured per look and role), else its box. */
  private edge(p: { x: number; y: number }, q: { x: number; y: number }, d?: LabDevice) {
    const dx = q.x - p.x, dy = q.y - p.y, L = Math.hypot(dx, dy) || 1;
    const look = d ? this.lookOf(d) : null;
    // the icon (size 100, so 100/130 px a unit) sits at the bottom of its box, 6 px lower: the node's centre in drawing units
    const prof = look ? iconReach(look, this.roleOfDev(d!), look === 'iso' ? 65 : 60, 48.6) : null;
    if (prof) {
      const deg = ((Math.atan2(dy, dx) * 180 / Math.PI) + 360) % 360, i = Math.floor(deg / 5), f = deg / 5 - i;
      const r = (prof[i % 72] * (1 - f) + prof[(i + 1) % 72] * f) * 100 / 130 - 1;
      const s = Math.min(Math.max(r, 0) / L, 1);
      return { x: p.x + dx * s, y: p.y + dy * s };
    }
    const s = Math.min(Math.abs((BW / 2 + 4) / (dx || 1e-6)), Math.abs((BH / 2 + 4) / (dy || 1e-6)));
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
    if (g?.moved) { const p = this.moved().get(g.id)!; this.lab.place(g.id, p.x - 75, p.y - BH / 2); this.dropOnHost(g.id, p); }
  }
  /** A switch dropped in another host's area moves there (when it is stopped). */
  private dropOnHost(id: string, p: { x: number; y: number }) {
    const d = this.devs().find(x => x.id === id); if (!d || !this.multi()) return;
    const z = this.zones().find(z => z.host !== this.hostOfDev(d) && p.x >= z.x && p.x <= z.x + z.w && p.y >= z.y && p.y <= z.y + z.h);
    if (!z) return;
    void this.moveTo(d, z.host);
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
  protected revertMgmt() { const h = this.mg().host; const el = [...this.host.nativeElement.querySelectorAll<HTMLSelectElement>('select[data-h]')].find(x => x.dataset['h'] === h); if (el) el.value = this.mg().mgmt; }
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
