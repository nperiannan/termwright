import { Component, OnDestroy, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DEVICE_KINDS, DeviceIcon, DeviceKind } from '../components/device-icon';
import { Backend, errText } from '../core/backend';
import { DeviceType, DiscoverResult, MovedDevice, SessionNode } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Moved } from './moved';
import { FolderPicker } from '../components/folder-picker';
import { Icon } from '../components/icon';

interface Row extends DiscoverResult { selected: boolean; exists: boolean; deviceType: string; open?: boolean; touched?: boolean; }

const ACK_KEY = 'termwright.discoverAck';

/**
 * File → Discover Devices: scans an address range for devices (SSH, Telnet,
 * web, printing, phones, Windows ports; optional SNMP v2c system name and
 * description), shows them with an icon per kind, and adds the selected ones
 * as sessions. It never tries to log in.
 */
@Component({
  selector: 'app-discover-dialog',
  imports: [FormsModule, DeviceIcon, Moved, FolderPicker, Icon],
  template: `
  <div class="modal-backdrop">
    <div class="modal disc">
      <div class="modal-head"><span class="dh-ic"><app-icon name="pulse" [size]="18" /></span>
        <span class="dh-t">Discover Devices<span class="dh-s">Find the devices on a network and add them as sessions - nothing logs in</span></span></div>
      <div class="modal-body">
        <div class="grid">
          <label>Addresses</label>
          <div>
            <div class="row specrow">
              <input name="spec" class="spec" [(ngModel)]="spec" [disabled]="running()" placeholder="192.168.1.0/24"
                (keydown.enter)="start()" spellcheck="false" />
              @if (running()) {
                <button type="button" (click)="stop()">Stop</button>
              } @else {
                <button type="button" class="primary" [disabled]="!spec.trim() || (!acked && !ackNow)" (click)="start()">Scan</button>
              }
            </div>
            <div class="hint">A network (192.168.1.0/24, or 192.168.1.0 for its /24), a range (192.168.1.10-80) or addresses, up to a /22</div>
          </div>
          <label>SNMP</label>
          <div class="row">
            <label class="inline"><input type="checkbox" [(ngModel)]="useSnmp" [disabled]="running()" /> Look up name and model (SNMP v2c)</label>
            <input class="comm" [type]="showComm ? 'text' : 'password'" [(ngModel)]="community" [disabled]="running() || !useSnmp" placeholder="community"
              autocomplete="off" spellcheck="false" title="Read-only SNMP community. It is used for this scan only and not saved." />
            <button type="button" class="icon-btn eye" (click)="showComm = !showComm" [disabled]="!useSnmp"
              [title]="showComm ? 'Hide the community' : 'Show the community'">{{ showComm ? 'Hide' : 'Show' }}</button>
          </div>
          @if (!acked) {
            <label></label>
            <div class="warn">Scanning connects to many addresses and can be noticed by security monitoring. Scan only networks you are
              allowed to scan, such as your lab. Termwright never tries to log in; it checks which ports answer, reads the SSH/Telnet
              greeting, the start page of web interfaces, asks phones over SIP and, with a community, reads the SNMP system description.
              <label class="ack"><input type="checkbox" [(ngModel)]="ackNow" /> I am allowed to scan these addresses</label></div>
          }
        </div>
        <div class="bar">
          @if (total()) {
            <progress [max]="total()" [value]="done()"></progress>
            <span class="hint">{{ done() }} / {{ total() }} addresses{{ again().total ? (running() ? ' · checking ' + again().total + ' device' + (again().total === 1 ? '' : 's') + ' again, more slowly (' + again().done + ' / ' + again().total + ')' : ' · ' + again().total + ' checked again') : '' }} · {{ rows().length }} found{{ running() ? '' : stopped() ? ' · stopped' : ' · done' }}{{ rows().length ? ' · double-click a device to connect' : '' }}</span>
          }
          <span class="spacer"></span>
          @if (rows().length) {
            <label class="inline hint"><input type="checkbox" [(ngModel)]="loginOnly" /> Only devices you can connect to</label>
          }
        </div>
        @if (err()) { <p class="err">{{ err() }}</p> }
        @if (snmpSilent()) {
          <p class="note">No device answered SNMP with this community. Check the community, that SNMP is enabled, and that the devices' SNMP access list allows this computer - with SNMP the name, model and software are shown for every device.</p>
        }
        @if (!ouiDate() && total() && !running()) {
          <p class="note">MAC addresses are shown for devices on your own subnet. <a (click)="downloadOui()">{{ ouiBusy() ? 'Downloading…' : 'Download the MAC vendor list' }}</a>
            (IEEE registry, about 3 MB, saved in the data folder) to name their makers - this also identifies phones and printers with no open ports.</p>
        }
        @if (!running() && (moved().length || noMac().length || reused().length)) {
          <div class="movedbar">
            @if (moved().length) {
              <span>⚠ <b>{{ moved().length }} saved session{{ moved().length === 1 ? '' : 's' }}</b> answer{{ moved().length === 1 ? 's' : '' }} at a new address (same MAC address).</span>
              <button type="button" class="primary" (click)="reviewMoved()">Review…</button>
            }
            @if (reused().length) {
              <span class="bad">{{ reused().length }} saved session{{ reused().length === 1 ? '' : 's' }}: the address now belongs to <b>another device</b> and the device itself was not found ({{ reused().slice(0, 3).join(', ') }}{{ reused().length > 3 ? '…' : '' }}).</span>
            }
            @if (noMac().length) {
              <span class="hint">{{ noMac().length }} saved session{{ noMac().length === 1 ? '' : 's' }} found at {{ noMac().length === 1 ? 'its' : 'their' }} address without a saved MAC address.</span>
              <button type="button" (click)="rememberMacs()" title="Save the MAC addresses so Termwright finds these devices again when their IP address changes">Remember MAC Addresses</button>
            }
          </div>
        }
        @if (rows().length) {
          <div class="tbl">
            <table class="list">
              <thead><tr>
                <th class="c"><input type="checkbox" [checked]="allSelected()" (change)="selectAll(!allSelected())" title="Select all that can be added" /></th>
                <th></th><th>Name</th><th>Address</th><th>Type</th><th>Vendor / system</th><th>Services</th>
                @if (deviceTypes().length) { <th>Device type</th> }
              </tr></thead>
              <tbody>
                @for (r of shown(); track r.ip) {
                  <tr [class.dim]="!r.protocol" [class.opened]="r.open" (click)="toggle(r, $event)" (dblclick)="connect(r, $event)" (contextmenu)="rowMenu($event, r)"
                    [title]="r.protocol ? 'Double-click to connect now (' + r.protocol + ') without saving' : ''">
                    <td class="c"><input type="checkbox" [checked]="r.selected" [disabled]="!r.protocol"
                      [title]="r.protocol ? '' : 'No SSH, Telnet, RDP or VNC port answered - cannot be added as a session'" /></td>
                    <td><app-device-icon [kind]="r.kind" /></td>
                    <td><b>{{ r.name }}</b>@if (r.self) { <span class="tag me">this computer</span> }@if (r.exists) { <span class="tag">in sessions</span> }
                      @if (r.dns && r.dns !== r.name) { <div class="hint">{{ r.dns }}</div> }</td>
                    <td class="mono">{{ r.ip }}@if (r.mac) { <div class="hint">{{ r.mac }}</div> }</td>
                    <td>{{ kindLabel(r.kind) }}</td>
                    <td class="sys">
                      <button type="button" class="info" [class.on]="r.open" (click)="details(r, $event)" title="What this is based on">ⓘ</button>
                      @if (r.vendor) { <b>{{ r.vendor }}</b> }{{ r.vendor && r.os ? ' · ' : '' }}{{ r.os }}
                      @if (r.open) { <ul class="ev">@for (e of r.evidence ?? []; track $index) { <li>{{ e }}</li> } @empty { <li>Only open ports were seen.</li> }</ul> }
                    </td>
                    <td>@for (s of r.services; track s) { <span class="svc" [class.login]="s === 'ssh' || s === 'telnet' || s === 'rdp' || s === 'vnc'">{{ svcLabel(s) }}</span> }</td>
                    @if (deviceTypes().length) {
                      <td (click)="$event.stopPropagation()">
                        <select [(ngModel)]="r.deviceType" (ngModelChange)="r.touched = true" [disabled]="!r.protocol">
                          <option value="">(none)</option>
                          @for (d of deviceTypes(); track d.name) { <option [value]="d.name">{{ d.name }}</option> }
                        </select>
                      </td>
                    }
                  </tr>
                }
              </tbody>
            </table>
          </div>
          <div class="save">
            <label class="inline">Add
              <select [ngModel]="saveMode()" (ngModelChange)="saveMode.set($event); savePrefs()">
                <option value="all">all selected devices</option>
                <option value="types">only the selected devices of these device types</option>
              </select></label>
            @if (saveMode() === 'types') {
              @for (t of foundTypes(); track t) {
                <label class="chip" [class.on]="picked().includes(t)"><input type="checkbox" [checked]="picked().includes(t)" (change)="toggleType(t)" />
                  {{ t || '(no device type)' }} <span class="n">{{ countOf(t) }}</span></label>
              } @empty { <span class="hint">No device can be added.</span> }
            }
            <span class="spacer"></span>
            <label class="inline" title="For example Discovered/EXOS and Discovered/VOSS">
              <input type="checkbox" [ngModel]="byType()" (ngModelChange)="byType.set($event); savePrefs()" /> A subfolder per device type</label>
          </div>
        }
      </div>
      @if (reviewing()) { <app-moved [embedded]="true" (closed)="reviewing.set(false)" /> }
      <div class="modal-foot">
        @if (rows().length) {
          <label class="inline">Add to folder</label>
          <app-folder-picker class="folder" [(value)]="folder" />
        }
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Close</button>
        @if (rows().length) {
          <button type="button" class="primary" [disabled]="!selectedCount() || adding()" (click)="add()">
            {{ adding() ? 'Adding…' : 'Add ' + selectedCount() + ' Session' + (selectedCount() === 1 ? '' : 's') }}</button>
        }
      </div>
    </div>
  </div>`,
  styles: [`
    .disc { width: min(1100px, 95vw); }
    .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
    .inline { display: inline-flex; gap: 6px; align-items: center; white-space: nowrap; }
    .comm { flex: 0 0 220px; width: 220px !important; max-width: 220px; }
    .disc .specrow > input.spec { flex: 0 1 340px; width: 340px; min-width: 160px; }
    .eye { font-size: calc(11px * var(--ui-scale, 1)); width: auto; padding: 0 6px; }
    .ack { display: flex; gap: 6px; align-items: center; margin-top: 6px; font-weight: 600; }
    .bar { display: flex; gap: 10px; align-items: center; margin: 12px 0 8px; }
    .bar progress { width: 220px; accent-color: var(--accent); }
    .spacer { flex: 1; }
    .tbl { max-height: 52vh; overflow: auto; border: 1px solid var(--border); border-radius: 6px; }
    table { width: 100%; }
    th { position: sticky; top: 0; background: var(--panel); z-index: 1; }
    td, th { vertical-align: middle; }
    tr { cursor: default; }
    tr.dim td { opacity: .6; }
    .c { width: 28px; text-align: center; }
    .sys { max-width: 340px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    tr.opened .sys { white-space: normal; }
    .info { border: 0; background: none; padding: 0 4px 0 0; height: auto; color: var(--muted); cursor: pointer; font-size: calc(13px * var(--ui-scale, 1)); }
    .info.on, .info:hover { color: var(--accent); }
    .ev { margin: 4px 0 2px; padding-left: 16px; font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--muted); white-space: normal; user-select: text; }
    .tag.me { background: color-mix(in srgb, var(--accent) 25%, transparent); color: var(--text); }
    .note { font-size: calc(12px * var(--ui-scale, 1)); color: var(--muted); margin: 0 0 8px; a { color: var(--accent); cursor: pointer; } }
    .svc { display: inline-block; font-size: calc(10.5px * var(--ui-scale, 1)); padding: 0 5px; margin: 1px 3px 1px 0; border-radius: 8px; border: 1px solid var(--border); color: var(--muted); }
    .svc.login { color: #fff; background: #0d9488; border-color: transparent; }
    .tag { white-space: nowrap; font-size: calc(10.5px * var(--ui-scale, 1)); margin-left: 6px; padding: 0 6px; border-radius: 8px; background: var(--hover); color: var(--muted); font-weight: 400; }
    .folder { width: 280px; }
    .dh-ic { display: none; }
    .dh-s { display: none; }
    /* ---- modern layout ---- */
    :host-context(.modern) {
      .disc { width: min(1160px, 95vw); }
      .modal-head { display: flex; align-items: center; gap: 12px; }
      .dh-ic { display: inline-flex; width: 34px; height: 34px; border-radius: 9px; align-items: center; justify-content: center; background: var(--accent-soft); color: var(--accent); }
      .dh-t { display: flex; flex-direction: column; } .dh-s { display: block; font-size: calc(12px * var(--ui-scale, 1)); font-weight: 400; color: var(--muted); }
      .grid { grid-template-columns: 90px 1fr; }
      .grid > label:empty { display: block !important; }
      .disc .specrow > input.spec { height: 36px; font-family: var(--mono); flex-basis: 380px; width: 380px; border-radius: var(--r); }
      .specrow button { height: 36px; padding: 0 20px; border-radius: var(--r); }
      .bar { margin: 16px 0 10px; }
      .bar progress { appearance: none; -webkit-appearance: none; height: 6px; width: 260px; border: 0; border-radius: 3px; overflow: hidden; background: var(--surface-2); }
      .bar progress::-webkit-progress-bar { background: var(--surface-2); border-radius: 3px; }
      .bar progress::-webkit-progress-value { background: var(--accent); border-radius: 3px; }
      .tbl { border-color: var(--line); border-radius: var(--r-lg); background: var(--surface); }
      table.list { margin: 0; border-collapse: separate; border-spacing: 0; }
      th { background: var(--surface-2); color: var(--faint); font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .05em; text-transform: uppercase;
        padding: 8px 8px; border-bottom: 1px solid var(--line); }
      td { padding: 7px 8px; border-bottom: 1px solid var(--line); }
      tbody tr:last-child td { border-bottom: 0; }
      tbody tr:hover td { background: var(--hover); }
      app-device-icon { transform: scale(1.35); transform-origin: center; display: inline-flex; margin: 0 4px; }
      td .hint { font-size: calc(11px * var(--ui-scale, 1)); }
      .mono { font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); }
      .svc { border-radius: 99px; padding: 1px 7px; font-weight: 600; }
      .svc.login { background: color-mix(in srgb, var(--ok) 22%, transparent); color: var(--ok); }
      .tag { border-radius: 99px; background: var(--surface-2); border: 1px solid var(--line); }
      td select { height: 28px; border-radius: var(--r-sm); }
      .save { margin-top: 12px; }
      .chip { border-radius: 99px; padding: 3px 10px; }
      .movedbar { border-radius: var(--r); }
    }
    .save { display: flex; flex-wrap: wrap; gap: 6px 8px; align-items: center; margin-top: 10px; font-size: calc(12.5px * var(--ui-scale, 1)); }
    .save select { max-width: 330px; }
    .chip { display: inline-flex; gap: 5px; align-items: center; padding: 2px 8px; border: 1px solid var(--border); border-radius: 12px; cursor: pointer; }
    .chip.on { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 15%, transparent); }
    .chip .n { color: var(--muted); font-size: calc(11px * var(--ui-scale, 1)); }
    .err { color: var(--danger); }
    .movedbar .bad { color: var(--danger); }
    .movedbar { display: flex; flex-wrap: wrap; gap: 8px 12px; align-items: center; padding: 8px 10px; margin-bottom: 8px; border: 1px solid color-mix(in srgb, #f59e0b 55%, var(--border)); border-radius: 6px; background: color-mix(in srgb, #f59e0b 9%, transparent); font-size: calc(12.5px * var(--ui-scale, 1)); }
    select { max-width: 150px; }
  `],
})
export class DiscoverDialog implements OnDestroy {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  spec = '';
  useSnmp = true;
  community = 'public';
  showComm = false;
  acked = this.loadAck();
  ackNow = false;
  loginOnly = false;
  folder = 'Discovered';
  private readonly prefs = loadPrefs();
  /** Which selected devices "Add" saves: all, or only those of the picked device types. */
  readonly saveMode = signal<'all' | 'types'>(this.prefs.mode === 'types' ? 'types' : 'all');
  readonly picked = signal<string[]>(Array.isArray(this.prefs.types) ? this.prefs.types : []);
  readonly byType = signal(!!this.prefs.byType);
  readonly running = signal(false);
  readonly stopped = signal(false);
  /** The second, slower look at devices that showed no login port. */
  readonly again = signal({ done: 0, total: 0 });
  readonly total = signal(0);
  readonly done = signal(0);
  readonly rows = signal<Row[]>([]);
  readonly err = signal('');
  readonly adding = signal(false);
  readonly ouiDate = signal('');
  readonly ouiBusy = signal(false);
  private usedCommunity = false;
  private id = '';
  private unsub: (() => void)[] = [];

