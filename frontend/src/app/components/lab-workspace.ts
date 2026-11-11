import { Component, HostListener, OnDestroy, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { LabService } from '../core/lab.service';
import { Backend } from '../core/backend';
import { EmuDeviceType, LabDevice, LabLink, LabModel } from '../core/models';
import { TabView, Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { LabConfigure, LabHosts, LabImages, LabNew } from './lab-pages';

const BW = 104, BH = 46;
type Pal = { key: string; name: string; sub: string; group: string; ver: string; os: string; cls: string; dt?: EmuDeviceType; m?: LabModel };

/** The lab as a workspace tab: device palette, canvas with cables, the
 *  selected switch, the topology summary and the Events log. */
@Component({
  selector: 'app-lab-workspace',
  imports: [FormsModule, LabConfigure, LabHosts, LabImages, LabNew],
  template: `
  <div class="ws" (click)="menu.set(null)" [style.grid-template-rows]="'auto minmax(0, 1fr) ' + dockH() + 'px'">
    <div class="tb">
      <b class="lname">{{ emu() ? tr('Emulated Lab') : tr('Simulated Lab') }}</b>
      @if (emu()) { <span class="chip" [title]="tr('KVM host')"><i class="d" [class.ok]="!!st()?.host"></i>{{ hostName() || tr('No host yet') }}@if (st()?.kvm) { · KVM }</span> }
      @else { <span class="chip" [title]="tr('The device pack the simulated switches come from')">{{ st()?.pack ? st()!.pack!.name + ' ' + st()!.pack!.version : tr('No device pack') }}</span>
        <button type="button" (click)="loadPack()">{{ st()?.pack ? tr('Load Another Pack…') : tr('Load Device Pack (.vm)…') }}</button> }
      <span class="sep"></span>
      <button type="button" class="primary" (click)="lab.start([])" [disabled]="!devs().length || lab.busy()">▶ {{ tr('Start All') }}</button>
      <button type="button" (click)="lab.stop([])" [disabled]="!running() || lab.busy()">■ {{ tr('Stop All') }}</button>
      @if (emu()) { <button type="button" (click)="sel() && lab.restart([sel()])" [disabled]="!selDev()?.running || lab.busy()" [title]="tr('Restart the selected switch')">⟳ {{ tr('Reload') }}</button> }
      <span class="sep"></span>
      <button type="button" [class.on]="linkMode()" (click)="toggleLink(); $event.stopPropagation()" [disabled]="devs().length < 2">⟷ {{ tr('Add Link') }}</button>
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
        <span class="chip mg" [title]="tr('How the switches’ first port (management) is connected; it applies when they start')">{{ tr('Management') }}
          <select [ngModel]="st()?.mgmt" (ngModelChange)="setMgmt($event)" name="wmg" [disabled]="lab.busy()">
            <option value="cloud">{{ tr('The host’s network, routed (cloud)') }}</option>
            <option value="lan">{{ tr('The host’s network (bridged)') }}</option>
            <option value="nat">{{ tr('The host’s NAT network') }}</option>
            <option value="bridge">{{ tr('Lab bridge on the host') }}</option>
            <option value="forward">{{ tr('Port forwarding on the host') }}</option>
          </select>@if (st()?.mgmt === 'cloud') { <button type="button" class="sm" (click)="openCloud(); $event.stopPropagation()" [title]="tr('Addresses of the switches on the host’s network')">{{ st()?.mgmtNet?.split('/')?.[0] }}…</button> }
          @if (cloudF(); as f) {
            <div class="cloudf" (click)="$event.stopPropagation()">
              <b>{{ tr('The host’s network, routed (cloud)') }}</b>
              <p class="muted small">{{ tr('Like GNS3’s cloud: the switches get fixed addresses on the host’s network {0} and can reach and be reached from it (SSH, HTTP/HTTPS, FTP, TFTP, SCP…). The host answers for their addresses and forwards for them, so only the host’s own MAC address is seen there: nothing has to be allowed on a VMware or Hyper-V virtual switch, and Wi-Fi works.', f.net) }}</p>
              <label>{{ tr('First switch’s address') }}</label><input class="mono" [(ngModel)]="f.addr" name="cfa" placeholder="10.0.0.146" />
              <label>{{ tr('Gateway') }}</label><input class="mono" [(ngModel)]="f.gw" name="cfg" />
              <p class="muted small">{{ tr('The next switches take the addresses after it ({0} switches: up to {1}). Pick free addresses: no DHCP server may hand them out.', devs().length, lastAddr(f.addr)) }}</p>
              <div class="row2"><span class="sp"></span><button type="button" (click)="cloudF.set(null)">{{ tr('Cancel') }}</button><button type="button" class="primary" (click)="applyCloud()" [disabled]="!f.addr.trim()">{{ tr('Use') }}</button></div>
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
            <svg class="links" [attr.width]="area().w" [attr.height]="area().h">
              @for (k of lines(); track k.key) {
                <g class="lk" [class.down]="k.down" [class.selk]="selLink() === k.key" (click)="pickLink(k, $event)" (contextmenu)="linkMenu(k, $event)">
                  <line class="hit" [attr.x1]="k.x1" [attr.y1]="k.y1" [attr.x2]="k.x2" [attr.y2]="k.y2" />
                  <line class="ln" [attr.x1]="k.x1" [attr.y1]="k.y1" [attr.x2]="k.x2" [attr.y2]="k.y2" [class.up]="k.up" />
                  <rect class="pb" [attr.x]="k.p1.x - k.w1 / 2" [attr.y]="k.p1.y - 9" [attr.width]="k.w1" height="18" rx="5" /><text class="pt" [attr.x]="k.p1.x" [attr.y]="k.p1.y + 4">{{ k.aPort }}</text>
                  <rect class="pb" [attr.x]="k.p2.x - k.w2 / 2" [attr.y]="k.p2.y - 9" [attr.width]="k.w2" height="18" rx="5" /><text class="pt" [attr.x]="k.p2.x" [attr.y]="k.p2.y + 4">{{ k.bPort }}</text>
                </g>
              }
              @for (g of ghostLines(); track $index) { <line class="gl" [attr.x1]="g.x1" [attr.y1]="g.y1" [attr.x2]="g.x2" [attr.y2]="g.y2" /> }
              @if (rubber(); as r) { <line class="rb" [attr.x1]="r.x1" [attr.y1]="r.y1" [attr.x2]="r.x2" [attr.y2]="r.y2" /> }
            </svg>
            @for (g of ghosts(); track g.name) { <div class="node ghost" [style.left.px]="g.x - 75" [style.top.px]="g.y - BH / 2"><div class="nbox"><span class="ico"></span></div><div class="nn">{{ g.name }}</div><div class="nm">{{ tr('new') }}</div></div> }
            @for (d of devs(); track d.id) {
              <div class="node" [class.sel]="sel() === d.id" [class.pick]="linkA()?.id === d.id" [style.left.px]="pos(d).x - 75" [style.top.px]="pos(d).y - BH / 2"
                   (pointerdown)="grab($event, d, cv)" (click)="clickNode(d, $event)" (dblclick)="open(d)" (contextmenu)="nodeMenu(d, $event)">
                <div class="nbox" [class.dim]="stateOf(d) === 'off'"><span class="ico" [class]="'ico ' + icoOf(d)"></span><i class="st" [class]="'st ' + stateOf(d)" [title]="tr(d.stateText || (d.running ? 'running' : 'stopped'))"></i>@if (warnOf(d)) { <i class="wn" [title]="warnOf(d)">!</i> }</div>
                <div class="nn">{{ d.hostname }}</div>
                <div class="nm">{{ d.persona?.slots?.[0]?.model || d.model }} · {{ subOf(d) }}</div>
                @if (d.personaError) { <span class="badge r" [title]="d.personaError">{{ tr('no persona') }}</span> }
                @else if (d.persona && d.personaOk) { <span class="badge">{{ tr('persona') }} ✓</span> }
                @else if (d.error) { <span class="badge r" [title]="d.error">{{ tr('problem') }}</span> }
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
              <a (click)="mdo('stop', d)" [class.off]="!d.running">■ {{ tr('Stop') }}</a>
              @if (emu()) { <a (click)="mdo('restart', d)" [class.off]="!d.running">⟳ {{ tr('Reload') }}</a> }
              <hr />
              @if (emu()) { <a (click)="mdo('console', d)" [class.off]="!d.running">{{ tr('Console') }} <kbd>telnet :{{ d.console }}</kbd></a> }
              <a (click)="mdo('ssh', d)" [class.off]="!d.running">{{ emu() ? 'SSH' : tr('Open Session') }} <kbd>{{ emu() ? d.ssh : '127.0.0.1:' + d.port }}</kbd></a>
              <hr />
              <a (click)="mdo('configure', d)">{{ tr('Configure…') }}</a>
              <a (click)="mdo('link', d)">{{ tr('Add Link from Here') }}</a>
              @if (emu()) { <a (click)="mdo('reset', d)">{{ tr('Back to Day 0') }}</a> }
              <hr />
              <a class="r" (click)="mdo('remove', d)">{{ tr('Delete') }}</a>
            }
            @if (m.link; as k) {
              @if (emu()) { <a (click)="ldo('pull', k)">{{ k.down ? tr('Plug In') : tr('Pull') }}</a> }
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
            @if (d.persona) { <span>{{ tr('Persona') }}</span><span [class.r]="!!d.personaError" [class.g]="!d.personaError && d.personaOk">{{ d.personaError || (d.personaOk ? '✓ ' + tr('in the switch') : tr('not written yet')) }}</span> }
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
        @if (emu() && st()?.usage; as u) {
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
        @if (emu() && selDev(); as d) { <a [class.on]="dock() === 'console'" (click)="dock.set('console'); loadTail()">{{ tr('{0} console', d.hostname) }}</a> }
        <span class="sp"></span><span class="muted small hint2">{{ dock() === 'events' ? tr('Everything Termwright does to a switch shows here') : tr('The end of its console (read-only); open the console to type') }}</span>
        <button type="button" class="sm" (click)="copyDock()" [title]="tr('Copy to the clipboard')">{{ tr('Copy') }}</button>
        @if (dock() === 'events' && emu()) { <button type="button" class="sm" (click)="clearEvents()">{{ tr('Clear') }}</button> }
        <button type="button" class="sm" (click)="dockH.set(dockH() > 260 ? 190 : 420)" [title]="tr('Taller or shorter')">{{ dockH() > 260 ? '▾' : '▴' }}</button>
      </div>
      @if (dock() === 'events') {
        <div class="ev">
          @for (e of events(); track $index) { <div [class]="e.level"><span class="t">{{ time(e.at) }}</span><b (click)="selectId(e.vm)">{{ e.name }}</b><span>{{ e.text }}</span></div> }
          @empty { <div><span class="t"></span><b></b><span class="muted">{{ emu() ? tr('Nothing yet - start a switch.') : tr('Simulated switches start at once; their state is on the canvas.') }}</span></div> }
        </div>
      } @else { <pre class="tail">{{ tail() || tr('No console output yet.') }}</pre> }
    </div>
  </div>`,
  styles: [`
    :host { display: block; height: 100%; }
    .ws { height: 100%; display: grid; background: var(--bg); color: var(--text); font-size: 13px; }
    .tb { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; padding: 8px 12px; border-bottom: 1px solid var(--line); background: var(--surface); white-space: nowrap;
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
      b, p, .row2 { grid-column: 1 / -1; margin: 0; } input { width: 100%; } .row2 { display: flex; gap: 8px; } .sp { flex: 1; } }
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
    .menu { position: absolute; z-index: 8; width: 220px; background: var(--surface); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 10px 30px rgba(0,0,0,.16); padding: 5px;
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
  `],
})
export class LabWorkspace implements OnDestroy {
  readonly tab = input.required<TabView>();
  readonly visible = input(true);
  protected lab = inject(LabService);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  protected ui = inject(Ui);
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
  protected dock = signal<'events' | 'console'>('events');
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
    const text = this.dock() === 'events' ? [...(this.st()?.events ?? [])].map(e => this.time(e.at) + '  ' + e.name + '  ' + e.level.toUpperCase() + '  ' + e.text).join('\n') : this.tail();
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
  @HostListener('document:keydown.escape') onEsc() { if (!this.visible()) return; if (this.cloudF()) { this.cloudF.set(null); return; } if (this.page()) { this.page.set(''); this.ghost.set(null); } else { this.cancelLink(); this.menu.set(null); } }
  ngOnDestroy() { clearInterval(this.timer); this.lab.watch('ws-' + this.tab().id, false); }

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
  protected lines = computed(() => {
    this.moved();
    const out = [];
    for (const k of this.links()) {
      const a = this.devs().find(d => d.id === k.a), b = this.devs().find(d => d.id === k.b);
      if (!a || !b) continue;
      const pa = this.pos(a), pb = this.pos(b);
      const e1 = this.edge(pa, pb), e2 = this.edge(pb, pa);
      const at = (t: number) => ({ x: e1.x + (e2.x - e1.x) * t, y: e1.y + (e2.y - e1.y) * t });
      out.push({ key: k.a + '/' + k.aPort, ...k, x1: e1.x, y1: e1.y, x2: e2.x, y2: e2.y, p1: at(0.18), p2: at(0.82), w1: k.aPort.length * 6.4 + 10, w2: k.bPort.length * 6.4 + 10, up: !!k.up, down: !!k.down });
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
      case 'restart': void this.lab.restart([d.id]); break;
      case 'console': this.console(d); break;
      case 'ssh': this.ssh(d); break;
      case 'configure': this.configure(d); break;
      case 'link': this.startLinkFrom(d); break;
      case 'reset': void this.reset(d); break;
      case 'remove': void this.remove(d); break;
    }
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
  console(d: LabDevice) { const p = this.lab.consoleOf(d.id); if (p) void this.tabs.openSession(p); }
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
  async loadTail() { const id = this.sel(); if (id && this.emu()) { const t = await this.lab.consoleTail(id); if (this.sel() === id) this.tail.set(t); } }
  protected cloudF = signal<{ addr: string; gw: string; net: string } | null>(null);
  openCloud() {
    const v = this.st(), h = v?.hostInfo;
    const net = h?.lanAddr ?? '';
    this.cloudF.set({ addr: v?.mgmt === 'cloud' ? (v?.mgmtNet ?? '').split('/')[0] : '', gw: v?.mgmt === 'cloud' ? v?.cloudGw ?? '' : h?.gateway ?? '', net: net ? net.replace(/\.\d+\//, '.0/') : tr('of the host') });
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
    await this.setMgmt('cloud', (f.addr.trim() + ' ' + f.gw.trim()).trim());
  }
  async setMgmt(m: string, net = '') {
    const was = this.st()?.mgmt ?? '';
    if (m === 'cloud' && !net) { this.lab.emu.set({ ...this.lab.emu()! }); this.openCloud(); return; }
    if (m === was && m !== 'cloud') return;
    const text: Record<string, string> = {
      lan: tr('The switches join the host’s network and get addresses from its DHCP server.'),
      nat: tr('The switches get addresses from the host’s NAT network (DHCP, like GNS3’s NAT node) and are reached through the host.'),
      bridge: tr('The switches get fixed addresses on a bridge of their own on the host, reached through the host.'),
      forward: tr('Each switch’s SSH is forwarded to a port on the host (no root needed).'),
      cloud: tr('The switches get the addresses from {0} on the host’s network, typed on their consoles at each start, and are reached directly.', net.split(' ')[0]),
    };
    const up = this.devs().filter(d => d.running).map(d => d.id);
    if (!up.length) {
      if (await this.ui.confirm(tr('Change management'), text[m] + ' ' + tr('Their sessions are updated; it applies when the switches start.'), tr('Change'))) await this.lab.setMgmt(m, net);
      else this.lab.emu.set({ ...this.lab.emu()! });
      return;
    }
    // running switches are stopped for it and started again
    if (!(await this.ui.confirm(tr('Change management'), text[m] + ' ' + tr('{0} switches are running: they are stopped, and started again with it (their configuration stays). Their sessions are updated.', up.length), tr('Stop, Change and Start')))) {
      this.lab.emu.set({ ...this.lab.emu()! });
      return;
    }
    if (await this.lab.stop(up) && await this.lab.setMgmt(m, net)) await this.lab.start(up);
    else this.lab.emu.set({ ...this.lab.emu()! });
  }
}
