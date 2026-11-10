import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { LabService } from '../core/lab.service';
import { EmuImage, EmuPersonaDef, EmuSlot, EmuTemplate, LabDevice, LabLink, LabOutput, LabPack, SessionNode } from '../core/models';
import { Backend } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Tabs } from '../core/tabs.service';
import { tr } from '../core/i18n';
import { Icon } from '../components/icon';

const NW = 150, NH = 46;

/** The simulated lab: its switches (list and topology), cables, start / stop. */
@Component({
  selector: 'app-lab-dialog',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop" (click)="ui.close()">
    <div class="modal labw" (click)="$event.stopPropagation()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="server" [size]="17" /></span>
        <span class="mh-t">{{ emu() ? tr('Emulated Lab') : tr('Simulated Lab') }}<span class="mh-s">{{ subtitle() }}</span></span>
        <span class="spacer"></span><select class="kindsel" [ngModel]="lab.kind()" (ngModelChange)="setKind($event)" name="kind" [attr.aria-label]="tr('Lab')"><option value="sim">{{ tr('Simulated lab (this computer)') }}</option><option value="emu">{{ tr('Emulated lab (KVM host)') }}</option></select></div>
      @if (consent(); as c) {
        <div class="modal-body consent">
          <div class="big">{{ tr('Load this device pack?') }}</div>
          <div class="kv wide"><span>{{ tr('Name') }}</span><b>{{ c.info.name }}</b><span>{{ tr('Version') }}</span><span>{{ c.info.version }}</span>
            <span>{{ tr('From') }}</span><span>{{ c.info.vendor }}</span><span>{{ tr('Contents') }}</span><span>{{ tr('{0} models', c.info.models) }} · {{ c.info.os.join(', ') }}</span>
            <span>{{ tr('Terms') }}</span><span class="terms">{{ c.info.license }}</span><span>SHA-256</span><span class="mono small">{{ c.info.hash }}</span></div>
          <p class="muted">{{ c.info.description }}</p>
          <p class="muted small">{{ tr('A device pack tells the simulator what its switches are: models, commands and outputs. It is kept on this computer only (in Termwright’s data folder), it is not sent anywhere, and you can remove it in the Lab window.') }}</p>
          <label class="chk"><input type="checkbox" [(ngModel)]="agree" name="agree" /> {{ tr('I am allowed to use this pack and agree to its terms') }}</label>
          <div class="btns" style="margin-top:14px"><button type="button" (click)="consent.set(null)">{{ tr('Cancel') }}</button><button type="button" class="primary" (click)="accept()" [disabled]="!agree || lab.busy()">{{ tr('Load Pack') }}</button></div>
        </div>
      } @else if (emu() && !lab.state()?.exists && !manage()) {
        <div class="modal-body empty">
          <div class="big">{{ tr('No emulated lab yet') }}</div>
          <p class="muted">{{ tr('The emulated lab boots the vendors’ own VM images (.qcow2) with QEMU/KVM on a Linux host you reach over SSH, so the real operating system runs - protocols, APIs and cloud agents included. Add the images you downloaded from the vendor, then make a lab.') }}</p>
          <div class="btns center"><button type="button" class="primary" (click)="wizard(false)" [disabled]="!images().length">{{ tr('New Lab…') }}</button>
            <button type="button" (click)="manage.set(true); tab.set('images')">{{ tr('Images') }} ({{ images().length }})…</button>
            <button type="button" (click)="manage.set(true); tab.set('hosts')">{{ tr('KVM Hosts') }} ({{ hosts().length }})…</button></div>
        </div>
      } @else if (!emu() && !lab.state()?.exists) {
        <div class="modal-body empty">
          @if (!pack() || pack()!.error) {
            <div class="big">{{ pack()?.error ? tr('The device pack could not be loaded') : tr('Load a device pack first') }}</div>
            @if (pack()?.error; as e) { <p class="err">{{ pack()!.name }}: {{ e }}</p> }
            @if (lab.state()?.waiting) { <p class="muted">{{ tr('{0} switches wait for it.', lab.state()!.waiting ?? 0) }}</p> }
            <p class="muted">{{ tr('The simulator runs switches from a device pack (.vm file): their models, commands and outputs. Termwright itself carries none - load a pack you are allowed to use. A few MB per switch, no virtual machines; not the real operating system, no protocols are run.') }}</p>
            <button type="button" class="primary" (click)="choosePack()">{{ tr('Load Device Pack (.vm)…') }}</button>
          } @else {
            <div class="big">{{ tr('No simulated lab yet') }}</div>
            <p class="muted">{{ tr('Device pack: {0} {1}', pack()!.name, pack()!.version) }} · <a (click)="choosePack()">{{ tr('Load another…') }}</a> · <a (click)="removePack()">{{ tr('Remove') }}</a></p>
            <button type="button" class="primary" (click)="wizard(false)">{{ tr('New Lab…') }}</button>
          }
        </div>
      } @else {
        <div class="ltabs" role="tablist">
          @if (emu()) {
            <button type="button" role="tab" [class.on]="tab() === 'hosts'" (click)="tab.set('hosts')"><span class="stepn">1</span>{{ tr('KVM Hosts') }} ({{ hosts().length }})</button>
            <button type="button" role="tab" [class.on]="tab() === 'images'" (click)="tab.set('images')"><span class="stepn">2</span>{{ tr('Images') }} ({{ images().length }})</button>
            <button type="button" role="tab" [class.on]="tab() === 'templates'" (click)="tab.set('templates')"><span class="stepn">3</span>{{ tr('Templates') }} ({{ templates().length }})</button>
            <button type="button" role="tab" [class.on]="tab() === 'list'" (click)="tab.set('list')"><span class="stepn">4</span>{{ tr('Switches') }} ({{ lab.devices().length }})</button>
          } @else { <button type="button" role="tab" [class.on]="tab() === 'list'" (click)="tab.set('list')">{{ tr('Switches') }} ({{ lab.devices().length }})</button> }
          <button type="button" role="tab" [class.on]="tab() === 'topo'" (click)="tab.set('topo')">{{ tr('Topology') }}</button>
          <button type="button" role="tab" [class.on]="tab() === 'cables'" (click)="tab.set('cables')">{{ tr('Cables') }} ({{ lab.links().length }})</button>
          @if (!emu()) { <button type="button" role="tab" [class.on]="tab() === 'outputs'" (click)="openOutputs()">{{ tr('Command Outputs') }} ({{ (lab.state()?.outputs ?? []).length }})</button> }
        </div>
        @if (emu() && usage(); as u) {
          <div class="hoststrip">
            <b>{{ hostName() || tr('No host yet') }}</b>
            <span [class.ok]="lab.state()?.kvm" [class.warn]="lab.state()?.kvm === false">{{ lab.state()?.kvm === false ? tr('no KVM: slow') : 'KVM' }}</span>
            <span>{{ tr('{0} of {1} switches running', u.running, lab.devices().length) }}</span>
            <span class="m">vCPU <b>{{ u.vcpus }}</b> / {{ u.hostCpus || '?' }}<i class="meter"><i [style.width.%]="pct(u.vcpus, u.hostCpus)"></i></i></span>
            <span class="m">RAM <b>{{ gb(u.ram) }}</b> / {{ gb(u.hostMemTotal) }} · {{ tr('{0} free', gb(u.hostMemFree)) }}<i class="meter"><i [style.width.%]="pct(u.hostMemTotal - u.hostMemFree, u.hostMemTotal)" [class.over]="u.hostMemFree < 1024"></i></i></span>
            <span class="m">CPU {{ tr('now') }} <b>{{ u.cores.toFixed(1) }}</b> {{ tr('cores') }} · {{ tr('load') }} {{ u.load }}</span>
            <span class="muted small">{{ tr('A stopped switch frees its vCPU and memory at once.') }}</span>
          </div>
        }
        <div class="modal-body">
          @if (tab() !== 'images' && tab() !== 'hosts') {
          <div class="bar">
            <span class="muted">{{ tr('Runs on') }}</span>
            <span class="where">{{ emu() ? (hostName() + ' - ' + tr('real VM images') + (lab.state()?.kvm === false ? ' (' + tr('no KVM: slow') + ')' : '')) : tr('This computer - simulated') }}</span>
            @if (lab.running() > 0) { <button type="button" class="primary" (click)="lab.stop()" [disabled]="lab.busy()">■ {{ tr('Stop Lab') }}</button> }
            @else { <button type="button" class="primary" (click)="lab.start()" [disabled]="lab.busy()">▶ {{ tr('Start Lab') }}</button> }
            <button type="button" (click)="wizard(true)">+ {{ tr('Add Switches') }}</button>
            <button type="button" [class.on]="cableMode()" (click)="toggleCable()" [title]="tr('Click two switches in the topology, or pick them here')">⟷ {{ tr('Cable') }}</button>
            <span class="spacer"></span>
            <button type="button" (click)="openSessions()" [disabled]="!openable().length">{{ openable().length === 1 ? tr('Open 1 Session') : tr('Open {0} Sessions', openable().length) }}</button>
            @if (emu()) {
              <button type="button" (click)="openConsoles()" [disabled]="!picked().length">{{ tr('Console') }}</button>
              <button type="button" (click)="lab.restart(pickedIds())" [disabled]="!picked().length || lab.busy()" [title]="tr('Stop and start: cable changes take effect')">{{ tr('Restart') }}</button>
              <button type="button" (click)="resetSel()" [disabled]="!picked().length || lab.busy()" [title]="tr('Throw the disk away and boot as new; the day-0 commands are typed again')">{{ tr('Back to Day 0') }}</button>
            }
            <button type="button" (click)="serialsOpen.set(!serialsOpen())" [class.on]="serialsOpen()" [title]="tr('Give the switches serial numbers from a list')">{{ tr('Serial Numbers…') }}</button>
            <button type="button" class="danger-ghost" (click)="removeSel()" [disabled]="!sel().size">{{ tr('Remove') }}</button>
          </div>
          }
          @if (serialsOpen()) {
            <div class="serbar">
              <div class="sercol"><textarea [(ngModel)]="serialText" name="ser" rows="4" spellcheck="false" [placeholder]="tr('One serial number per line - or only the first: the others count up from it')"></textarea>
                <div class="macrow"><input class="mono" [(ngModel)]="macStart" name="macst" [placeholder]="tr('First MAC, e.g. 00:04:96:a1:00:00')" spellcheck="false" />
                  <button type="button" class="sm" (click)="applyMACs()" [disabled]="!macStart.trim()" [title]="tr('Each switch gets a block of 64 addresses from it')">{{ tr('Set MACs') }}</button></div></div>
              <div><p class="muted small">@if (emu()) { {{ tr('Given in order to {0} - the switches ticked in the list, or all of them. Each VM gets it as its SMBIOS serial number (system, board and chassis) when it starts next - Restart them; whether the operating system shows it depends on the image.', sel().size ? tr('{0} ticked switches', sel().size) : tr('all {0} switches', lab.devices().length)) }} }
                  @else { {{ tr('Given in order to {0} - the switches ticked in the list, or all of them. Each switch shows it in show version / show switch / show sys-info and in the outputs ({serial}).', sel().size ? tr('{0} ticked switches', sel().size) : tr('all {0} switches', lab.devices().length)) }} }</p>
                <button type="button" class="primary sm" (click)="applySerials()" [disabled]="!serialText.trim()">{{ tr('Set Serial Numbers') }}</button>
                <button type="button" class="sm" (click)="newSerials()" [title]="emu() ? tr('In each image’s serial number format (Images)') : tr('In each model’s format from the device pack')">{{ tr('New in the Model’s Format') }}</button>
                <button type="button" class="sm" (click)="serialsOpen.set(false)">{{ tr('Cancel') }}</button></div>
            </div>
          }
          @if (cable(); as c) {
            <div class="cablebar">
              <select [ngModel]="c.a" (ngModelChange)="pickA($event)" name="ca">@for (d of lab.devices(); track d.id) { <option [value]="d.id">{{ d.hostname }}</option> }</select>
              <select [(ngModel)]="c.aPort" name="cap">@for (p of c.aPorts; track p) { <option [value]="p">{{ p }}</option> }</select>
              <span>⟷</span>
              <select [ngModel]="c.b" (ngModelChange)="pickB($event)" name="cb">@for (d of lab.devices(); track d.id) { <option [value]="d.id">{{ d.hostname }}</option> }</select>
              <select [(ngModel)]="c.bPort" name="cbp">@for (p of c.bPorts; track p) { <option [value]="p">{{ p }}</option> }</select>
              <button type="button" class="primary" (click)="addCable()" [disabled]="!c.a || !c.b || c.a === c.b || !c.aPort || !c.bPort">{{ tr('Add Cable') }}</button>
              <button type="button" (click)="cable.set(null); cableMode.set(false)">{{ tr('Cancel') }}</button>
            </div>
          }
          @switch (tab()) {
            @case ('list') {
              @if (emu()) {
              <div class="tbl"><table>
                <thead><tr><th class="c"><input type="checkbox" [checked]="allSel()" (change)="selAll($any($event.target).checked)" [attr.aria-label]="tr('Select all')" /></th>
                  <th>{{ tr('Switch') }}</th><th>{{ tr('Image') }}</th><th>{{ tr('Model') }}</th><th>{{ tr('Serial') }}</th><th>vCPU · RAM · {{ tr('Disk') }}</th><th>{{ tr('SSH (management)') }}</th><th>{{ tr('Console') }}</th><th>{{ tr('State') }}</th><th>CPU</th><th></th></tr></thead>
                <tbody>
                @for (d of lab.devices(); track d.id) {
                  <tr [class.sel]="sel().has(d.id)" [class.foc]="focus() === d.id" (click)="focusOn(d)">
                    <td class="c"><input type="checkbox" [checked]="sel().has(d.id)" (change)="toggleSel(d.id)" (click)="$event.stopPropagation()" /></td>
                    <td><b>{{ d.hostname }}</b></td><td><span class="os">{{ lab.osShort(d.os) }}</span> {{ d.model }}</td><td>{{ d.persona?.slots?.[0]?.model ?? '-' }}@if ((d.persona?.slots?.length ?? 0) > 1) { <span class="pill b">{{ tr('stack of {0}', d.persona!.slots.length) }}</span> }@if (d.persona && !d.personaOk) { <span class="pendm" [title]="tr('Not written into the switch yet: Apply')"> ●</span> }</td>
                    <td class="mono">{{ d.serial || '-' }}</td><td>{{ d.cpus }} · {{ gb(d.ram) }}@if (d.diskGb) { · {{ d.diskGb }} GB }</td>
                    <td class="mono">{{ d.ssh }}</td><td class="mono">telnet :{{ d.console }}</td>
                    <td><span class="pill" [class]="'pill ' + stCls(d)" [title]="d.error || ''">@if (d.state === 'running') { <span class="dot"></span> } @else if (d.state === 'stopped') { <span class="dot off"></span> } @else if (d.state !== 'failed') { <span class="dot y"></span> }{{ tr(d.stateText || d.state || '') }}</span></td>
                    <td [title]="tr('{0} host cores now (QEMU’s own threads included)', (d.cores ?? 0).toFixed(2))">{{ d.running ? cpuPct(d) : '-' }}</td>
                    <td class="acts"><button type="button" class="sm" (click)="console(d); $event.stopPropagation()" [disabled]="!d.running">{{ tr('Console') }}</button>
                      <button type="button" class="sm" (click)="connect(d); $event.stopPropagation()" [disabled]="d.state !== 'running'">SSH</button>
                      <button type="button" class="sm" (click)="d.running || d.state === 'booting' ? lab.stop([d.id]) : lab.start([d.id]); $event.stopPropagation()">{{ d.running || d.state === 'booting' ? tr('Stop') : tr('Start') }}</button></td>
                  </tr>
                } @empty { <tr><td colspan="11" class="muted">{{ tr('No switches yet: New Lab… makes them from your templates.') }}</td></tr> }
                </tbody></table></div>
              @if (focused(); as d) { @if (d.error) { <p class="err small">{{ d.error }}</p> }
                <div class="devpanel">
                  <div class="dcol">
                    <h4>{{ d.hostname }} · {{ tr('Resources') }}</h4>
                    <div class="row2"><label>vCPU <input type="number" min="1" [(ngModel)]="resCpus" name="rc" class="n" /></label>
                      <label>RAM <input type="number" min="256" step="256" [(ngModel)]="resRam" name="rr" class="n2" /> MB</label>
                      <label>{{ tr('Disk') }} <input type="number" min="0" [(ngModel)]="resDisk" name="rd" class="n" /> GB</label>
                      <button type="button" class="sm" (click)="saveResources(d)" [disabled]="lab.busy()">{{ tr('Save') }}</button></div>
                    <p class="muted small">{{ tr('The switch gets them when it starts next (Restart). A disk only grows; 0 keeps the image’s size.') }}</p>
                    @if (personaDef(d); as pd) {
                      <h4>{{ pd.name || tr('Persona') }} @if (d.persona) { <span [class]="d.personaOk ? 'ok' : 'warn'">{{ d.personaOk ? '✓ ' + tr('in the switch') : '● ' + tr('not written yet') }}</span> }</h4>
                      <table class="slots"><thead><tr><th>#</th><th>{{ tr('Model') }}</th><th>{{ tr('Serial') }}</th><th>{{ pd.modules || tr('Module') }}</th>@if (pd.stack) { <th>{{ tr('Primary') }}</th><th>{{ tr('Ports') }}</th><th></th> }</tr></thead>
                        <tbody>@for (sl of slots(); track $index) {
                          <tr><td>{{ $index + 1 }}</td>
                            <td><select [(ngModel)]="sl.model" [name]="'sm' + $index">@for (m of pd.models ?? []; track m.name) { <option [value]="m.name">{{ m.name }}</option> }</select></td>
                            <td><input class="mono" [(ngModel)]="sl.serial" [name]="'ss' + $index" /></td>
                            <td><select [(ngModel)]="sl.module" [name]="'sv' + $index"><option value="">-</option>@for (x of modulesOf(pd, sl.model); track x) { <option [value]="x">{{ x }}</option> }</select></td>
                            @if (pd.stack) { <td><input type="radio" name="master" [checked]="sl.master" (change)="setMaster($index)" /></td>
                              <td><input class="mono ports" [(ngModel)]="slotPorts[$index]" [name]="'sp' + $index" placeholder="10:1, 3:3" [title]="tr('Front-panel port : VM port, for the stack links')" /></td>
                              <td><button type="button" class="sm" (click)="dropSlot($index)" [disabled]="slots().length === 1">✕</button></td> }</tr>
                        }</tbody></table>
                      @for (v of pd.values ?? []; track v.key) { <div class="row2"><span class="muted small">{{ v.label || v.key }}</span><input class="mono grow" [(ngModel)]="pvals[v.key]" [name]="'pv' + v.key" [placeholder]="v.default" /></div> }
                      <div class="btns">@if (pd.stack) { <button type="button" class="sm" (click)="addSlot(pd)">+ {{ tr('Stack Member') }}</button> }
                        <button type="button" class="sm" (click)="savePersona(d, false)" [disabled]="lab.busy()">{{ tr('Save') }}</button>
                        <button type="button" class="primary sm" (click)="savePersona(d, true)" [disabled]="lab.busy()">{{ tr('Save and Apply') }}</button>
                        @if (d.persona) { <button type="button" class="sm" (click)="clearPersona(d)">{{ tr('No Persona') }}</button> }</div>
                      <p class="muted small">{{ tr('Apply writes {0} into the switch’s disk while it is off and boots it again (a running switch is powered off for it). The first slot’s serial number is the switch’s.', pd.file) }}</p>
                    }
                  </div>
                  <div class="tail"><div class="th">{{ tr('{0} · console (last lines)', d.hostname) }}<span class="spacer"></span><a (click)="loadTail()">{{ tr('Refresh') }}</a></div><pre>{{ tail() || tr('(nothing yet)') }}</pre></div>
                </div> }
              @else { <p class="muted small note">{{ tr('The real operating system runs in each VM on the KVM host. Termwright boots it, waits for the console to be ready and types the image’s day-0 commands once. Click a switch for its console.') }}</p> }
              @if (!lab.devices().length) { <div class="btns" style="margin-top:10px"><button type="button" class="primary" (click)="wizard(false)" [disabled]="!images().length">{{ tr('New Lab…') }}</button></div> }
              } @else {
              <div class="tbl"><table>
                <thead><tr><th class="c"><input type="checkbox" [checked]="allSel()" (change)="selAll($any($event.target).checked)" [attr.aria-label]="tr('Select all')" /></th>
                  <th>{{ tr('Switch') }}</th><th>OS</th><th>{{ tr('Model') }}</th><th>{{ tr('Serial') }}</th><th>{{ tr('Management') }}</th><th>SSH</th><th>{{ tr('State') }}</th><th>{{ tr('Logins') }}</th><th></th></tr></thead>
                <tbody>
                @for (d of lab.devices(); track d.id) {
                  <tr [class.sel]="sel().has(d.id)" (click)="focus.set(d.id)">
                    <td class="c"><input type="checkbox" [checked]="sel().has(d.id)" (change)="toggleSel(d.id)" (click)="$event.stopPropagation()" /></td>
                    <td><b>{{ d.hostname }}</b>@if (d.unsaved) { <span class="muted" [title]="tr('Changes not saved on the switch')"> *</span> }</td>
                    <td><span class="os" [class]="'os ' + d.os">{{ lab.osShort(d.os) }}</span></td>
                    <td>{{ d.model }}</td><td class="mono">{{ d.serial }}</td><td class="mono">{{ d.mgmtIp }}</td><td class="mono">127.0.0.1:{{ d.port }}</td>
                    <td><span class="pill" [class.g]="d.running"><span class="dot" [class.off]="!d.running"></span>{{ d.running ? tr('running') : tr('stopped') }}</span></td>
                    <td>@if (d.logins) { <span class="pill b">● {{ d.logins }}</span> } @else { <span class="muted">-</span> }</td>
                    <td class="acts"><button type="button" class="sm" (click)="connect(d); $event.stopPropagation()" [disabled]="!d.running">{{ tr('Connect') }}</button>
                      <button type="button" class="sm" (click)="d.running ? lab.stop([d.id]) : lab.start([d.id]); $event.stopPropagation()">{{ d.running ? tr('Stop') : tr('Start') }}</button></td>
                  </tr>
                }
                </tbody></table></div>
              <p class="muted small note">{{ tr('Simulated on this computer: the switches’ command line, VLANs, ports and IP interfaces, ping and LLDP over the cables. Not the real operating system - no protocols are run. A switch forgets what was not saved (save configuration / save config) when it stops.') }}</p>
              }
            }
            @case ('topo') {
              <div class="split">
                <div class="canvas">
                  <svg #svg class="topo" [attr.viewBox]="'0 0 ' + box().w + ' ' + box().h" (pointermove)="drag($event, $any(svg))" (pointerup)="drop()" (pointerleave)="drop()">
                    @for (k of lab.links(); track $index) {
                      @if (line(k); as L) {
                        <line [attr.x1]="L.x1" [attr.y1]="L.y1" [attr.x2]="L.x2" [attr.y2]="L.y2" [class]="'c' + (k.up ? '' : ' d')" />
                        <text class="pl" [attr.x]="L.x1 + (L.x2 - L.x1) * .16" [attr.y]="L.y1 + (L.y2 - L.y1) * .16 - 3">{{ k.aPort }}</text>
                        <text class="pl" [attr.x]="L.x1 + (L.x2 - L.x1) * .84" [attr.y]="L.y1 + (L.y2 - L.y1) * .84 - 3">{{ k.bPort }}</text>
                      }
                    }
                    @for (d of lab.devices(); track d.id) {
                      <g class="n" [class.sel]="focus() === d.id" [class.pick]="cable()?.a === d.id" (pointerdown)="grab($event, d)" (click)="clickNode(d)" (dblclick)="emu() ? console(d) : connect(d)">
                        <rect [attr.x]="pos(d).x" [attr.y]="pos(d).y" [attr.width]="NW" [attr.height]="NH" rx="9" />
                        <circle [attr.cx]="pos(d).x + 12" [attr.cy]="pos(d).y + 15" r="4" [class]="nodeCls(d)" />
                        <text [attr.x]="pos(d).x + 22" [attr.y]="pos(d).y + 19">{{ d.hostname }}</text>
                        <text class="s" [attr.x]="pos(d).x + 12" [attr.y]="pos(d).y + 36">{{ lab.osShort(d.os) }} · {{ emu() ? (d.state === 'running' ? ':' + d.console : tr(d.stateText || '')) : d.mgmtIp.split('/')[0] }}</text>
                      </g>
                    }
                  </svg>
                  <div class="hint muted small">{{ cableMode() ? tr('Cable: click the first switch, then the second.') : emu() ? tr('Drag to arrange · click for details · double-click for the console') : tr('Drag to arrange · click for details · double-click to connect') }}</div>
                </div>
                <div class="insp">
                  @if (emu() && focused(); as d) {
                    <h4>{{ d.hostname }} <span class="os">{{ lab.osShort(d.os) }}</span></h4>
                    <div class="kv"><span>{{ tr('State') }}</span><span [class.ok]="d.state === 'running'" [class.bad]="d.state === 'failed'">{{ tr(d.stateText || '') }}</span>
                      <span>{{ tr('Image') }}</span><span>{{ d.model }}</span><span>{{ tr('Host') }}</span><span>{{ hostName() }}</span>
                      <span>vCPU · RAM</span><span>{{ d.cpus }} · {{ gb(d.ram) }}@if (d.running) { · CPU {{ cpuPct(d) }} }</span>
                      <span>{{ tr('Serial') }}</span><span class="mono">@if (idEdit()) { <input class="mono idin" [(ngModel)]="idSerial" name="ids" [placeholder]="tr('the VM’s own')" /> } @else { {{ d.serial || '-' }} }</span>
                      <span>MAC</span><span class="mono">@if (idEdit()) { <input class="mono idin" [(ngModel)]="idMac" name="idm" /> } @else { {{ d.mac }} <a class="x" (click)="editId(d)" [title]="tr('Change the serial number and MAC address')">✎</a> }</span>
                      <span>SSH</span><span class="mono">{{ d.ssh }}</span><span>{{ tr('Console') }}</span><span class="mono">telnet :{{ d.console }}</span>
                      <span>{{ tr('Management') }}</span><span class="mono">{{ d.mgmtIp }}</span></div>
                    @if (idEdit()) { <div class="btns" style="margin:-6px 0 6px"><button type="button" class="primary sm" (click)="saveId(d)">{{ tr('Save') }}</button><button type="button" class="sm" (click)="idEdit.set(false)">{{ tr('Cancel') }}</button></div>
                      <p class="muted small">{{ tr('The VM gets them when it starts next (Restart). The serial number goes to its SMBIOS (system, board, chassis); the MAC is its first port’s, the others follow.') }}</p> }
                    @if (d.error) { <p class="err small">{{ d.error }}</p> }
                    <h4>{{ tr('Cables') }}</h4>
                    <div class="kv">
                      @for (c of cablesOf(d); track c.port) { <span class="mono">{{ c.port }}</span><span [class.bad]="!c.up">{{ c.peer }} {{ c.peerPort }}@if (c.down) { · {{ tr('pulled') }} } @else if (!c.up) { · {{ tr('down') }} }
                        <a class="x" (click)="lab.setLink(d.id, c.port, !!c.down)" [title]="c.down ? tr('Plug the cable in again') : tr('Pull the cable (the link goes down on both switches)')">{{ c.down ? '⏚' : '⏻' }}</a>
                        <a class="x" (click)="lab.disconnect(d.id, c.port)" [title]="tr('Remove the cable (when the switches restart)')">✕</a></span> }
                      @empty { <span class="muted">{{ tr('none') }}</span><span></span> }
                    </div>
                    <p class="muted small">{{ tr('New and removed cables take effect when both switches restart; pulling a cable works at once.') }}</p>
                    <div class="btns"><button type="button" class="primary sm" (click)="console(d)" [disabled]="!d.running">{{ tr('Console') }}</button>
                      <button type="button" class="sm" (click)="connect(d)" [disabled]="d.state !== 'running'">SSH</button>
                      <button type="button" class="sm" (click)="lab.restart([d.id])" [disabled]="lab.busy()">{{ tr('Restart') }}</button>
                      <button type="button" class="sm" (click)="resetOne(d)" [disabled]="lab.busy()">{{ tr('Back to Day 0') }}</button></div>
                  } @else if (focused(); as d) {
                    <h4>{{ d.hostname }} <span class="os" [class]="'os ' + d.os">{{ lab.osShort(d.os) }}</span></h4>
                    <div class="kv"><span>{{ tr('State') }}</span><span [class.ok]="d.running">{{ d.running ? '● ' + tr('running') : tr('stopped') }}@if (d.logins) { · {{ d.logins }} {{ tr('logins') }} }</span>
                      <span>{{ tr('Model') }}</span><span>{{ d.model }}</span><span>{{ tr('Software') }}</span><span>{{ d.version }}</span>
                      <span>{{ tr('Serial') }}</span><span class="mono">@if (idEdit()) { <input class="mono idin" [(ngModel)]="idSerial" name="ids" /> } @else { {{ d.serial }} }</span>
                      <span>MAC</span><span class="mono">@if (idEdit()) { <input class="mono idin" [(ngModel)]="idMac" name="idm" /> } @else { {{ d.mac }} <a class="x" (click)="editId(d)" [title]="tr('Change the serial number and MAC address')">✎</a> }</span>
                      <span>{{ tr('Management') }}</span><span class="mono">{{ d.mgmtIp }}</span><span>SSH</span><span class="mono">127.0.0.1:{{ d.port }}</span>
                      <span>{{ tr('Session') }}</span><span>{{ lab.sessionOf(d.id) || '-' }}</span></div>
                    @if (idEdit()) { <div class="btns" style="margin:-6px 0 12px"><button type="button" class="primary sm" (click)="saveId(d)">{{ tr('Save') }}</button><button type="button" class="sm" (click)="idEdit.set(false)">{{ tr('Cancel') }}</button></div> }
                    <h4>{{ tr('Cables') }}</h4>
                    <div class="kv">
                      @for (c of cablesOf(d); track c.port) { <span class="mono">{{ c.port }}</span><span [class.bad]="!c.up">{{ c.peer }} {{ c.peerPort }}@if (!c.up) { · {{ tr('down') }} } <a class="x" (click)="lab.disconnect(d.id, c.port)" [title]="tr('Remove the cable')">✕</a></span> }
                      @empty { <span class="muted">{{ tr('none') }}</span><span></span> }
                    </div>
                    <div class="btns"><button type="button" class="primary sm" (click)="connect(d)" [disabled]="!d.running">{{ tr('Connect') }}</button>
                      <button type="button" class="sm" (click)="d.running ? lab.stop([d.id]) : lab.start([d.id])">{{ d.running ? tr('Stop') : tr('Start') }}</button>
                      <button type="button" class="sm" (click)="showConfig(d)">{{ tr('Show config') }}</button></div>
                    @if (cfg()) { <pre class="cfg">{{ cfg() }}</pre> }
                  } @else { <p class="muted small">{{ tr('Click a switch for its details and cables.') }}</p> }
                </div>
              </div>
            }
            @case ('outputs') {
              <div class="outs">
                <div class="olist">
                  @for (o of outs(); track $index) {
                    <a [class.on]="oSel() === $index" (click)="oSel.set($index)"><span class="os" [class]="'os ' + (o.os || 'both')">{{ o.os ? lab.osShort(o.os) : tr('All') }}</span> <span class="mono">{{ o.command || tr('(new)') }}</span></a>
                  } @empty { <p class="muted small">{{ tr('No outputs yet.') }}</p> }
                  <button type="button" class="sm" (click)="addOut()">+ {{ tr('Add Output') }}</button>
                </div>
                <div class="oedit">
                  @if (outs()[oSel()]; as o) {
                    <div class="bar"><select [(ngModel)]="o.os" name="oos">@for (k of osIds(); track k) { <option [value]="k">{{ lab.osShort(k) }}</option> }<option value="">{{ tr('All') }}</option></select>
                      <input class="mono grow" [(ngModel)]="o.command" name="ocmd" [placeholder]="tr('show …')" spellcheck="false" />
                      <button type="button" class="sm danger-ghost" (click)="dropOut()">{{ tr('Remove') }}</button></div>
                    <textarea class="otext" [(ngModel)]="o.text" name="otext" spellcheck="false" [placeholder]="tr('Paste the output of the command from a real switch')"></textarea>
                    <p class="muted small">{{ tr('Placeholders: {hostname} {serial} {mac} {mgmtAddr} {mgmtIp} {model} {version} {uptime} {time}; from the configuration: {word:snmp-server location|none} (the next word), {line:prefix|default} (the rest of the line), {if:ssh enable|Enabled|Disabled}; the pack’s own values with {word:kv name|default}.') }}</p>
                  } @else { <p class="muted small">{{ tr('The simulator prints these instead of its own output, with each switch’s values filled in - paste the real output of a command so tools that read it see what a real switch prints.') }}</p> }
                </div>
              </div>
              <div class="bar" style="margin-top:10px"><span class="spacer"></span><button type="button" class="primary" (click)="saveOuts()" [disabled]="lab.busy()">{{ tr('Save Outputs') }}</button></div>
            }
            @case ('images') {
              <div class="bar"><button type="button" class="primary" (click)="addImage()" [disabled]="lab.busy()">+ {{ tr('Add Image (.qcow2, .img, .vmdk)…') }}</button>
                <button type="button" (click)="importAppliance()" [disabled]="lab.busy()">{{ tr('Import GNS3 Appliance (.gns3a)…') }}</button>
                <button type="button" (click)="importProfiles()" [disabled]="lab.busy()" [title]="tr('Image profiles and templates from a file (for example from a private pack)')">{{ tr('Import Profiles (.json)…') }}</button><span class="spacer"></span>
                <button type="button" (click)="imEdit() && lab.upload(imEdit()!.id)" [disabled]="!imEdit() || !lab.state()?.host || imEdit()!.missing">{{ tr('Copy to Host') }}</button>
                <button type="button" class="danger-ghost" (click)="removeImage()" [disabled]="!imEdit()">{{ tr('Remove') }}</button></div>
              <div class="split ims">
                <div class="canvas plain"><table>
                  <thead><tr><th>{{ tr('Image') }}</th><th>{{ tr('Version') }}</th><th>{{ tr('File') }}</th><th>{{ tr('Size') }}</th><th>vCPU · RAM</th><th>{{ tr('Ports') }}</th><th>{{ tr('Ready at') }}</th><th>{{ hostName() || tr('Host') }}</th></tr></thead>
                  <tbody>@for (im of images(); track im.id) {
                    <tr [class.sel]="imEdit()?.id === im.id" (click)="pickImage(im.id)"><td><b>{{ im.name }}</b>@if (im.persona) { <span class="pill b" [title]="tr('Can act as other models: {0}', personaModels(im))">{{ im.persona.name || tr('Persona') }}</span> }</td><td>{{ im.version || '-' }}</td><td class="mono">{{ im.fileName || '-' }}@if (im.missing) { <span class="bad"> · {{ tr('file missing') }}</span> }</td>
                      <td>{{ mb(im.size) }}</td><td>{{ im.cpus }} · {{ gb(im.ram) }}</td><td>{{ im.nics }} × {{ im.nic }}</td><td class="mono">{{ im.ready }}</td>
                      <td>@if (im.upload) { <span class="pill b">{{ tr('copying') }} {{ im.upload }}</span> } @else if (im.onHost) { <span class="pill g">✓ {{ tr('on the host') }}</span> } @else { <span class="pill">{{ tr('copied when needed') }}</span> }</td></tr>
                  } @empty { <tr><td colspan="8" class="muted">{{ tr('No images yet.') }}</td></tr> }</tbody></table>
                  <p class="muted small" style="padding:10px 12px">{{ tr('Images are yours: download them from the vendor (several network operating systems offer free VM images) and add them here. Termwright copies each image to the host once; every switch boots from a small copy-on-write disk on top of it.') }}</p></div>
                <div class="insp wide">
                  @if (imEdit(); as im) {
                    <h4>{{ tr('Image') }}</h4>
                    <div class="kv form">
                      <span>{{ tr('Name') }}</span><input [(ngModel)]="im.name" name="imn" />
                      <span>{{ tr('Version') }}</span><input [(ngModel)]="im.version" name="imv" placeholder="33.4.1.15" />
                      <span>{{ tr('File') }}</span><span><span class="mono small">{{ im.fileName || '-' }}</span> <a (click)="chooseImageFile(im.id)">{{ tr('Choose…') }}</a></span>
                      <span>{{ tr('Device type') }}</span><input [(ngModel)]="im.deviceType" name="imd" [placeholder]="'Linux, Cisco IOS…'" />
                      <span>vCPU · RAM</span><span class="row2"><input type="number" min="1" [(ngModel)]="im.cpus" name="imc" class="n" /> <input type="number" min="128" step="256" [(ngModel)]="im.ram" name="imr" class="n2" /> MB</span>
                      <span>{{ tr('Ports') }}</span><span class="row2"><input type="number" min="1" max="60" [(ngModel)]="im.nics" name="imp" class="n" />
                        <select [(ngModel)]="im.nic" name="imnic">@for (n of nicModels; track n) { <option [value]="n">{{ n }}</option> }</select></span>
                      <span></span><span class="muted small">{{ tr('The first port is the management port; the others can be cabled.') }}</span>
                      <span>{{ tr('Port names') }}</span><input class="mono" [(ngModel)]="im.portName" name="impn" placeholder="{n}  ·  1/{n}" />
                      <span>{{ tr('Disk') }}</span><select [(ngModel)]="im.disk" name="imdk"><option value="ide">IDE</option><option value="virtio">virtio</option><option value="sata">SATA</option><option value="scsi">SCSI</option></select>
                      <span>{{ tr('Ready when') }}</span><input class="mono" [(ngModel)]="im.ready" name="imrd" placeholder="login:" />
                      <span>{{ tr('Login') }}</span><span class="row2"><input [(ngModel)]="im.login" name="iml" class="n3" /> <input type="password" [(ngModel)]="imPassword" name="impw" [placeholder]="im.hasPassword ? tr('(saved)') : tr('no password')" autocomplete="new-password" class="n3" /></span>
                      <span>{{ tr('Address command') }}</span><input class="mono" [(ngModel)]="im.addrCmd" name="imac" [placeholder]="'show vlan Mgmt'" spellcheck="false" />
                      <span></span><span class="muted small">{{ tr('On the host’s network the switch gets its address by DHCP: Termwright types this on the console after each boot and takes the first address it shows.') }}</span>
                      <span>{{ tr('Serial format') }}</span><input class="mono" [(ngModel)]="im.serial" name="imsn" [placeholder]="'AB{YY}{WW}-{N:5}'" spellcheck="false" />
                      <span></span><span class="muted small">{{ tr('New switches get a serial number like this: {N:5} digits, {A:2} letters, {H:4} hex, {YY} year, {WW} week, {R:1-9} a number between, {L:abc} one of the letters. Empty: the VM’s own.') }}</span>
                      <span>{{ tr('Prompt') }}</span><input class="mono" [(ngModel)]="im.prompt" name="impr" placeholder="[>#$]\\s*$" />
                    </div>
                    <h4>{{ tr('First boot (day-0) commands') }}</h4>
                    <textarea class="mono day0" [(ngModel)]="day0Text" name="imday0" spellcheck="false" rows="6"></textarea>
                    <p class="muted small">{{ tr('Typed on the console after the first boot. {hostname} {mgmtIp} {mgmtAddr} {mgmtMask} {gateway} {n} are filled in per switch.') }}</p>
                    <h4>{{ tr('Answers on the console') }}</h4>
                    <textarea class="mono day0" [(ngModel)]="answersText" name="imans" spellcheck="false" rows="3" [placeholder]="'[y/N] => n'"></textarea>
                    <p class="muted small">{{ tr('One per line: text the console shows => what to type (nothing after => is Enter).') }}</p>
                    <span class="muted small">{{ tr('More QEMU options') }}</span><input class="mono wide" [(ngModel)]="im.extra" name="imx" spellcheck="false" />
                    @if (im.source) { <p class="muted small">{{ tr('From') }}: {{ im.source }}</p> }
                    <div class="btns" style="margin-top:10px"><button type="button" class="primary sm" (click)="saveImage()" [disabled]="lab.busy()">{{ tr('Save') }}</button></div>
                  } @else { <p class="muted small">{{ tr('Click an image for how it boots.') }}</p> }
                </div>
              </div>
            }
            @case ('templates') {
              <div class="bar"><button type="button" class="primary" (click)="newTemplate()" [disabled]="!images().length">+ {{ tr('New Template') }}</button><span class="spacer"></span>
                <button type="button" (click)="tpEdit() && addFromTemplate(tpEdit()!.id)" [disabled]="!tpEdit()?.id || !lab.state()?.host && !hosts().length">{{ tr('Add Switches from It…') }}</button>
                <button type="button" class="danger-ghost" (click)="removeTemplate()" [disabled]="!tpEdit()?.id">{{ tr('Remove') }}</button></div>
              <div class="split ims">
                <div class="canvas plain"><table>
                  <thead><tr><th>{{ tr('Template') }}</th><th>{{ tr('Image') }}</th><th>{{ tr('Model') }}</th><th>vCPU · RAM · {{ tr('Disk') }}</th><th>{{ tr('Serial from') }}</th><th>{{ tr('MAC from') }}</th><th>{{ tr('Switches') }}</th></tr></thead>
                  <tbody>@for (t of templates(); track t.id) {
                    <tr [class.sel]="tpEdit()?.id === t.id" (click)="pickTemplate(t.id)"><td><b>{{ t.name }}</b></td><td>{{ imageName(t.image) }}</td><td>{{ t.model || '-' }}@if (t.module) { + {{ t.module }} }</td>
                      <td>{{ t.cpus }} · {{ gb(t.ram) }}@if (t.diskGb) { · {{ t.diskGb }} GB }</td><td class="mono">{{ t.serialStart || '-' }}</td><td class="mono">{{ t.macStart || '-' }}</td><td>{{ usedBy(t.id) }}</td></tr>
                  } @empty { <tr><td colspan="7" class="muted">{{ tr('No templates yet: a template is an image with its resources, the model it acts as, and where serial numbers and MAC addresses start.') }}</td></tr> }</tbody></table></div>
                <div class="insp wide">
                  @if (tpEdit(); as t) {
                    <h4>{{ t.id ? tr('Template') : tr('New Template') }}</h4>
                    <div class="kv form">
                      <span>{{ tr('Name') }}</span><input [(ngModel)]="t.name" name="tpn" placeholder="M-48" />
                      <span>{{ tr('Image') }}</span><select [(ngModel)]="t.image" name="tpi" (ngModelChange)="tpImage($event)">@for (im of images(); track im.id) { <option [value]="im.id">{{ im.name }}{{ im.version ? ' · ' + im.version : '' }}</option> }</select>
                      @if (personaDefOf(t.image); as pd) {
                        <span>{{ tr('Model') }}</span><select [(ngModel)]="t.model" name="tpm"><option value="">{{ tr('(the image’s own)') }}</option>@for (m of pd.models ?? []; track m.name) { <option [value]="m.name">{{ m.name }}</option> }</select>
                        <span>{{ pd.modules || tr('Module') }}</span><select [(ngModel)]="t.module" name="tpx"><option value="">-</option>@for (x of modulesOf(pd, t.model); track x) { <option [value]="x">{{ x }}</option> }</select>
                        @for (v of pd.values ?? []; track v.key) { <span>{{ v.label || v.key }}</span><input class="mono" [(ngModel)]="tpVals[v.key]" [name]="'tv' + v.key" [placeholder]="v.default" /> }
                      }
                      <span>vCPU · RAM</span><span class="row2"><input type="number" min="1" [(ngModel)]="t.cpus" name="tpc" class="n" /> <input type="number" min="256" step="256" [(ngModel)]="t.ram" name="tpr" class="n2" /> MB</span>
                      <span>{{ tr('Disk') }}</span><span class="row2"><input type="number" min="0" [(ngModel)]="t.diskGb" name="tpd" class="n" /> GB <span class="muted small">{{ tr('0 = the image’s size') }}</span></span>
                      <span>{{ tr('Serial from') }}</span><input class="mono" [(ngModel)]="t.serialStart" name="tps" placeholder="AR022205Q-40036" spellcheck="false" />
                      <span>{{ tr('MAC from') }}</span><input class="mono" [(ngModel)]="t.macStart" name="tpmac" placeholder="00:04:96:a1:00:00" spellcheck="false" />
                    </div>
                    <p class="muted small">{{ tr('Each new switch from this template takes the next serial number (the last number counts up: …-40036, …-40037) and the next block of 64 MAC addresses.') }}</p>
                    <div class="btns"><button type="button" class="primary sm" (click)="saveTemplate()" [disabled]="lab.busy() || !t.name || !t.image">{{ tr('Save') }}</button></div>
                  } @else { <p class="muted small">{{ tr('Click a template, or make a new one from an image.') }}</p> }
                </div>
              </div>
            }
            @case ('hosts') {
              <div class="bar"><span class="muted">{{ tr('SSH session of a Linux host') }}</span>
                <select [ngModel]="hostPick || sshSessions()[0]" (ngModelChange)="hostPick = $event" name="hpick" class="hpick">@for (s of sshSessions(); track s) { <option [value]="s">{{ s }}</option> }</select>
                <button type="button" class="primary" (click)="checkHost(hostPick || sshSessions()[0])" [disabled]="!(hostPick || sshSessions()[0]) || lab.busy()">{{ tr('Check and Add') }}</button></div>
              @if (setupHost(); as h) {
                <div class="setup">
                  <div class="big">{{ tr('Set up {0} for the emulated lab', h.name) }}</div>
                  @if (!running()) {
                    <p class="muted small">{{ tr('Termwright logs in to the host and runs, as root (sudo):') }}</p>
                    <ul>@for (p of h.info.plan ?? []; track $index) { <li class="mono small">{{ p }}</li> }</ul>
                    <p class="muted small">{{ tr('Packages come from the distribution’s own repositories. Nothing else is changed; the switches run as {0}, not as root.', h.info.user) }}</p>
                    <div class="bar">@if (!h.info.sudo) { <span class="muted">{{ tr('sudo password of {0}', h.info.user) }}</span><input type="password" [(ngModel)]="sudoPw" name="sudopw" autocomplete="off" class="pw" /> }
                      <button type="button" class="primary" (click)="runSetup(h.session)" [disabled]="!h.info.sudo && !sudoPw">{{ tr('Set Up') }}</button><button type="button" (click)="closeSetup()">{{ tr('Cancel') }}</button>
                      <span class="muted small">{{ h.info.sudo ? tr('sudo works without a password here.') : tr('The password goes to sudo on the host only; it is not saved.') }}</span></div>
                  }
                  @if (lab.setup(); as s) { @if (s.host === h.session) {
                    <div class="bar"><b>{{ s.done ? (s.error ? tr('Set-up failed') : tr('Set up') + ' ✓') : tr('Setting up…') + ' ' + s.step }}</b><span class="spacer"></span>@if (s.done) { <button type="button" class="sm" (click)="closeSetup()">{{ tr('Close') }}</button> }</div>
                    @if (s.error) { <p class="err small">{{ s.error }}</p> }
                    <pre class="setuplog">{{ s.lines.join('\n') || '…' }}</pre>
                  } }
                </div>
              }
              <div class="tbl"><table>
                <thead><tr><th>{{ tr('Host') }}</th><th>{{ tr('System') }}</th><th>KVM</th><th>QEMU</th><th>vCPU</th><th>{{ tr('Network') }}</th><th>RAM {{ tr('free') }}</th><th>{{ tr('Disk free') }}</th><th>sudo</th><th></th></tr></thead>
                <tbody>@for (h of hosts(); track h.session) {
                  <tr><td><b>{{ h.name }}</b>@if (h.session === lab.state()?.host) { <span class="pill b">{{ tr('the lab') }}</span> }<div class="muted small">{{ h.info.user }}</div></td><td class="small">{{ h.info.os }}</td>
                    <td><span [class.ok]="h.info.kvm === 'yes'" [class.bad]="h.info.kvm !== 'yes'">{{ h.info.kvm === 'yes' ? '✓' : h.info.kvm }}</span></td><td class="small">{{ h.info.qemu || '-' }}</td><td>{{ h.info.cpus }}</td><td class="small">{{ h.info.lanKind ? h.info.lanIf + ' (' + (h.info.lanKind === 'bridge' ? tr('bridge') : 'macvtap') + ')' : (h.info.uplink || '-') }}</td>
                    <td>{{ gb(h.info.memFree) }} / {{ gb(h.info.memTotal) }}</td><td>{{ gb(h.info.diskFree) }}</td><td>{{ h.info.sudo ? '✓' : '-' }}</td>
                    <td class="acts">@if (h.info.canSetup) { <button type="button" class="sm primary" (click)="openSetup(h.session)">{{ tr('Set Up QEMU/KVM…') }}</button> }
                      <button type="button" class="sm" (click)="checkHost(h.session)" [disabled]="lab.busy()">{{ tr('Check Again') }}</button> <button type="button" class="sm" (click)="lab.removeHost(h.session)">{{ tr('Remove') }}</button></td></tr>
                  @for (p of h.info.problems ?? []; track $index) { <tr><td></td><td colspan="9" class="bad small">✕ {{ p }}</td></tr> }
                  @for (p of h.info.notes ?? []; track $index) { <tr><td></td><td colspan="9" class="warn small">! {{ p }}</td></tr> }
                } @empty { <tr><td colspan="10" class="muted">{{ tr('No KVM hosts yet: pick the SSH session of a Linux server with KVM and QEMU.') }}</td></tr> }</tbody></table></div>
              <p class="muted small note">{{ tr('Termwright runs qemu on the host over SSH, as the session’s user, in the folder ~/termwright-lab; Set Up installs QEMU from the distribution’s packages when it is missing. The switches’ sessions go through the host session as jump host.') }}</p>
            }
            @case ('cables') {
              <div class="tbl"><table>
                <thead><tr><th>{{ tr('Switch') }}</th><th>{{ tr('Port') }}</th><th></th><th>{{ tr('Switch') }}</th><th>{{ tr('Port') }}</th><th>{{ tr('State') }}</th><th></th></tr></thead>
                <tbody>
                @for (k of lab.links(); track $index) {
                  <tr><td>{{ nameOf(k.a) }}</td><td class="mono">{{ k.aPort }}</td><td>⟷</td><td>{{ nameOf(k.b) }}</td><td class="mono">{{ k.bPort }}</td>
                    <td><span class="pill" [class.g]="k.up" [class.r]="!k.up">{{ k.up ? tr('up') : k.down ? tr('pulled') : tr('down') }}</span></td>
                    <td class="acts">@if (emu()) { <button type="button" class="sm" (click)="lab.setLink(k.a, k.aPort, !!k.down)">{{ k.down ? tr('Plug In') : tr('Pull') }}</button> } <button type="button" class="sm" (click)="lab.disconnect(k.a, k.aPort)">{{ tr('Remove') }}</button></td></tr>
                } @empty { <tr><td colspan="7" class="muted">{{ tr('No cables. Use Cable to connect two switches.') }}</td></tr> }
                </tbody></table></div>
            }
          }
        </div>
        <div class="modal-foot"><span class="muted small">{{ tr('Sessions are in the Session Manager under {0}', lab.state()?.folder || '') }}</span><span class="spacer"></span>
          <button type="button" class="primary" (click)="ui.close()">{{ tr('Close') }}</button></div>
      }
    </div>
  </div>`,
  styles: [`
    .labw { width: min(1180px, 96vw); }
    .modal-head .spacer { flex: 1; }
    .pendm { color: var(--warn); font-size: 10px; }
    .stepn { display: inline-flex; width: 17px; height: 17px; border-radius: 50%; background: var(--accent-soft); color: var(--accent); font-size: 10.5px; font-weight: 700; align-items: center; justify-content: center; margin-right: 6px; }
    .hoststrip { display: flex; gap: 16px; align-items: center; flex-wrap: wrap; padding: 8px 18px; border-bottom: 1px solid var(--line); background: var(--surface-2); font-size: 12.5px;
      .m { display: inline-flex; align-items: center; gap: 6px; } .meter { display: inline-block; width: 70px; height: 6px; border-radius: 4px; background: var(--surface); overflow: hidden; i { display: block; height: 100%; background: var(--accent); } i.over { background: var(--danger); } } }
    .sercol { display: flex; flex-direction: column; gap: 6px; } .macrow { display: flex; gap: 6px; input { flex: 1; height: 26px; font-size: 12px; } }
    .devpanel { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr); gap: 12px; margin-top: 10px; .tail { margin-top: 0; } h4 { margin: 0 0 6px; font-size: 13px; }
      .row2 { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-bottom: 6px; input { height: 26px; } .n { width: 56px; } .n2 { width: 76px; } .grow { flex: 1; } }
      .dcol { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; overflow: auto; max-height: 300px; }
      table.slots { width: 100%; margin-bottom: 8px; th, td { padding: 3px 5px; font-size: 12px; } select, input { height: 24px; font-size: 12px; width: 100%; } input[type=radio] { width: auto; } .ports { width: 90px; } } } .kindsel { height: 28px; font-size: 12.5px; margin-left: 12px; }
    .btns.center { justify-content: center; } .where { font-weight: 500; } .warn { color: var(--warn); }
    .pill.y { color: var(--warn); border-color: color-mix(in srgb, var(--warn) 45%, transparent); background: color-mix(in srgb, var(--warn) 10%, transparent); }
    .dot.y { background: var(--warn); } tr.foc td { box-shadow: inset 0 1px 0 var(--accent), inset 0 -1px 0 var(--accent); }
    .tail { margin-top: 10px; border: 1px solid var(--line); border-radius: 10px; overflow: hidden; .th { display: flex; padding: 6px 12px; font-size: 12px; font-weight: 600; background: var(--surface-2); a { cursor: pointer; color: var(--accent); font-weight: 400; } }
      pre { margin: 0; padding: 8px 12px; font: 11.5px/1.5 var(--mono); max-height: 190px; overflow: auto; white-space: pre-wrap; } }
    .ims { height: min(560px, 60vh); } .ims td, .ims th { white-space: nowrap; } .canvas.plain { background: var(--surface); overflow: auto; } .insp.wide { width: 410px; }
    .kv.form { grid-template-columns: 110px 1fr; align-items: center; input, select { height: 26px; font-size: 12.5px; min-width: 0; } .row2 { display: flex; gap: 6px; align-items: center; } .n { width: 54px; } .n2 { width: 80px; } .n3 { width: 120px; } a { cursor: pointer; color: var(--accent); } }
    .day0 { width: 100%; font-size: 11.5px; resize: vertical; } input.wide { width: 100%; height: 26px; margin-top: 4px; } .hpick { min-width: 280px; }
    .setup { border: 1px solid var(--accent); border-radius: 10px; padding: 12px 14px; margin-bottom: 12px; background: var(--accent-soft);
      .big { font-weight: 600; margin-bottom: 6px; } ul { margin: 4px 0 8px; padding-left: 20px; } input.pw { width: 160px; } }
    .setuplog { margin: 0; max-height: 200px; overflow: auto; font: 11px/1.45 var(--mono); background: var(--bg); border: 1px solid var(--line); border-radius: 8px; padding: 8px; white-space: pre-wrap; }
    .topo circle.y { fill: var(--warn); } .topo circle.b { fill: var(--accent); } .topo circle.r { fill: var(--danger); }
    .serbar { display: grid; grid-template-columns: 280px 1fr; gap: 12px; padding: 10px; border: 1px solid var(--line); border-radius: 9px; margin-bottom: 10px; background: var(--surface);
      textarea { font: 12px var(--mono); resize: vertical; } p { margin: 0 0 8px; } }
    .idin { width: 100%; height: 24px; }
    .outs { display: grid; grid-template-columns: 260px 1fr; gap: 12px; min-height: 380px; }
    .olist { border: 1px solid var(--line); border-radius: 10px; padding: 6px; display: flex; flex-direction: column; gap: 2px; overflow: auto; max-height: 52vh;
      a { padding: 6px 8px; border-radius: 6px; cursor: pointer; display: flex; gap: 6px; align-items: center; } a.on { background: var(--accent-soft); } button { margin-top: 6px; align-self: flex-start; } }
    .oedit { display: flex; flex-direction: column; min-width: 0; .grow { flex: 1; } }
    .otext { flex: 1; min-height: 260px; font: 12px/1.45 var(--mono); resize: vertical; white-space: pre; }
    .os.both { background: var(--surface-2); color: var(--muted); }
    .empty { text-align: center; padding: 40px 30px; .big { font-size: 17px; font-weight: 600; margin-bottom: 8px; } p { max-width: 560px; margin: 0 auto 16px; } a { cursor: pointer; color: var(--accent); } }
    .consent { padding: 24px 30px; .big { font-size: 17px; font-weight: 600; margin-bottom: 12px; } .kv.wide { grid-template-columns: 110px 1fr; } .terms { color: var(--warn); } }
    .chk { display: inline-flex; gap: 8px; align-items: center; } .err { color: var(--danger); }
    .ltabs { display: flex; gap: 4px; padding: 0 18px; border-bottom: 1px solid var(--line);
      button { border: 0; background: none; border-radius: 0; height: 36px; padding: 0 12px; color: var(--muted); border-bottom: 2px solid transparent; }
      button.on { color: var(--text); border-bottom-color: var(--accent); } }
    .bar, .cablebar { display: flex; gap: 8px; align-items: center; margin-bottom: 10px; flex-wrap: wrap; }
    .cablebar { padding: 8px 10px; border: 1px solid var(--accent); border-radius: 9px; background: var(--accent-soft); }
    .bar button.on { border-color: var(--accent); color: var(--accent); }
    .spacer { flex: 1; }
    .tbl { max-height: 52vh; overflow: auto; border: 1px solid var(--line); border-radius: 10px; }
    table { width: 100%; border-collapse: collapse; }
    th { position: sticky; top: 0; background: var(--surface-2); text-align: left; font-size: 12px; font-weight: 600; padding: 7px 10px; z-index: 1; }
    td { padding: 6px 10px; border-top: 1px solid var(--line); vertical-align: middle; }
    tr.sel td { background: var(--accent-soft); }
    .c { width: 30px; }
    .acts { text-align: right; white-space: nowrap; }
    button.sm { height: 26px; padding: 0 9px; font-size: 12px; }
    .mono { font-family: var(--mono); font-size: 12px; }
    .os { font-size: 10.5px; font-weight: 700; padding: 1px 6px; border-radius: 5px; letter-spacing: .3px; background: color-mix(in srgb, #7c5cff 18%, transparent); color: #8f75ff; }
    
    .pill { display: inline-flex; gap: 5px; align-items: center; padding: 1px 8px; border-radius: 10px; font-size: 11.5px; border: 1px solid var(--line); white-space: nowrap; color: var(--muted); }
    .pill.g { color: var(--ok); border-color: color-mix(in srgb, var(--ok) 45%, transparent); background: color-mix(in srgb, var(--ok) 12%, transparent); }
    .pill.r { color: var(--danger); border-color: color-mix(in srgb, var(--danger) 45%, transparent); }
    .pill.b { color: var(--accent); border-color: color-mix(in srgb, var(--accent) 45%, transparent); background: var(--accent-soft); }
    .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--ok); display: inline-block; } .dot.off { background: var(--faint); }
    .note { margin: 10px 0 0; }
    .split { display: flex; height: min(540px, 58vh); border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
    .canvas { flex: 1; min-width: 0; position: relative; overflow: auto; background: var(--bg); }
    .canvas .hint { position: absolute; left: 10px; bottom: 6px; }
    .topo { display: block; width: 100%; min-width: 600px; height: 100%; user-select: none; }
    .topo .n { cursor: grab; }
    .topo .n rect { fill: var(--surface); stroke: var(--line); stroke-width: 1.2; }
    .topo .n.sel rect { stroke: var(--accent); stroke-width: 2; }
    .topo .n.pick rect { stroke: var(--warn); stroke-width: 2; stroke-dasharray: 4 3; }
    .topo .n text { fill: var(--text); font: 600 12px var(--ui-font); } .topo .n text.s { fill: var(--muted); font: 11px var(--mono); }
    .topo circle.on { fill: var(--ok); } .topo circle.off { fill: var(--faint); }
    .topo line.c { stroke: var(--ok); stroke-width: 2; } .topo line.c.d { stroke: var(--danger); stroke-dasharray: 5 4; }
    .topo text.pl { fill: var(--muted); font: 10px var(--mono); }
    .insp { width: 300px; border-left: 1px solid var(--line); padding: 12px 14px; overflow: auto; background: var(--surface); }
    .insp h4 { margin: 0 0 6px; font-size: 13px; }
    .kv { display: grid; grid-template-columns: 96px 1fr; gap: 5px 8px; font-size: 12.5px; margin-bottom: 12px; }
    .kv > span:nth-child(odd) { color: var(--muted); }
    .kv .x { margin-left: 6px; cursor: pointer; color: var(--muted); }
    .ok { color: var(--ok); } .bad { color: var(--danger); }
    .btns { display: flex; gap: 6px; flex-wrap: wrap; }
    .cfg { margin: 10px 0 0; max-height: 200px; overflow: auto; font: 11px/1.4 var(--mono); background: var(--bg); border: 1px solid var(--line); border-radius: 8px; padding: 8px; white-space: pre; }
  `],
})
export class LabDialog implements OnInit, OnDestroy {
  protected ui = inject(Ui);
  protected lab = inject(LabService);
  private tabs = inject(Tabs);
  private backend = inject(Backend);
  protected tr = tr;
  protected NW = NW; protected NH = NH;
  protected tab = signal<'list' | 'topo' | 'cables' | 'outputs' | 'images' | 'hosts' | 'templates'>('list');
  protected templates = computed(() => this.lab.emu()?.templates ?? []);
  protected usage = computed(() => this.lab.emu()?.usage ?? null);
  protected emu = computed(() => this.lab.kind() === 'emu');
  protected manage = signal(false);
  protected images = computed(() => this.lab.emu()?.images ?? []);
  protected hosts = computed(() => this.lab.emu()?.hosts ?? []);
  protected hostName = computed(() => { const e = this.lab.emu(); return e?.hosts?.find(h => h.session === e.host)?.name ?? e?.host ?? ''; });
  protected nicModels = ['e1000', 'virtio-net-pci', 'vmxnet3', 'rtl8139', 'i82559er', 'e1000e'];
  protected sel = signal(new Set<string>());
  protected focus = signal('');
  protected cfg = signal('');
  protected cableMode = signal(false);
  protected cable = signal<{ a: string; aPort: string; aPorts: string[]; b: string; bPort: string; bPorts: string[] } | null>(null);
  private moved = signal(new Map<string, { x: number; y: number }>());
  private dragging: { id: string; dx: number; dy: number; moved: boolean } | null = null;

  protected subtitle = computed(() => {
    const ds = this.lab.devices(), ls = this.lab.links();
    if (!ds.length) return this.emu() ? tr('Real VM images on a Linux KVM host') : tr('Switches simulated on this computer, from a device pack');
    if (this.emu()) return tr('{0} switches · {1} ready', ds.length, ds.filter(d => d.state === 'running').length) + ' · ' + this.hostName();
    const down = ls.filter(l => !l.up).length;
    return tr('{0} switches · {1} running', ds.length, this.lab.running()) + (down ? ' · ' + tr('{0} cables down', down) : '');
  });
  protected focused = computed(() => this.lab.devices().find(d => d.id === this.focus()));
  protected allSel = computed(() => this.lab.devices().length > 0 && this.lab.devices().every(d => this.sel().has(d.id)));
  protected openable = computed(() => this.lab.devices().filter(d => (d.state ? d.state === 'running' : d.running) && (this.sel().size ? this.sel().has(d.id) : true)));
  protected box = computed(() => {
    let w = 900, h = 420;
    for (const d of this.lab.devices()) { const p = this.pos(d); w = Math.max(w, p.x + NW + 40); h = Math.max(h, p.y + NH + 40); }
    return { w, h };
  });

  private tailTimer: ReturnType<typeof setInterval> | null = null;
  ngOnInit() {
    this.lab.watching.set(true); void this.lab.refresh();
    const sf = this.lab.setupFor();
    if (sf) { this.lab.setupFor.set(''); this.lab.kind.set('emu'); this.manage.set(true); this.tab.set('hosts'); this.openSetup(sf); }
    this.tailTimer = setInterval(() => { if (this.emu() && this.tab() === 'list' && this.focus()) void this.loadTail(); }, 3000);
  }
  ngOnDestroy() { this.lab.watching.set(false); if (this.tailTimer) clearInterval(this.tailTimer); }
  setKind(k: 'sim' | 'emu') { this.lab.kind.set(k); this.tab.set('list'); this.sel.set(new Set()); this.focus.set(''); this.manage.set(false); }
  gb(mb?: number) { return !mb ? '-' : mb >= 1024 ? (mb / 1024).toFixed(mb % 1024 ? 1 : 0) + ' GB' : mb + ' MB'; }
  mb(bytes: number) { return !bytes ? '-' : bytes >= 1 << 30 ? (bytes / (1 << 30)).toFixed(1) + ' GB' : Math.round(bytes / (1 << 20)) + ' MB'; }
  stCls(d: LabDevice) { return d.state === 'running' ? 'g' : d.state === 'failed' ? 'r' : d.state === 'stopped' ? '' : 'y'; }
  nodeCls(d: LabDevice) { return !d.state ? (d.running ? 'on' : 'off') : d.state === 'running' ? 'on' : d.state === 'stopped' ? 'off' : d.state === 'failed' ? 'r' : d.state === 'day0' ? 'b' : 'y'; }
  protected tail = signal('');
  focusOn(d: LabDevice) { this.focus.set(d.id); this.tail.set(''); this.loadDevice(d); void this.loadTail(); }
  async loadTail() { const id = this.focus(); if (id) { const t = await this.lab.consoleTail(id); if (this.focus() === id) this.tail.set(t); } }
  protected picked = computed(() => this.lab.devices().filter(d => this.sel().size ? this.sel().has(d.id) : d.id === this.focus()));
  pickedIds() { return this.picked().map(d => d.id); }
  console(d: LabDevice) { const p = this.lab.consoleOf(d.id); if (p) void this.tabs.openSession(p); }
  openConsoles() { for (const d of this.picked()) this.console(d); }
  async resetSel() { const ds = this.picked(); if (ds.length && await this.ui.confirm(tr('Back to Day 0'), tr('Throw away the disks of {0} switches and boot them as new? Their configuration is lost; the day-0 commands are typed again.', ds.length), tr('Back to Day 0'), true)) await this.lab.reset(ds.map(d => d.id)); }
  async resetOne(d: LabDevice) { if (await this.ui.confirm(tr('Back to Day 0'), tr('Throw away the disk of {0} and boot it as new? Its configuration is lost; the day-0 commands are typed again.', d.hostname), tr('Back to Day 0'), true)) await this.lab.reset([d.id]); }

  // images
  protected imEdit = signal<EmuImage | null>(null);
  protected imPassword = '';
  protected day0Text = '';
  protected answersText = '';
  pct(a: number, b: number) { return b ? Math.min(100, Math.round(a * 100 / b)) : 0; }
  imageName(id: string) { const im = this.images().find(i => i.id === id); return im ? im.name + (im.version ? ' · ' + im.version : '') : '-'; }
  personaDefOf(imageId: string) { return this.images().find(i => i.id === imageId)?.persona ?? null; }
  personaDef(d: LabDevice) { return d.image ? this.personaDefOf(d.image) : null; }
  personaModels(im: EmuImage) { return (im.persona?.models ?? []).map(m => m.name).join(', '); }
  modulesOf(pd: EmuPersonaDef, model: string) { return pd.models?.find(m => m.name === model)?.modules ?? []; }
  usedBy(tid: string) { return this.lab.devices().filter(d => d.template === tid).length; }
  // templates
  protected tpEdit = signal<EmuTemplate | null>(null);
  protected tpVals: Record<string, string> = {};
  pickTemplate(id: string) { const t = this.templates().find(x => x.id === id); if (t) { this.tpEdit.set({ ...t }); this.tpVals = { ...(t.values ?? {}) }; } }
  newTemplate() { const im = this.images()[0]; this.tpEdit.set({ id: '', name: '', image: im?.id ?? '', cpus: im?.cpus || 1, ram: im?.ram || 2048, diskGb: 0, model: '', module: '', values: {}, serialStart: '', macStart: '' }); this.tpVals = {}; }
  tpImage(id: string) { const t = this.tpEdit(); const im = this.images().find(i => i.id === id); if (t && im) this.tpEdit.set({ ...t, image: id, cpus: im.cpus || t.cpus, ram: im.ram || t.ram, model: '', module: '' }); }
  async saveTemplate() {
    const t = this.tpEdit(); if (!t) return;
    const isNew = !t.id;
    if (await this.lab.saveTemplate({ ...t, cpus: +t.cpus || 1, ram: +t.ram || 1024, diskGb: +t.diskGb || 0, values: { ...this.tpVals } })) {
      const list = this.templates(); this.pickTemplate(isNew ? list[list.length - 1]?.id : t.id);
    }
  }
  async removeTemplate() { const t = this.tpEdit(); if (t?.id && await this.lab.removeTemplate(t.id)) this.tpEdit.set(null); }
  addFromTemplate(id: string) { this.lab.wizardTemplate.set(id); this.wizard(!!this.lab.state()?.exists); }
  async importProfiles() {
    const p = await this.backend.chooseOpenFile(tr('Import Image Profiles'), [{ displayName: tr('Image profiles'), pattern: '*.json' }]).catch(() => '');
    if (p) await this.lab.importProfiles(p);
  }
  // resources and persona of the focused switch
  protected resCpus = 1; protected resRam = 1024; protected resDisk = 0;
  protected slots = signal<EmuSlot[]>([]);
  protected slotPorts: string[] = [];
  protected pvals: Record<string, string> = {};
  private loadDevice(d: LabDevice) {
    this.resCpus = d.cpus || 1; this.resRam = d.ram || 1024; this.resDisk = d.diskGb || 0;
    const pd = this.personaDef(d);
    const sl = d.persona?.slots?.length ? d.persona.slots.map((x, i) => ({ ...x, serial: x.serial || (i === 0 ? d.serial || '' : '') })) : [{ num: 1, model: pd?.models?.[0]?.name ?? '', serial: d.serial || '', module: '', master: true, ports: null }];
    this.slots.set(sl); this.slotPorts = sl.map(x => (x.ports ?? []).map(p => p.port + ':' + p.vif).join(', '));
    this.pvals = { ...(d.persona?.values ?? {}) };
  }
  async saveResources(d: LabDevice) { await this.lab.setResources(d.id, +this.resCpus || 1, +this.resRam || 1024, +this.resDisk || 0); }
  addSlot(pd: EmuPersonaDef) {
    const last = this.slots()[this.slots().length - 1];
    this.slots.set([...this.slots(), { num: this.slots().length + 1, model: last?.model ?? pd.models?.[0]?.name ?? '', serial: '', module: '', master: false, ports: null }]); this.slotPorts.push('');
  }
  dropSlot(i: number) { this.slots.set(this.slots().filter((_, j) => j !== i)); this.slotPorts.splice(i, 1); }
  setMaster(i: number) { this.slots.set(this.slots().map((x, j) => ({ ...x, master: j === i }))); }
  async savePersona(d: LabDevice, apply: boolean) {
    const sl: EmuSlot[] = [];
    for (const [i, x] of this.slots().entries()) {
      const ports: { port: number; vif: number }[] = [];
      for (const f of (this.slotPorts[i] ?? '').split(/[,; ]+/).filter(Boolean)) { const [p, v] = f.split(':').map(Number); if (!p || !v) { this.ui.error(tr('{0} is not port:vm-port', f)); return; } ports.push({ port: p, vif: v }); }
      sl.push({ ...x, num: i + 1, ports });
    }
    if (await this.lab.setPersona(d.id, { slots: sl, values: { ...this.pvals } }) && apply) await this.lab.applyPersona([d.id]);
  }
  async clearPersona(d: LabDevice) { if (await this.lab.setPersona(d.id, { slots: [], values: null })) { const n = this.lab.devices().find(x => x.id === d.id); if (n) this.loadDevice(n); } }
  protected macStart = '';
  async applyMACs() {
    const ids = (this.sel().size ? this.lab.devices().filter(d => this.sel().has(d.id)) : this.lab.devices()).map(d => d.id);
    if (await this.lab.setMACs(ids, this.macStart.trim())) this.macStart = '';
  }
  cpuPct(d: LabDevice) { return Math.min(100, Math.round(d.cpu ?? 0)) + ' %'; }
  async newSerials() {
    const ids = (this.sel().size ? this.lab.devices().filter(d => this.sel().has(d.id)) : this.lab.devices()).map(d => d.id);
    if (await this.lab.newSerials(ids)) this.serialsOpen.set(false);
  }
  pickImage(id: string) {
    const im = this.images().find(i => i.id === id); if (!im) return;
    this.imEdit.set({ ...im }); this.imPassword = '';
    this.day0Text = (im.day0 ?? []).join('\n');
    this.answersText = (im.answers ?? []).map(a => a.match + ' => ' + a.send).join('\n');
  }
  async saveImage() {
    const im = this.imEdit(); if (!im) return;
    const out: EmuImage = { ...im, password: this.imPassword, day0: this.day0Text.split(/\r?\n/).filter(l => l.trim()),
      answers: this.answersText.split(/\r?\n/).filter(l => l.includes('=>')).map(l => { const i = l.indexOf('=>'); return { match: l.slice(0, i).trim(), send: l.slice(i + 2).trim() }; }),
      cpus: +im.cpus || 1, ram: +im.ram || 1024, nics: +im.nics || 1 };
    if (await this.lab.saveImage(out)) this.pickImage(im.id);
  }
  async addImage() {
    const p = await this.backend.chooseOpenFile(tr('Add VM Image'), [{ displayName: tr('VM images'), pattern: '*.qcow2;*.img;*.vmdk;*.raw' }]).catch(() => '');
    if (p && await this.lab.addImage(p)) { const ims = this.images(); if (ims.length) this.pickImage(ims[ims.length - 1].id); }
  }
  async importAppliance() {
    const p = await this.backend.chooseOpenFile(tr('Import GNS3 Appliance'), [{ displayName: tr('GNS3 appliances'), pattern: '*.gns3a' }]).catch(() => '');
    if (p && await this.lab.importAppliance(p)) { const ims = this.images(); if (ims.length) this.pickImage(ims[ims.length - 1].id); }
  }
  async chooseImageFile(id: string) {
    const p = await this.backend.chooseOpenFile(tr('VM image file'), [{ displayName: tr('VM images'), pattern: '*.qcow2;*.img;*.vmdk;*.raw' }]).catch(() => '');
    if (p && await this.lab.setImageFile(id, p)) this.pickImage(id);
  }
  async removeImage() { const im = this.imEdit(); if (im && await this.ui.confirm(tr('Remove image'), tr('Forget {0}? Its copy on the hosts stays.', im.name), tr('Remove'), true) && await this.lab.removeImage(im.id)) this.imEdit.set(null); }
  // hosts
  protected hostPick = '';
  protected sshSessions = computed(() => {
    const out: string[] = [], skip = [this.lab.emu()?.folder, this.lab.sim()?.folder].filter(Boolean) as string[];
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) { if (!skip.some(f => c.path === f || c.path.startsWith(f + '/'))) walk(c); } else if (c.protocol === 'SSH2') out.push(c.path); });
    walk(this.ui.tree());
    return out;
  });
  async checkHost(path: string) { if (path) await this.lab.checkHost(path); }
  // host set-up
  protected setupOf = signal('');
  protected sudoPw = '';
  protected setupHost = computed(() => this.hosts().find(h => h.session === this.setupOf()) ?? null);
  protected running = computed(() => { const s = this.lab.setup(); return !!s && s.host === this.setupOf(); });
  openSetup(host: string) { this.setupOf.set(host); this.sudoPw = ''; if (this.lab.setup()?.done) this.lab.setup.set(null); }
  closeSetup() { this.setupOf.set(''); this.sudoPw = ''; if (this.lab.setup()?.done) this.lab.setup.set(null); }
  async runSetup(host: string) { const pw = this.sudoPw; this.sudoPw = ''; await this.lab.setupHost(host, pw); }

  pos(d: LabDevice) { return this.moved().get(d.id) ?? { x: d.x || 40, y: d.y || 60 }; }
  nameOf(id: string) { return this.lab.devices().find(d => d.id === id)?.hostname ?? id; }
  line(k: LabLink) {
    const a = this.lab.devices().find(d => d.id === k.a), b = this.lab.devices().find(d => d.id === k.b);
    if (!a || !b) return null;
    const pa = this.pos(a), pb = this.pos(b);
    const ax = pa.x + NW / 2, ay = pa.y + NH / 2, bx = pb.x + NW / 2, by = pb.y + NH / 2;
    // from the edge of each box, not its middle
    const edge = (x: number, y: number, tx: number, ty: number) => {
      const dx = tx - x, dy = ty - y; const s = Math.min(Math.abs((NW / 2) / (dx || 1e-6)), Math.abs((NH / 2) / (dy || 1e-6)));
      return { x: x + dx * Math.min(s, 1), y: y + dy * Math.min(s, 1) };
    };
    const e1 = edge(ax, ay, bx, by), e2 = edge(bx, by, ax, ay);
    return { x1: e1.x, y1: e1.y, x2: e2.x, y2: e2.y };
  }
  cablesOf(d: LabDevice) {
    const out: { port: string; peer: string; peerPort: string; up: boolean; down: boolean }[] = [];
    for (const k of this.lab.links()) {
      if (k.a === d.id) out.push({ port: k.aPort, peer: this.nameOf(k.b), peerPort: k.bPort, up: !!k.up, down: !!k.down });
      else if (k.b === d.id) out.push({ port: k.bPort, peer: this.nameOf(k.a), peerPort: k.aPort, up: !!k.up, down: !!k.down });
    }
    return out;
  }

  toggleSel(id: string) { const s = new Set(this.sel()); s.has(id) ? s.delete(id) : s.add(id); this.sel.set(s); }
  selAll(on: boolean) { this.sel.set(on ? new Set(this.lab.devices().map(d => d.id)) : new Set()); }

  // identity
  protected idEdit = signal(false);
  protected idSerial = ''; protected idMac = '';
  editId(d: LabDevice) { this.idSerial = d.serial; this.idMac = d.mac; this.idEdit.set(true); }
  async saveId(d: LabDevice) { if (await this.lab.setIdentity(d.id, this.idSerial, this.idMac)) this.idEdit.set(false); }
  protected serialsOpen = signal(false);
  protected serialText = '';
  async applySerials() {
    const ids = (this.sel().size ? this.lab.devices().filter(d => this.sel().has(d.id)) : this.lab.devices()).map(d => d.id);
    if (await this.lab.setSerials(ids, this.serialText.split(/\r?\n/))) { this.serialsOpen.set(false); this.serialText = ''; }
  }

  // command outputs
  protected outs = signal<LabOutput[]>([]);
  protected oSel = signal(0);
  openOutputs() { this.outs.set((this.lab.state()?.outputs ?? []).map(o => ({ ...o }))); this.oSel.set(0); this.tab.set('outputs'); }
  osIds() { return Object.keys(this.lab.state()?.os ?? {}).sort(); }
  addOut() { this.outs.set([...this.outs(), { os: this.osIds()[0] ?? '', command: '', text: '' }]); this.oSel.set(this.outs().length - 1); }
  dropOut() { this.outs.set(this.outs().filter((_, i) => i !== this.oSel())); this.oSel.set(Math.max(0, this.oSel() - 1)); }
  async saveOuts() { if (await this.lab.setOutputs(this.outs().filter(o => o.command.trim()))) this.openOutputs(); }

  // device pack
  protected pack = computed(() => this.lab.state()?.pack ?? null);
  protected consent = signal<{ path: string; info: LabPack } | null>(null);
  protected agree = false;
  async choosePack() {
    const path = await this.backend.chooseOpenFile(tr('Load Device Pack'), [{ displayName: tr('Device packs'), pattern: '*.vm' }]).catch(() => '');
    if (!path) return;
    try { this.agree = false; this.consent.set({ path, info: await this.lab.packInfo(path) }); } catch (e) { this.ui.error(e); }
  }
  async accept() { const c = this.consent(); if (c && await this.lab.acceptPack(c.path, c.info.hash)) this.consent.set(null); }
  async removePack() { if (await this.ui.confirm(tr('Remove device pack'), tr('Remove {0} from this computer?', this.pack()?.name ?? ''), tr('Remove'), true)) await this.lab.removePack(); }

  wizard(add: boolean) { this.ui.labWizard.set({ add, kind: this.lab.kind() }); this.ui.open('labWizard'); }

  connect(d: LabDevice) { const p = this.lab.sessionOf(d.id); if (p) void this.tabs.openSession(p); }
  openSessions() { for (const d of this.openable()) this.connect(d); this.ui.close(); }

  async removeSel() {
    const ids = [...this.sel()];
    if (!ids.length) return;
    const msg = this.emu() ? tr('Remove {0} emulated switches? They stop, their disks on the host and their sessions are deleted.', ids.length) : tr('Remove {0} simulated switches and their sessions? Their configuration is lost.', ids.length);
    if (!(await this.ui.confirm(tr('Remove switches'), msg, tr('Remove'), true))) return;
    if (await this.lab.remove(ids, true)) this.sel.set(new Set());
  }

  async showConfig(d: LabDevice) {
    const cmd = this.lab.state()?.os?.[d.os]?.configCommand;
    this.cfg.set(cmd ? await this.lab.show(d.id, cmd) : tr('The device pack names no command for the configuration.'));
  }

  // dragging in the topology
  grab(ev: PointerEvent, d: LabDevice) {
    if (this.cableMode()) return;
    const svg = (ev.target as Element).closest('svg') as SVGSVGElement;
    const p = this.toSvg(ev, svg), q = this.pos(d);
    this.dragging = { id: d.id, dx: p.x - q.x, dy: p.y - q.y, moved: false };
  }
  drag(ev: PointerEvent, svg: SVGSVGElement) {
    if (!this.dragging) return;
    const p = this.toSvg(ev, svg);
    const m = new Map(this.moved());
    m.set(this.dragging.id, { x: Math.max(0, p.x - this.dragging.dx), y: Math.max(0, p.y - this.dragging.dy) });
    this.moved.set(m);
    this.dragging.moved = true;
  }
  drop() {
    const d = this.dragging; this.dragging = null;
    if (d?.moved) { const p = this.moved().get(d.id)!; this.lab.place(d.id, p.x, p.y); }
  }
  private toSvg(ev: PointerEvent, svg: SVGSVGElement) {
    const pt = svg.createSVGPoint(); pt.x = ev.clientX; pt.y = ev.clientY;
    const m = svg.getScreenCTM(); const r = m ? pt.matrixTransform(m.inverse()) : pt;
    return { x: r.x, y: r.y };
  }

  // cables
  toggleCable() {
    if (this.cableMode()) { this.cableMode.set(false); this.cable.set(null); return; }
    this.cableMode.set(true);
    const ds = this.lab.devices();
    if (this.tab() !== 'topo' && ds.length >= 2) void this.startCable(ds[0].id, ds[1].id);
  }
  clickNode(d: LabDevice) {
    this.focus.set(d.id); this.cfg.set(''); this.tail.set('');
    if (!this.cableMode()) return;
    const c = this.cable();
    if (!c || (c.a && c.b)) { this.cable.set({ a: d.id, aPort: '', aPorts: [], b: '', bPort: '', bPorts: [] }); return; }
    if (c.a !== d.id) void this.startCable(c.a, d.id);
  }
  private async startCable(a: string, b: string) {
    const [ap, bp] = await Promise.all([this.lab.freePorts(a), this.lab.freePorts(b)]);
    this.cable.set({ a, aPort: ap[0] ?? '', aPorts: ap, b, bPort: bp[0] ?? '', bPorts: bp });
  }
  async pickA(id: string) { const c = this.cable()!; const p = await this.lab.freePorts(id); this.cable.set({ ...c, a: id, aPorts: p, aPort: p[0] ?? '' }); }
  async pickB(id: string) { const c = this.cable()!; const p = await this.lab.freePorts(id); this.cable.set({ ...c, b: id, bPorts: p, bPort: p[0] ?? '' }); }
  async addCable() {
    const c = this.cable(); if (!c) return;
    if (await this.lab.connect({ a: c.a, aPort: c.aPort, b: c.b, bPort: c.bPort })) { this.cable.set(null); this.cableMode.set(false); }
  }
}
