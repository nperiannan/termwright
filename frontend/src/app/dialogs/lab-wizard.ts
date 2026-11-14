import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { LabService } from '../core/lab.service';
import { EmuDeviceType, EmuGroup, EmuHostInfo, LabGroup, LabModel, SessionNode } from '../core/models';
import { Backend } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Tabs } from '../core/tabs.service';
import { tr } from '../core/i18n';
import { Icon } from '../components/icon';

interface Row extends LabGroup { os: string; }
interface DRow { key: string; image: string; module: string; count: number; names: string; serial: string; mac: string; cpus: number; ram: number; diskGb: number; open: boolean; }
const CABLING = [
  { v: 'leaf-spine', label: 'Leaf-spine', hint: 'The first two switches (the first row) as the core pair with a link between them; every other switch to both cores.' },
  { v: 'chain', label: 'Chain', hint: 'Each switch to the next one.' },
  { v: 'ring', label: 'Ring', hint: 'A chain closed back to the first switch.' },
  { v: 'none', label: 'No cables', hint: 'Draw them yourself in the Lab window.' },
];

/** New Lab / Add Switches: where it runs, which switches, cabling, review. */
@Component({
  selector: 'app-lab-wizard',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop" (click)="ui.close()">
    <div class="modal labz" (click)="$event.stopPropagation()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="server" [size]="17" /></span>
        <span class="mh-t">{{ add ? tr('Add Switches') : tr('New Lab') }}<span class="mh-s">{{ tr('Step {0} of {1}', step() - first + 1, 4 - first) }}</span></span></div>
      <div class="wiz">
        <div class="steps">
          @for (s of stepNames; track $index) { @if ($index >= first) {
            <div [class.on]="$index === step()" [class.done]="$index < step()">{{ $index < step() ? '✓ ' : '' }}{{ $index - first + 1 }}  {{ tr(s) }}</div> } }
        </div>
        <div class="body">
          @switch (step()) {
            @case (0) {
              <b>{{ tr('Where should the switches run?') }}</b>
              <div class="choice">
                <div class="c" [class.on]="kind() === 'sim'" [class.off]="!lab.sim()?.pack" (click)="setKind('sim')"><b>{{ tr('This computer - simulated') }}</b>
                  <span class="muted small">{{ lab.sim()?.pack ? tr('Starts in a second, a few MB per switch, from the device pack {0}.', lab.sim()?.pack?.name ?? '') : tr('Needs a device pack (Lab window → Load Device Pack).') }}</span>
                  <ul><li>{{ tr('Up to 500 switches') }}</li><li>{{ tr('Not the real NOS, no protocols') }}</li><li>{{ tr('Works offline on macOS, Windows and Linux') }}</li></ul></div>
                <div class="c" [class.on]="kind() === 'emu'" (click)="setKind('emu')"><b>{{ tr('Linux KVM host - real VM images') }}</b><span class="muted small">{{ tr('Boots the vendors’ own VM images (.qcow2) with QEMU/KVM on a Linux server you reach over SSH. The real OS runs: protocols, APIs, cloud agents.') }}</span>
                  <ul><li>{{ tr('About 1-4 GB of memory and 1-2 vCPU per switch, 1-3 minutes to boot') }}</li><li>{{ tr('Termwright starts, wires and opens the VMs') }}</li></ul></div>
              </div>
              @if (kind() === 'emu') {
                <div class="bar"><span>{{ tr('KVM host') }}</span>
                  <select [(ngModel)]="host" name="host" class="hsel" (ngModelChange)="hostInfo.set(hostOf($event))">@for (s of hostChoices(); track s.path) { <option [value]="s.path">{{ s.label }}</option> }</select>
                  <button type="button" (click)="check()" [disabled]="!host || lab.busy()">{{ hostInfo() ? tr('Check Again') : tr('Check') }}</button>
                  @if (lab.busy()) { <span class="muted small">{{ tr('Logging in and checking…') }}</span> }</div>
                @if (hostInfo(); as h) {
                  <div class="kpis"><div><small>vCPU</small><b>{{ h.cpus }}</b><span class="muted">{{ tr('load') }} {{ h.load }}</span></div><div><small>{{ tr('RAM free') }}</small><b>{{ gb(h.memFree) }}</b><span class="muted">/ {{ gb(h.memTotal) }}</span></div>
                    <div><small>{{ tr('Disk free') }}</small><b>{{ gb(h.diskFree) }}</b></div><div><small>{{ tr('Fits about') }}</small><span class="fits">@for (f of fits(); track f.name) { {{ f.n }} × {{ f.name }}<br> } @empty { - }</span></div></div>
                  <div class="checks">
                    <div><span [class]="h.user ? 'ok' : 'bad'">{{ h.user ? '✓' : '✕' }}</span> {{ tr('SSH login as {0}', h.user) }} - {{ h.os }}, {{ h.kernel }}</div>
                    <div><span [class]="h.kvm === 'yes' ? 'ok' : 'warn'">{{ h.kvm === 'yes' ? '✓' : '!' }}</span> KVM: {{ h.kvm === 'yes' ? tr('/dev/kvm usable') : h.kvm === 'noaccess' ? tr('no access to /dev/kvm') : tr('none - software emulation') }}</div>
                    <div><span [class]="h.qemu ? 'ok' : 'bad'">{{ h.qemu ? '✓' : '✕' }}</span> QEMU {{ h.qemu || tr('missing') }}</div>
                    <div><span class="ok">✓</span> {{ tr('Lab folder ~/{0} - images and switch disks are kept here', h.dir) }}</div>
                    @for (p of h.problems ?? []; track $index) { <div class="bad">✕ {{ p }}</div> }
                    @if (h.canSetup) { <div><button type="button" class="sm primary" (click)="setUp()">{{ tr('Set Up QEMU/KVM on This Host…') }}</button> <span class="muted small">{{ tr('installs QEMU from the distribution’s packages and lets {0} use KVM', h.user) }}</span></div> }
                    @for (p of h.notes ?? []; track $index) { <div class="warn">! {{ p }}</div> }
                  </div>
                } @else { <p class="muted small">{{ tr('Pick the SSH session of a Linux server, then Check. When QEMU or KVM access is missing, Set Up installs it from the distribution’s packages.') }}</p> }
              }
            }
            @case (1) {
              @if (kind() === 'emu') {
              @if (!deviceTypes().length) {
                <div class="empty2"><b>{{ tr('No images yet') }}</b><p class="muted small">{{ tr('Add the VM image files you downloaded (for example the EXOS or VOSS VM). Termwright recognises them and lists the devices they can be.') }}</p>
                  <button type="button" class="primary" (click)="addImage(-1)">{{ tr('Add Image File…') }}</button></div>
              } @else {
              <div class="tbl"><table class="dev">
                <thead><tr><th>{{ tr('Device') }}</th><th>{{ tr('Software') }}</th><th>{{ tr('How many') }}</th><th>{{ tr('Names') }}</th><th></th></tr></thead>
                <tbody>@for (r of drows(); track $index) {
                  <tr><td><select class="devsel" [ngModel]="r.key" (ngModelChange)="setDevice($index, $event)" [name]="'dk' + $index">
                      @for (g of deviceGroups(); track g.name) { <optgroup [label]="g.name">@for (d of g.items; track d.key) { <option [value]="d.key">{{ d.model || d.kind }}{{ d.model && d.osName ? ' · ' + d.osName : '' }}</option> }</optgroup> }</select></td>
                    <td>@if (dtOf(r.key)?.images?.length) { <select [ngModel]="r.image" (ngModelChange)="dpatch($index, { image: $event })" [name]="'di' + $index">@for (id of dtOf(r.key)!.images; track id) { <option [value]="id">{{ imageLabel(id) }}</option> }</select> }
                      @else { <button type="button" class="sm primary" (click)="addImage($index)">{{ tr('Add Image File…') }}</button> }</td>
                    <td><input type="number" min="1" max="200" [ngModel]="r.count" (ngModelChange)="dpatch($index, { count: +$event })" [name]="'dn' + $index" class="num" /></td>
                    <td><input class="mono" [ngModel]="r.names" (ngModelChange)="dpatch($index, { names: $event })" [name]="'dm' + $index" spellcheck="false" [title]="tr('{n} is the number')" /></td>
                    <td><button type="button" class="sm" (click)="ddrop($index)" [disabled]="drows().length === 1" [title]="tr('Remove')">✕</button></td></tr>
                  <tr class="sub"><td colspan="5"><div class="subrow">
                    @if (dtOf(r.key)?.modules?.length) { <label>{{ dtOf(r.key)!.modName || tr('Module') }} <select [ngModel]="r.module" (ngModelChange)="dpatch($index, { module: $event })" [name]="'dv' + $index"><option value="">{{ tr('none') }}</option>@for (m of dtOf(r.key)!.modules; track m) { <option [value]="m">{{ m }}</option> }</select></label> }
                    <label>{{ tr('Serial from') }} <input class="mono ser" [ngModel]="r.serial" (ngModelChange)="dpatch($index, { serial: $event })" [name]="'ds' + $index" spellcheck="false" /></label>
                    <label>{{ tr('MAC from') }} <input class="mono mac" [ngModel]="r.mac" (ngModelChange)="dpatch($index, { mac: $event })" [name]="'dc' + $index" spellcheck="false" /></label>
                    <button type="button" class="sm" (click)="suggest($index)" [title]="tr('Make a new serial number and MAC address in the model’s format')">↻ {{ tr('New') }}</button>
                    <a class="res" (click)="dpatch($index, { open: !r.open })">{{ r.open ? '▾' : '▸' }} {{ tr('{0} vCPU · {1}', r.cpus, gb(r.ram)) }}{{ r.diskGb ? ' · ' + r.diskGb + ' GB' : '' }}</a>
                    @if (r.open) { <label>vCPU <input type="number" min="1" [ngModel]="r.cpus" (ngModelChange)="dpatch($index, { cpus: +$event })" [name]="'dpc' + $index" class="num s" /></label>
                      <label>RAM <input type="number" min="256" step="256" [ngModel]="r.ram" (ngModelChange)="dpatch($index, { ram: +$event })" [name]="'dpr' + $index" class="num" /> MB</label>
                      <label>{{ tr('Disk') }} <input type="number" min="0" [ngModel]="r.diskGb" (ngModelChange)="dpatch($index, { diskGb: +$event })" [name]="'dpd' + $index" class="num s" /> GB</label> }
                  </div></td></tr>
                }</tbody></table></div>
              <div class="bar"><button type="button" class="sm" (click)="dmore()">+ {{ tr('Add Another Device') }}</button><span class="spacer"></span><a class="muted small" (click)="addImage(-1)">{{ tr('Add an image file…') }}</a></div>
              }
              @if (hostInfo(); as h) {
                <div class="kpis three"><div><small>vCPU</small><b>{{ need().cpus }}</b><span class="muted">{{ tr('of {0}', h.cpus) }}</span><i class="meter"><i [style.width.%]="pct(need().cpus, h.cpus * 2)"></i></i></div>
                  <div><small>RAM</small><b>{{ gb(need().ram) }}</b><span class="muted">{{ tr('of {0} free', gb(h.memFree)) }}</span><i class="meter"><i [style.width.%]="pct(need().ram, h.memFree)" [class.over]="need().ram > h.memFree"></i></i></div>
                  <div><small>{{ tr('Disk') }}</small><b>~ {{ gb(need().disk) }}</b><span class="muted">{{ tr('switch disks') }}</span><i class="meter"><i [style.width.%]="pct(need().disk, h.diskFree)"></i></i></div></div>
                @if (need().ram > h.memFree) { <p class="bad small">{{ tr('The switches need more memory than the host has free.') }}</p> }
              }
              @if (!add) {
                <b class="sub">{{ tr('Management') }}</b>
                <div class="choice four">
                  <label class="c" [class.on]="mgmt === 'lan'" [class.off]="!lanOk()"><input type="radio" name="mg" value="lan" [(ngModel)]="mgmt" [disabled]="!lanOk()" /><b>{{ tr('The host’s network (bridged)') }}</b>
                    <span class="muted small">{{ lanText() }}</span></label>
                  <label class="c" [class.on]="mgmt === 'nat'" [class.off]="!natOk()"><input type="radio" name="mg" value="nat" [(ngModel)]="mgmt" [disabled]="!natOk()" /><b>{{ tr('The host’s NAT network') }}</b>
                    <span class="muted small">{{ natText() }}</span></label>
                  <label class="c" [class.on]="mgmt === 'forward'"><input type="radio" name="mg" value="forward" [(ngModel)]="mgmt" /><b>{{ tr('Port forwarding on the host') }}</b><span class="muted small">{{ tr('No root needed. Each switch’s management port is reached through the host (127.0.0.1:port on the host); its sessions use the host session as jump host.') }}</span></label>
                  <label class="c" [class.on]="mgmt === 'bridge'" [class.off]="!hostInfo()?.sudo"><input type="radio" name="mg" value="bridge" [(ngModel)]="mgmt" [disabled]="!hostInfo()?.sudo" /><b>{{ tr('Lab bridge on the host') }}</b><span class="muted small">{{ tr('Needs password-less sudo. The switches get addresses in {0} on a bridge of their own on the host, reachable from the host only.', mgmtNet) }}</span></label>
                </div>
              }
              } @else {
              <div class="tbl"><table>
                <thead><tr><th>OS</th><th>{{ tr('Model') }}</th><th>{{ tr('How many') }}</th><th>{{ tr('Names') }}</th><th>{{ tr('Ports') }}</th><th></th></tr></thead>
                <tbody>
                @for (r of rows(); track $index) {
                  <tr><td>@if ((modelOf(r.model)?.os?.length ?? 0) > 1) {
                        <select class="ossel" [ngModel]="r.os" (ngModelChange)="patch($index, { os: $event })" [name]="'o' + $index" [title]="tr('This model runs more than one operating system')">@for (o of modelOf(r.model)!.os; track o) { <option [value]="o">{{ osShort(o) }}</option> }</select>
                      } @else { <span class="os">{{ osShort(r.os) }}</span> }</td>
                    <td><select [ngModel]="r.model" (ngModelChange)="setModel($index, $event)" [name]="'m' + $index">
                      @for (f of families(); track f) { <optgroup [label]="f">@for (m of models(f); track m.sku) { <option [value]="m.sku">{{ m.sku }}</option> }</optgroup> }</select></td>
                    <td><input type="number" min="0" max="200" [ngModel]="r.count" (ngModelChange)="patch($index, { count: +$event })" [name]="'n' + $index" class="num" /></td>
                    <td><input class="mono" [ngModel]="r.names" (ngModelChange)="patch($index, { names: $event })" [name]="'p' + $index" spellcheck="false" /></td>
                    <td class="small" [title]="modelOf(r.model)?.layout ?? ''">{{ modelOf(r.model)?.ports }} <span class="muted">· {{ modelOf(r.model)?.layout }}</span></td>
                    <td><button type="button" class="sm" (click)="drop($index)" [disabled]="rows().length === 1" [title]="tr('Remove')">✕</button></td></tr>
                }
                </tbody></table></div>
              <div class="bar"><button type="button" class="sm" (click)="more()">+ {{ tr('Add a model') }}</button><span class="spacer"></span>
                @if (!add) { <span class="muted">{{ tr('Management network') }}</span><input class="mono net" [(ngModel)]="mgmtNet" name="net" spellcheck="false" /> }
                <span class="muted">{{ tr('Password') }}</span><input type="password" [(ngModel)]="password" name="pw" [placeholder]="tr('any')" class="pw" autocomplete="new-password" /></div>
              <p class="muted small">{{ tr('{n} in a name is the number. The switches log in as admin; without a password any password is taken. They listen on this computer only (127.0.0.1).') }}</p>
              }
            }
            @case (2) {
              <div class="presets">
                @for (c of cabling; track c.v) {
                  <label class="c" [class.on]="cable === c.v"><input type="radio" name="cab" [value]="c.v" [(ngModel)]="cable" /> <b>{{ tr(c.label) }}</b><span class="muted small">{{ tr(c.hint) }}</span></label>
                }
              </div>
              <div class="prev"><svg [attr.viewBox]="'0 0 ' + preview().w + ' 200'">
                @for (l of preview().lines; track $index) { <line [attr.x1]="l[0]" [attr.y1]="l[1]" [attr.x2]="l[2]" [attr.y2]="l[3]" [class.isc]="l[4]" /> }
                @for (n of preview().nodes; track $index) { <g [class.old]="n.old"><rect [attr.x]="n.x" [attr.y]="n.y" width="120" height="30" rx="7" /><text [attr.x]="n.x + 60" [attr.y]="n.y + 19">{{ n.name }}</text></g> }
              </svg></div>
            }
            @case (3) {
              @if (kind() === 'emu') {
              <div class="kv">
                <span>{{ tr('Runs on') }}</span><span>{{ hostLabel() }} - {{ tr('real VM images') }}</span>
                <span>{{ tr('Switches') }}</span><span>{{ summary() }}</span>
                <span>{{ tr('Cabling') }}</span><span>{{ tr(cablingLabel()) }}</span>
                <span>{{ tr('Management') }}</span><span>{{ mgmtLabel(add ? (lab.emu()?.mgmt ?? '') : mgmt) }}</span>
                <span>{{ tr('Day-0 config') }}</span><span>{{ tr('The images’ first-boot commands, typed on each console') }}</span>
                <span>{{ tr('Sessions') }}</span><span>{{ tr('Folder') }}: {{ lab.emu()?.folder || 'Emulated Lab' }} {{ tr('(SSH and console per switch)') }}</span>
              </div>
              <label class="chk"><input type="checkbox" [(ngModel)]="start" name="st" /> {{ tr('Start the switches now') }}</label>
              <p class="muted small">{{ tr('Images not on the host yet are copied first. Boot takes 1-3 minutes per switch.') }}</p>
              } @else {
              <div class="kv">
                <span>{{ tr('Runs on') }}</span><span>{{ tr('This computer - simulated') }}</span>
                <span>{{ tr('Switches') }}</span><span>{{ summary() }}</span>
                <span>{{ tr('Cabling') }}</span><span>{{ tr(cablingLabel()) }}</span>
                <span>{{ tr('Day-0 config') }}</span><span>{{ tr('Hostname, management address, login admin') }}{{ password ? '' : ' ' + tr('(any password)') }}</span>
                <span>{{ tr('Management') }}</span><span class="mono">{{ add ? (lab.sim()?.mgmtNet || mgmtNet) : mgmtNet }}</span>
                <span>{{ tr('Sessions') }}</span><span>{{ tr('Folder') }}: {{ lab.sim()?.folder || 'Simulated Lab' }}</span>
              </div>
              <label class="chk"><input type="checkbox" [(ngModel)]="start" name="st" /> {{ tr('Start the switches now') }}</label>
              <p class="muted small">{{ tr('Protocols need the real operating system - they come with KVM hosts.') }}</p>
              }
            }
          }
        </div>
      </div>
      <div class="modal-foot wfoot"><span class="muted small sumtxt" [title]="summary()">{{ summary() }}</span><span class="spacer"></span>
        <button type="button" (click)="ui.close()">{{ tr('Cancel') }}</button>
        @if (step() > first) { <button type="button" (click)="step.set(step() - 1)">{{ tr('Back') }}</button> }
        @if (step() < 3) { <button type="button" class="primary" (click)="step.set(step() + 1)" [disabled]="!canNext()">{{ tr('Next') }}</button> }
        @else { <button type="button" class="primary" (click)="create()" [disabled]="!total() || lab.busy()">{{ add ? tr('Add {0} Switches', total()) : tr('Create Lab') }}</button> }
      </div>
    </div>
  </div>`,
  styles: [`
    .labz { width: min(980px, 96vw); }
    .wfoot button { white-space: nowrap; flex: none; }
    .wfoot .sumtxt { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .wiz { display: grid; grid-template-columns: 190px 1fr; min-height: 430px; }
    .steps { border-right: 1px solid var(--line); padding: 14px 10px; background: var(--surface-2); }
    .steps div { padding: 8px 10px; border-radius: 8px; color: var(--muted); margin-bottom: 4px; font-size: 13px; }
    .steps div.on { background: var(--accent-soft); color: var(--text); font-weight: 600; } .steps div.done { color: var(--ok); }
    .body { padding: 14px 18px; min-width: 0; }
    .choice { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-top: 10px; }
    .choice .c { border: 1px solid var(--line); border-radius: 12px; padding: 14px; background: var(--surface); }
    .choice .c.on { border-color: var(--accent); box-shadow: 0 0 0 2px var(--accent-soft); } .choice .c.off { opacity: .6; }
    .choice b { display: block; font-size: 14px; margin-bottom: 4px; } .choice ul { margin: 8px 0 0; padding-left: 18px; font-size: 12.5px; color: var(--muted); }
    .soon { font-size: 10.5px; font-weight: 600; padding: 1px 7px; border-radius: 9px; background: var(--surface-2); color: var(--muted); margin-left: 4px; }
    .tbl { border: 1px solid var(--line); border-radius: 10px; overflow: auto; max-height: 260px; }
    table { width: 100%; border-collapse: collapse; } th { background: var(--surface-2); text-align: left; font-size: 12px; font-weight: 600; padding: 7px 10px; }
    td { padding: 5px 10px; border-top: 1px solid var(--line); }
    input.num { width: 70px; } input.net { width: 130px; } input.pw { width: 110px; }
    .bar { display: flex; gap: 8px; align-items: center; margin: 10px 0; flex-wrap: wrap; } .spacer { flex: 1; }
    button.sm { height: 26px; padding: 0 9px; font-size: 12px; }
    .mono { font-family: var(--mono); font-size: 12px; }
    .os { font-size: 10.5px; font-weight: 700; padding: 1px 6px; border-radius: 5px; background: color-mix(in srgb, #7c5cff 18%, transparent); color: #8f75ff; }
    .ossel { height: 26px; font-size: 12px; }
    
    .presets { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; }
    .presets .c { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; display: flex; flex-direction: column; gap: 4px; cursor: pointer; background: var(--surface); }
    .presets .c.on { border-color: var(--accent); box-shadow: 0 0 0 2px var(--accent-soft); } .presets input { display: none; }
    .prev { margin-top: 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--bg); overflow: auto; }
    .prev svg { display: block; width: 100%; min-width: 500px; height: 220px; }
    .prev rect { fill: var(--surface); stroke: var(--line); } .prev .old rect { stroke-dasharray: 4 3; }
    .prev text { fill: var(--text); font: 600 11px var(--ui-font); text-anchor: middle; }
    .prev line { stroke: var(--ok); stroke-width: 1.6; } .prev line.isc { stroke: var(--accent); stroke-width: 3; }
    .choice .c { cursor: pointer; } .choice.three { grid-template-columns: repeat(3, 1fr); } .choice.four { grid-template-columns: repeat(4, 1fr); } .choice.four .c { padding: 11px; } .choice input { display: none; } b.sub { display: block; margin-top: 12px; }
    .hsel { min-width: 330px; } .ok { color: var(--ok); } .bad { color: var(--danger); } .warn { color: var(--warn); }
    .kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; margin: 4px 0 10px; } .kpis.three { grid-template-columns: repeat(3, 1fr); }
    .kpis > div { border: 1px solid var(--line); border-radius: 10px; padding: 8px 12px; background: var(--surface); display: flex; flex-direction: column; gap: 2px; }
    .kpis small { color: var(--muted); font-size: 11.5px; } .kpis b { font-size: 20px; } .kpis .fits { font-size: 13px; font-weight: 600; }
    .meter { display: block; height: 6px; border-radius: 4px; background: var(--surface-2); overflow: hidden; margin-top: 4px; } .meter i { display: block; height: 100%; background: var(--accent); } .meter i.over { background: var(--danger); }
    .checks { border: 1px solid var(--line); border-radius: 10px; padding: 4px 12px; } .checks div { padding: 5px 0; border-top: 1px solid var(--line); font-size: 13px; } .checks div:first-child { border-top: 0; }
    input.num.s { width: 56px; } .devsel { width: 220px; } table.dev td > select:not(.devsel) { width: 190px; } table.dev td > input.mono { width: 110px; } table.dev td > input.num { width: 60px; }
    table.dev tr.sub td { border-top: 0; padding-top: 0; padding-bottom: 9px; }
    .subrow { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; font-size: 12px; color: var(--muted);
      label { display: inline-flex; gap: 6px; align-items: center; } input, select { height: 26px; font-size: 12px; } .ser { width: 140px; } .mac { width: 142px; } .res { cursor: pointer; color: var(--accent); } }
    .empty2 { text-align: center; padding: 30px; border: 1px dashed var(--line); border-radius: 12px; p { max-width: 480px; margin: 6px auto 14px; } }
    .kv { display: grid; grid-template-columns: 140px 1fr; gap: 8px 12px; font-size: 13px; margin-bottom: 14px; } .kv > span:nth-child(odd) { color: var(--muted); }
    .chk { display: inline-flex; gap: 8px; align-items: center; }
  `],
})
export class LabWizard {
  protected ui = inject(Ui);
  private tabs = inject(Tabs);
  protected lab = inject(LabService);
  private backend = inject(Backend);
  protected tr = tr;
  protected kind = signal<'sim' | 'emu'>(this.ui.labWizard().kind ?? this.lab.kind());
  protected add = this.ui.labWizard().add && !!(this.kind() === 'emu' ? this.lab.emu() : this.lab.sim())?.exists;
  protected first = this.add ? 1 : 0;
  protected step = signal(this.first);
  protected stepNames = ['Where it runs', 'Switches', 'Cabling', 'Review'];
  protected cabling = CABLING;
  protected cable = 'leaf-spine';
  protected mgmtNet = '10.99.0.0/16';
  protected password = '';
  protected start = true;
  protected rows = signal<Row[]>([]);
  constructor() {
    this.rows.set(this.firstRows());
    const e = this.lab.emu();
    this.host = e?.host || e?.hosts?.[0]?.session || this.hostChoices()[0]?.path || '';
    this.hostInfo.set(this.hostOf(this.host));
    this.mgmt = this.bestMgmt(this.hostInfo());
    this.erows.set(this.images().slice(0, 2).map((im, i) => ({ image: im.id, count: i ? 4 : 2, names: (i ? 'leaf' : 'core') + '-{n}', cpus: im.cpus || 1, ram: im.ram || 1024 })));
    if (this.add && this.kind() === 'emu') this.erows.set(this.erows().slice(0, 1).map(r => ({ ...r, count: 1, names: 'sw-{n}' })));
    void this.firstDevices();
  }
  osShort(os: string) { return this.lab.sim()?.os?.[os]?.short || os.toUpperCase(); }
  setKind(k: 'sim' | 'emu') { if (k === 'sim' && !this.lab.sim()?.pack) return; this.kind.set(k); }
  // emulated lab
  protected host = '';
  protected hostInfo = signal<EmuHostInfo | null>(null);
  protected mgmt = 'forward';
  protected erows = signal<EmuGroup[]>([]);
  protected images = computed(() => this.lab.emu()?.images ?? []);
  protected templates = computed(() => this.lab.emu()?.templates ?? []);
  setSource(i: number, v: string) {
    if (v.startsWith('t:')) { const t = this.templates().find(x => x.id === v.slice(2)); if (t) this.epatch(i, { template: t.id, image: t.image, cpus: t.cpus, ram: t.ram, diskGb: t.diskGb, names: t.name.toLowerCase().replace(/[^a-z0-9]+/g, '-') + '-{n}' }); }
    else { const im = this.imageOf(v.slice(2)); this.epatch(i, { template: '', image: v.slice(2), cpus: im?.cpus || 1, ram: im?.ram || 1024, diskGb: 0 }); }
  }
  imageOf(id: string) { return this.images().find(i => i.id === id); }
  hostOf(path: string) { return this.lab.emu()?.hosts?.find(h => h.session === path)?.info ?? null; }
  protected hostChoices = computed(() => {
    const out: { path: string; label: string }[] = [], skip = [this.lab.emu()?.folder, this.lab.sim()?.folder].filter(Boolean) as string[];
    for (const h of this.lab.emu()?.hosts ?? []) out.push({ path: h.session, label: h.name + ' (' + tr('checked') + ')' });
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) { if (!skip.some(f => c.path === f || c.path.startsWith(f + '/'))) walk(c); } else if (c.protocol === 'SSH2' && !out.some(o => o.path === c.path)) out.push({ path: c.path, label: c.path }); });
    walk(this.ui.tree());
    return out;
  });
  hostLabel() { return this.hostChoices().find(h => h.path === this.host)?.label.replace(/ \(.*\)$/, '') ?? this.host; }
  // devices (emulated): a model the images can be, the image (version) to use, serial and MAC start
  protected deviceTypes = computed(() => this.lab.emu()?.deviceTypes ?? []);
  protected deviceGroups = computed(() => {
    const g = new Map<string, EmuDeviceType[]>();
    for (const d of this.deviceTypes()) { const n = d.model ? (d.osName || d.kind) : this.tr('Images as they are'); g.set(n, [...(g.get(n) ?? []), d]); }
    return [...g].map(([name, items]) => ({ name, items }));
  });
  protected drows = signal<DRow[]>([]);
  dtOf(key: string) { return this.deviceTypes().find(d => d.key === key); }
  dtLabel(key: string) { const d = this.dtOf(key); return d ? (d.model ? d.model + (d.osName ? ' ' + d.osName : '') : d.kind) : ''; }
  imageLabel(id: string) { const im = this.imageOf(id); return im ? (im.version ? im.version + ' · ' : '') + im.fileName : id; }
  dpatch(i: number, p: Partial<DRow>) { this.drows.set(this.drows().map((r, j) => (j === i ? { ...r, ...p } : r))); }
  private newRow(key: string, count: number): DRow {
    const d = this.dtOf(key);
    const base = (d?.model || d?.kind || 'sw').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
    return { key, image: d?.images?.[0] ?? '', module: '', count, names: base + '-{n}', serial: '', mac: '', cpus: d?.cpus || 1, ram: d?.ram || 2048, diskGb: 0, open: false };
  }
  async setDevice(i: number, key: string) {
    const r = this.newRow(key, this.drows()[i]?.count || 1);
    this.dpatch(i, r); await this.suggest(i);
  }
  async suggest(i: number) {
    const r = this.drows()[i]; const d = r && this.dtOf(r.key);
    const s = await this.backend.emuSuggest(d?.model || d?.kind || '').catch(() => null);
    if (s) this.dpatch(i, { serial: s.serial, mac: s.mac });
  }
  async dmore() { const d = this.deviceTypes()[0]; if (!d) return; this.drows.set([...this.drows(), this.newRow(d.key, 1)]); await this.suggest(this.drows().length - 1); }
  ddrop(i: number) { this.drows.set(this.drows().filter((_, j) => j !== i)); }
  async addImage(i: number) {
    const p = await this.backend.chooseOpenFile(tr('Add VM Image'), [{ displayName: tr('VM images'), pattern: '*.qcow2;*.img;*.vmdk;*.raw' }]).catch(() => '');
    if (!p || !(await this.lab.addImage(p))) return;
    if (i >= 0) { const r = this.drows()[i]; const d = this.dtOf(r.key); if (d?.images?.length) this.dpatch(i, { image: d.images[0] }); }
    else if (!this.drows().length && this.deviceTypes().length) await this.dmore();
  }
  private async firstDevices() {
    const want = this.lab.wizardTemplate(); this.lab.wizardTemplate.set('');
    const t = want ? this.templates().find(x => x.id === want) : undefined;
    const ds = this.deviceTypes();
    if (t) {
      const key = ds.find(d => d.model === t.model && d.images.includes(t.image))?.key ?? ds.find(d => d.images.includes(t.image))?.key ?? '';
      if (key) { this.drows.set([{ ...this.newRow(key, 1), image: t.image, module: t.module, cpus: t.cpus, ram: t.ram, diskGb: t.diskGb, serial: t.serialStart, mac: t.macStart }]); if (!t.serialStart || !t.macStart) await this.suggest(0); return; }
    }
    const first = ds.find(d => d.images.length) ?? ds[0];
    if (first) { this.drows.set([this.newRow(first.key, this.add ? 1 : 2)]); await this.suggest(0); }
  }
  setUp() { this.lab.setupFor.set(this.host); this.ui.open('lab'); }
  async check() { const h = await this.lab.checkHost(this.host); if (h) { this.hostInfo.set(h.info); if (!this.add) this.mgmt = this.bestMgmt(h.info); } }
  /** The first port joins the host's network (DHCP) whenever the host allows it. */
  /** As Termwright picks it: the host's network on a real host, its NAT network on a VM host, then a lab bridge, then port forwarding. */
  bestMgmt(h: EmuHostInfo | null) {
    const vm = !!h?.virt && h.virt !== 'none' && h.virt !== 'unknown';
    return h?.sudo && h.lanKind && !vm ? 'lan' : h?.sudo && h.nat ? 'nat' : h?.sudo && h.lanKind ? 'lan' : h?.sudo ? 'bridge' : 'forward';
  }
  natOk() { const h = this.hostInfo(); return !!h?.sudo && (!!h.nat || !!h.libvirt); }
  natText() {
    const h = this.hostInfo();
    if (!h?.sudo) return tr('Needs password-less sudo on the host.');
    if (!h.nat && !h.libvirt) return tr('The host has no NAT network yet: Set Up QEMU/KVM installs libvirt’s (virbr0).');
    return tr('Like GNS3’s NAT node: the switches get addresses from the host’s DHCP ({0}), reach out through the host, and are reached through the host. Works with any image.', h.nat || 'virbr0');
  }
  lanOk() { const h = this.hostInfo(); return !!h?.sudo && !!h.lanKind; }
  lanText() {
    const h = this.hostInfo();
    if (!h) return '';
    if (!h.sudo) return tr('Needs password-less sudo on the host.');
    if (!h.lanKind) return h.wifi ? tr('The host’s network port is Wi-Fi; switches cannot join it.') : tr('The host has no network port to join.');
    return (h.lanKind === 'bridge' ? tr('Each switch’s first port joins the bridge {0} of the host', h.lanIf) : tr('Each switch’s first port joins the network of {0} (macvtap; the host’s own set-up is not changed)', h.lanIf))
      + ' - ' + tr('it gets its address from your DHCP server, so this computer and the cloud reach it directly.');
  }
  mgmtLabel(m: string) { return m === 'lan' ? tr('The host’s network (bridged, DHCP)') : m === 'bridge' ? tr('Lab bridge on the host') : tr('Port forwarding on the host'); }
  protected fits = computed(() => { const h = this.hostInfo(); if (!h) return []; return this.images().slice(0, 3).map(im => ({ name: im.name, n: Math.max(0, Math.min(Math.floor(h.memFree / (im.ram || 1024)), Math.floor(h.cpus * 2 / (im.cpus || 1)))) })); });
  protected need = computed(() => this.drows().reduce((n, r) => ({ cpus: n.cpus + r.count * (r.cpus || 1), ram: n.ram + r.count * (r.ram || 1024), disk: n.disk + r.count * 1024 }), { cpus: 0, ram: 0, disk: 0 }));
  pct(a: number, b: number) { return b ? Math.min(100, Math.round(a * 100 / b)) : 0; }
  gb(mb: number) { return mb >= 1024 ? (mb / 1024).toFixed(mb % 1024 ? 1 : 0) + ' GB' : mb + ' MB'; }
  epatch(i: number, p: Partial<EmuGroup>) { this.erows.set(this.erows().map((r, j) => (j === i ? { ...r, ...p } : r))); }
  setImage(i: number, id: string) { const im = this.imageOf(id); this.epatch(i, { image: id, cpus: im?.cpus || 1, ram: im?.ram || 1024 }); }
  emore() { const im = this.images()[0]; if (im) this.erows.set([...this.erows(), { image: im.id, count: 1, names: 'sw-{n}', cpus: im.cpus || 1, ram: im.ram || 1024 }]); }
  edrop(i: number) { this.erows.set(this.erows().filter((_, j) => j !== i)); }
  canNext() {
    if (this.step() === 0) return this.kind() === 'sim' ? !!this.lab.sim()?.pack : !!this.hostInfo() && (this.hostInfo()!.problems ?? []).length === 0 && this.images().length > 0;
    if (this.step() === 1) return this.total() > 0;
    return true;
  }

  protected total = computed(() => (this.kind() === 'emu' ? this.drows().filter(r => !!r.image) : this.rows()).reduce((n, r) => n + (r.count > 0 ? r.count : 0), 0));
  protected summary = computed(() => {
    const parts = this.kind() === 'emu' ? this.drows().filter(r => r.count > 0 && r.image).map(r => `${r.count} × ${this.dtLabel(r.key)}`) : this.rows().filter(r => r.count > 0).map(r => `${r.count} × ${r.model}`);
    return parts.length ? tr('{0} switches', this.total()) + ' (' + parts.join(', ') + ')' : tr('No switches chosen');
  });
  protected cablingLabel = () => CABLING.find(c => c.v === this.cable)?.label ?? '';

  families(): string[] { return [...new Set((this.lab.sim()?.models ?? []).map(m => m.family))]; }
  models(family: string): LabModel[] { return (this.lab.sim()?.models ?? []).filter(m => m.family === family); }
  modelOf(name: string) { return (this.lab.sim()?.models ?? []).find(m => m.sku === name); }
  setModel(i: number, model: string) {
    const m = this.modelOf(model);
    const os = m?.os.includes(this.rows()[i].os) ? this.rows()[i].os : (m?.os[0] ?? '');
    this.patch(i, { model, os, names: this.nameFor(model) });
  }
  private nameFor(model: string) { return 'sw-' + model.split('-').find(p => /\d/.test(p))!.toLowerCase().replace(/[^a-z0-9]+/g, '') + '-{n}'; }
  /** First rows: a model of each family (two each); adding: one row. */
  private firstRows(): Row[] {
    const ms = this.lab.sim()?.models ?? [];
    const pick = this.add ? ms.slice(0, 1) : this.families().slice(0, 3).map(f => this.models(f)[0]);
    return pick.filter(Boolean).map(m => ({ os: m.os[0], model: m.sku, count: 2, names: this.nameFor(m.sku) }));
  }
  patch(i: number, p: Partial<Row>) { this.rows.set(this.rows().map((r, j) => (j === i ? { ...r, ...p } : r))); }
  more() { const m = (this.lab.sim()?.models ?? [])[0]; if (m) this.rows.set([...this.rows(), { os: m.os[0], model: m.sku, count: 1, names: this.nameFor(m.sku) }]); }
  drop(i: number) { this.rows.set(this.rows().filter((_, j) => j !== i)); }

  /** A sketch of the cabling (the real port choice happens in Termwright). */
  protected preview = computed(() => {
    const names: { name: string; os: string; old?: boolean }[] = [];
    const em = this.kind() === 'emu';
    if (this.add) for (const d of (em ? this.lab.emu() : this.lab.sim())?.devices ?? []) names.push({ name: d.hostname, os: d.os, old: true });
    const used = new Set(names.map(n => n.name.toLowerCase()));
    for (const r of em ? this.drows().map(r => ({ ...r, os: '' })) : this.rows()) {
      let n = 1;
      for (let i = 1; i <= Math.min(r.count, 12); i++) { // numbered on past the names in use, as Termwright does
        let name = '';
        const pat = r.names.includes('{n}') || em && r.count === 1 ? r.names : r.names + '-{n}';
        do { name = pat.includes('{n}') ? pat.replace('{n}', String(n)) : n > 1 ? pat + '-' + (n - 1) : pat; n++; } while (used.has(name.toLowerCase()));
        used.add(name.toLowerCase());
        names.push({ name, os: r.os });
      }
    }
    // keep the last switches there already (the new ones are cabled to them) and the new ones
    const olds = names.filter(n => n.old), news = names.filter(n => !n.old);
    const keepOld = this.cable === 'leaf-spine' ? olds.slice(0, 2) : olds.slice(-Math.min(olds.length, 3));
    const shown = [...keepOld, ...news].slice(0, 16);
    let cores: number[] = [];
    if (this.cable === 'leaf-spine') {
      cores = shown.length > 1 ? [0, 1] : [0];
    }
    const top = shown.map((_, i) => i).filter(i => cores.includes(i)), bottom = shown.map((_, i) => i).filter(i => !cores.includes(i));
    const w = Math.max(560, Math.max(top.length, bottom.length) * 135 + 20);
    const at = new Map<number, { x: number; y: number }>();
    top.forEach((i, k) => at.set(i, { x: (w - top.length * 135) / 2 + k * 135, y: 18 }));
    bottom.forEach((i, k) => at.set(i, { x: (w - bottom.length * 135) / 2 + k * 135, y: cores.length ? 150 : 85 }));
    const lines: [number, number, number, number, boolean][] = [];
    const mid = (i: number) => ({ x: at.get(i)!.x + 60, y: at.get(i)!.y + 15 });
    const join = (a: number, b: number, isc = false) => { const p = mid(a), q = mid(b); lines.push([p.x, p.y, q.x, q.y, isc]); };
    if (this.cable === 'leaf-spine') {
      if (top.length === 2) join(top[0], top[1], true);
      for (const l of bottom) if (!shown[l].old) for (const c of top) join(c, l);
    } else if (this.cable === 'chain' || this.cable === 'ring') {
      const fresh = shown.map((_, i) => i).filter(i => !shown[i].old);
      const lastOld = shown.map((_, i) => i).filter(i => shown[i].old).pop();
      const seq = lastOld !== undefined && fresh.length ? [lastOld, ...fresh] : fresh;
      for (let k = 0; k + 1 < seq.length; k++) join(seq[k], seq[k + 1]);
      if (this.cable === 'ring' && seq.length > 2) join(seq[seq.length - 1], seq[0]);
    }
    return { w, lines, nodes: shown.map((n, i) => ({ ...n, ...at.get(i)! })) };
  });

  async create() {
    if (this.kind() === 'emu') {
      const groups = this.drows().filter(r => r.count > 0 && r.image).map(r => ({ image: r.image, model: this.dtOf(r.key)?.model ?? '', module: r.module, count: r.count, names: r.names, serialStart: r.serial, macStart: r.mac, cpus: r.cpus, ram: r.ram, diskGb: r.diskGb }));
      const ok = await this.lab.addEmu({ host: this.add ? (this.lab.emu()?.host ?? this.host) : this.host, groups, cabling: this.cable, mgmt: this.mgmt, mgmtNet: this.mgmtNet, start: this.start });
      if (ok) this.done();
      return;
    }
    const ok = await this.lab.add({
      groups: this.rows().filter(r => r.count > 0).map(r => ({ os: r.os, model: r.model, count: r.count, names: r.names })),
      mgmtNet: this.add ? '' : this.mgmtNet, cabling: this.cable, password: this.password, start: this.start,
    });
    if (ok) this.done();
  }
  /** Back to the lab: its workspace tab when it is open, else the Lab window. */
  private done() { if (this.tabs.tabs().some(t => t.kind === 'lab')) this.ui.close(); else this.ui.open('lab'); }
}