  readonly deviceTypes = computed<DeviceType[]>(() => this.ui.settings()?.deviceTypes ?? []);
  readonly folders = computed(() => {
    const out: string[] = [];
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) { out.push(c.path); walk(c); } });
    walk(this.ui.tree());
    return out;
  });
  /** Host names and addresses already in the Session Manager. */
  private readonly known = computed(() => {
    const s = new Set<string>();
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) walk(c); else if (c.hostname) s.add(c.hostname.toLowerCase()); });
    walk(this.ui.tree());
    return s;
  });

  /** Saved sessions (by MAC address) whose device answers at another address now. */
  readonly moved = computed<MovedDevice[]>(() => {
    const byMac = new Map<string, string>();
    for (const r of this.rows()) if (r.mac) byMac.set(r.mac.toLowerCase(), r.ip);
    const out: MovedDevice[] = [];
    for (const n of this.sessionNodes()) {
      const mac = normMac(n.mac);
      const now = mac ? byMac.get(mac) : undefined;
      if (now && n.hostname && now !== n.hostname) out.push({ path: n.path, old: n.hostname, new: now, mac });
    }
    return out;
  });
  private readonly knownMacs = computed(() => new Set(this.sessionNodes().map(n => normMac(n.mac)).filter(Boolean)));
  /** Saved sessions found at their address whose MAC address is not saved yet. */
  readonly noMac = computed(() => {
    const byIp = new Map(this.rows().filter(r => r.mac && !r.self).map(r => [r.ip, r.mac]));
    return this.sessionNodes().filter(n => !n.mac && n.hostname && byIp.has(n.hostname) && !n.jump && !n.proxy)
      .map(n => ({ path: n.path, mac: byIp.get(n.hostname!)! }));
  });
  private sessionNodes() {
    const out: SessionNode[] = [];
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => { if (c.isFolder) walk(c); else out.push(c); });
    walk(this.ui.tree());
    return out;
  }

  readonly reviewing = signal(false);
  reviewMoved() { this.ui.movedInput.set({ items: this.moved() }); this.reviewing.set(true); }

  /** Saved sessions whose address now answers with another MAC, and whose own MAC was not seen. */
  readonly reused = computed(() => {
    const byIp = new Map(this.rows().filter(r => r.mac).map(r => [r.ip, r.mac]));
    const seen = new Set(this.rows().filter(r => r.mac).map(r => r.mac.toLowerCase()));
    return this.sessionNodes().filter(n => {
      const mac = normMac(n.mac);
      const now = n.hostname ? byIp.get(n.hostname) : '';
      return !!mac && !!now && normMac(now) !== mac && !seen.has(mac);
    }).map(n => n.path);
  });

  async rememberMacs() {
    const list = this.noMac();
    if (!(await this.ui.confirm('Remember MAC Addresses', `Save the MAC address seen at the address of ${list.length === 1 ? 'this session' : 'these ' + list.length + ' sessions'}?\n\n` +
      list.slice(0, 12).map(x => `${x.path}  ${x.mac}`).join('\n') + (list.length > 12 ? '\n…' : '') +
      '\n\nDo this only when these addresses still belong to the right devices.', 'Save'))) return;
    const macs = Object.fromEntries(this.noMac().map(x => [x.path, x.mac]));
    try {
      const n = await this.backend.rememberMacs(macs);
      await this.ui.refreshTree();
      this.ui.toast(`MAC address saved in ${n} session${n === 1 ? '' : 's'}`, 'success');
    } catch (e) { this.ui.error(e); }
  }

  constructor() { this.backend.discoverOuiStatus().then(d => this.ouiDate.set(d)).catch(() => {}); }

  /** SNMP was asked but no device answered (wrong community, ACL, SNMP off). */
  snmpSilent() { return this.usedCommunity && !this.running() && this.rows().length > 0 && !this.rows().some(r => r.services.includes('snmp')); }

  async downloadOui() {
    if (this.ouiBusy()) return;
    this.ouiBusy.set(true);
    try {
      const n = await this.backend.discoverDownloadOui();
      this.ouiDate.set(new Date().toISOString().slice(0, 10));
      this.ui.toast(`MAC vendor list downloaded (${n.toLocaleString()} vendors) - scan again to use it`, 'success');
    } catch (e) { this.err.set(errText(e)); }
    this.ouiBusy.set(false);
  }

  details(r: Row, ev: Event) {
    ev.stopPropagation();
    r.open = !r.open;
    this.rows.update(l => [...l]);
  }

  shown() { return this.loginOnly ? this.rows().filter(r => r.protocol) : this.rows(); }
  /** The devices "Add" saves. */
  toAdd() {
    const types = this.saveMode() === 'types' ? new Set(this.picked()) : null;
    return this.rows().filter(r => r.selected && r.protocol && (!types || types.has(r.deviceType)));
  }
  selectedCount() { return this.toAdd().length; }
  /** Device types of the devices that can be added ("" = none), named ones first. */
  foundTypes() {
    const set = new Set(this.rows().filter(r => r.protocol).map(r => r.deviceType ?? ''));
    return [...set].sort((a, b) => (a === '') !== (b === '') ? (a === '' ? 1 : -1) : a.localeCompare(b));
  }
  countOf(t: string) { return this.rows().filter(r => r.selected && r.protocol && (r.deviceType ?? '') === t).length; }
  toggleType(t: string) {
    this.picked.update(l => l.includes(t) ? l.filter(x => x !== t) : [...l, t]);
    this.savePrefs();
  }
  savePrefs() {
    try { localStorage.setItem(PREFS_KEY, JSON.stringify({ mode: this.saveMode(), types: this.picked(), byType: this.byType() })); } catch { /* ignore */ }
  }
  allSelected() { const a = this.shown().filter(r => r.protocol); return a.length > 0 && a.every(r => r.selected); }
  kindLabel(k: string) { return DEVICE_KINDS[k as DeviceKind]?.label ?? 'Unknown device'; }
  svcLabel(s: string) {
    return ({ ssh: 'SSH', telnet: 'Telnet', http: 'HTTP', https: 'HTTPS', smb: 'SMB', rdp: 'RDP', printer: 'Print', ipp: 'IPP', sip: 'SIP', 'sip-udp': 'SIP', mdns: 'mDNS', snmp: 'SNMP', esphome: 'ESPHome', mqtt: 'MQTT', modbus: 'Modbus', coap: 'CoAP', vnc: 'VNC' } as Record<string, string>)[s] ?? s;
  }

  private loadAck() { try { return localStorage.getItem(ACK_KEY) === '1'; } catch { return false; } }

  async start() {
    if (this.running() || !this.spec.trim() || (!this.acked && !this.ackNow)) return;
    if (!this.acked) { this.acked = true; try { localStorage.setItem(ACK_KEY, '1'); } catch { /* ignore */ } }
    this.err.set('');
    this.rows.set([]);
    this.stopped.set(false);
    this.again.set({ done: 0, total: 0 });
    this.unsub.forEach(u => u());
    this.unsub = [];
    try {
      this.usedCommunity = this.useSnmp && !!this.community.trim();
      const r = await this.backend.discoverStart(this.spec.trim(), this.useSnmp ? this.community.trim() : '');
      this.id = r.id;
      this.total.set(r.total);
      this.done.set(0);
      this.running.set(true);
      this.unsub.push(
        this.backend.on('discover:found:' + r.id, (d: DiscoverResult) => this.found(d)),
        this.backend.on('discover:progress:' + r.id, (p: { done: number }) => this.done.set(p.done)),
        this.backend.on('discover:again:' + r.id, (p: { done: number; total: number }) => this.again.set(p)),
        this.backend.on('discover:done:' + r.id, (stopped: boolean) => { this.running.set(false); this.stopped.set(!!stopped); }),
      );
    } catch (e) { this.err.set(errText(e)); }
  }

  stop() { if (this.id) void this.backend.discoverStop(this.id); }

  ngOnDestroy() { this.stop(); this.unsub.forEach(u => u()); }

  private found(d: DiscoverResult) {
    const k = this.known();
    // a saved session of this device - also when it moved to this address (same MAC)
    const exists = [d.ip, d.dns, d.name].some(x => !!x && k.has(x.toLowerCase())) || (!!d.mac && this.knownMacs().has(normMac(d.mac)));
    const prev = this.rows().find(r => r.ip === d.ip);
    // a device can be reported again at the end of the scan with its MAC address
    const row: Row = { ...d, services: d.services ?? [], exists, open: prev?.open, touched: prev?.touched,
      selected: prev?.touched ? prev.selected : !!d.protocol && !exists && !d.self,
      deviceType: prev?.touched ? prev.deviceType : this.guessType(d) };
    this.rows.update(l => [...l.filter(r => r.ip !== d.ip), row].sort((a, b) => ipNum(a.ip) - ipNum(b.ip)));
  }

  /** Picks a device type whose name matches what the device reports (VOSS, EXOS, Linux, …). */
  private guessType(d: DiscoverResult): string {
    const types = this.deviceTypes();
    if (!types.length || !d.protocol || d.protocol === 'RDP' || d.protocol === 'VNC') return '';
    const named = (words: string[]) => types.find(t => words.some(w => t.name.toLowerCase().includes(w)));
    // the network OS any evidence named wins over a generic "Linux" from OpenSSH
    if (d.platform) {
      const t = named(PLATFORM_WORDS[d.platform] ?? [d.platform.toLowerCase()]);
      if (t) return t.name;
    }
    const text = `${d.sysDescr} ${d.os} ${d.vendor} ${d.web?.cert ?? ''} ${d.telnet} ${d.web?.title ?? ''} ${(d.web?.hints ?? []).join(' ')} ${d.sip} ${d.banner}`.toLowerCase();
    const direct = types.find(t => t.name.length > 2 && text.includes(t.name.toLowerCase()));
    if (direct) return direct.name;
    const aliases: [RegExp, string[]][] = [
      [/voss|vsp|fabric ?engine/, ['voss', 'fabric']], [/exos|extremexos|switch ?engine|summit/, ['exos', 'switch engine']],
      [/nx-os/, ['nx-os', 'nxos', 'cisco']], [/cisco/, ['ios', 'cisco']], [/junos|juniper/, ['junos', 'juniper']],
      [/arista|\beos\b/, ['eos', 'arista']], [/linux|ubuntu|debian|centos|red ?hat|rocky|alma|amazon|openssh/, ['linux', 'server']],
      [/windows/, ['windows']],
    ];
    for (const [re, words] of aliases) {
      if (!re.test(text)) continue;
      const t = named(words);
      if (t) return t.name;
    }
    // The Telnet login prompt against each device type's first login step
    // (EXOS "login:", VOSS "Login:" …); for servers prefer a Linux-like type.
    const prompt = (d.telnet ?? '').trim().split(' / ').pop() ?? '';
    if (prompt) {
      const fits = types.filter(t => { const e = t.loginSteps?.[0]?.expect?.trim(); return !!e && prompt.endsWith(e); });
      const pick = d.kind === 'server' ? fits.find(t => /linux|unix|server/i.test(t.name)) : fits.find(t => !/linux|unix|server/i.test(t.name));
      if (pick ?? fits.length === 1) return (pick ?? fits[0]).name;
    }
    return '';
  }

  /** A session for a device, as Add saves it. */
  private async sessionFor(r: Row, folder = '') {
    const s = await this.backend.newSessionDefaults();
    Object.assign(s, {
      name: (r.name || r.ip).replace(/[\\/:*?"<>|]+/g, '-').trim() || r.ip, folder, path: '', protocol: r.protocol, hostname: r.ip, port: r.port, mac: r.mac || '',
      deviceType: r.deviceType, description: [r.vendor, r.os].filter(Boolean).length ? [[r.vendor, r.os].filter(Boolean).join(' · ')] : [],
    });
    return s;
  }

  /** Opens a tab to the device without saving a session; the dialog stays open. */
  async connect(r: Row, ev?: Event) {
    if ((ev?.target as HTMLElement | undefined)?.closest('select, button, input')) return;
    if (!r.protocol) return;
    try {
      await this.tabs.openAdHoc(await this.sessionFor(r));
      this.ui.toast(`Connecting to ${r.name || r.ip} - the tab opens behind this window`, 'info');
    } catch (e) { this.ui.error(e); }
  }

  rowMenu(ev: MouseEvent, r: Row) {
    this.ui.showMenu(ev, [
      { label: r.protocol ? `Connect (${r.protocol})` : 'No SSH, Telnet, RDP or VNC port answered', disabled: !r.protocol, action: () => void this.connect(r) },
      { label: 'Copy Address', action: () => void this.backend.clipboardSet(r.ip) },
      { separator: true },
      { label: r.selected ? 'Do Not Add' : 'Add as a Session', disabled: !r.protocol, action: () => { r.selected = !r.selected; r.touched = true; this.rows.update(l => [...l]); } },
    ]);
  }

  toggle(r: Row, ev: Event) {
    if (!r.protocol || (ev.target as HTMLElement).tagName === 'SELECT') return;
    r.selected = !r.selected;
    r.touched = true;
    this.rows.update(l => [...l]);
  }

  selectAll(v: boolean) {
    const ips = new Set(this.shown().map(r => r.ip));
    this.rows.update(l => l.map(r => ips.has(r.ip) && r.protocol ? { ...r, selected: v } : r));
  }

  async add() {
    const pick = this.toAdd();
    if (!pick.length) return;
    this.adding.set(true);
    const folder = this.folder.trim().replace(/^\/+|\/+$/g, '');
    let added = 0;
    const failed: string[] = [];
    try {
      const made = new Set(this.folders());
      const ensure = async (f: string) => {
        if (f && !made.has(f)) { made.add(f); await this.backend.createFolder(f).catch(() => {}); }
      };
      await ensure(folder);
      for (const r of pick) {
        const sub = this.byType() ? (r.deviceType || 'Other').replace(/[\\/:*?"<>|]+/g, '-') : '';
        const dest = [folder, sub].filter(Boolean).join('/');
        await ensure(dest);
        const s = await this.backend.newSessionDefaults();
        const base = (r.name || r.ip).replace(/[\\/:*?"<>|]+/g, '-').trim() || r.ip;
        Object.assign(s, {
          name: base, folder: dest, path: '', protocol: r.protocol, hostname: r.ip, port: r.port, mac: r.mac || '',
          deviceType: r.deviceType, description: [r.vendor, r.os].filter(Boolean).length ? [[r.vendor, r.os].filter(Boolean).join(' · ')] : [],
        });
        try {
          await this.backend.saveSession(s, '');
        } catch {
          try { s.name = `${base} (${r.ip})`; await this.backend.saveSession(s, ''); } catch (e) { failed.push(`${r.ip}: ${errText(e)}`); continue; }
        }
        added++;
        r.selected = false;
        r.exists = true;
      }
    } finally {
      this.adding.set(false);
      this.rows.update(l => [...l]);
      await this.ui.refreshTree();
    }
    if (failed.length) this.err.set('Not added: ' + failed.join('; '));
    this.ui.toast(`Added ${added} session${added === 1 ? '' : 's'}${folder ? ' to ' + folder : ''}`, 'success');
  }
}

/** Words in device type names for each network OS the scan recognises. */
const PLATFORM_WORDS: Record<string, string[]> = {
  EXOS: ['exos', 'switch engine'], VOSS: ['voss', 'fabric'], BOSS: ['boss'], 'NX-OS': ['nx-os', 'nxos'], 'IOS XR': ['ios xr', 'ios-xr', 'xr'],
  'IOS XE': ['ios xe', 'ios-xe', 'ios'], IOS: ['ios', 'cisco'], Junos: ['junos', 'juniper'], EOS: ['eos', 'arista'], FortiOS: ['forti'],
  'PAN-OS': ['pan-os', 'palo'], RouterOS: ['routeros', 'mikrotik'], Comware: ['comware', 'h3c'], ArubaOS: ['aruba', 'aos'],
};

const PREFS_KEY = 'termwright.discover.save';
function loadPrefs(): { mode?: string; types?: string[]; byType?: boolean } {
  try { return JSON.parse(localStorage.getItem(PREFS_KEY) ?? '{}') ?? {}; } catch { return {}; }
}

function ipNum(ip: string) { return ip.split('.').reduce((n, p) => n * 256 + Number(p), 0); }

function normMac(s: string | undefined) {
  const bare = (s ?? '').toLowerCase().replace(/[^0-9a-f]/g, '');
  return bare.length === 12 ? bare.match(/../g)!.join(':') : '';
}
