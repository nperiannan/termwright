import { Component, OnDestroy, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DEVICE_KINDS, DeviceIcon, DeviceKind } from '../components/device-icon';
import { Backend, errText } from '../core/backend';
import { DeviceType, DiscoverResult, SessionNode } from '../core/models';
import { Ui } from '../core/ui.service';

interface Row extends DiscoverResult { selected: boolean; exists: boolean; deviceType: string; }

const ACK_KEY = 'termwright.discoverAck';

/**
 * File → Discover Devices: scans an address range for devices (SSH, Telnet,
 * web, printing, phones, Windows ports; optional SNMP v2c system name and
 * description), shows them with an icon per kind, and adds the selected ones
 * as sessions. It never tries to log in.
 */
@Component({
  selector: 'app-discover-dialog',
  imports: [FormsModule, DeviceIcon],
  template: `
  <div class="modal-backdrop">
    <div class="modal disc">
      <div class="modal-head">Discover Devices</div>
      <div class="modal-body">
        <div class="grid">
          <label>Addresses</label>
          <input name="spec" [(ngModel)]="spec" [disabled]="running()" placeholder="10.20.0.0/24, 10.20.1.10-80 or single addresses (up to a /22)"
            (keydown.enter)="start()" spellcheck="false" />
          <label>SNMP</label>
          <div class="row">
            <label class="inline"><input type="checkbox" [(ngModel)]="useSnmp" [disabled]="running()" /> Look up name and model (SNMP v2c)</label>
            <input class="comm" type="password" [(ngModel)]="community" [disabled]="running() || !useSnmp" placeholder="community" autocomplete="off"
              title="Read-only SNMP community. It is used for this scan only and not saved." />
          </div>
          @if (!acked) {
            <label></label>
            <div class="warn">Scanning connects to many addresses and can be noticed by security monitoring. Scan only networks you are
              allowed to scan, such as your lab. Termwright never tries to log in; it checks which ports answer, reads the SSH/Telnet
              greeting and, with a community, the SNMP system description.
              <label class="ack"><input type="checkbox" [(ngModel)]="ackNow" /> I am allowed to scan these addresses</label></div>
          }
        </div>
        <div class="bar">
          @if (running()) {
            <button type="button" (click)="stop()">Stop</button>
          } @else {
            <button type="button" class="primary" [disabled]="!spec.trim() || (!acked && !ackNow)" (click)="start()">Scan</button>
          }
          @if (total()) {
            <progress [max]="total()" [value]="done()"></progress>
            <span class="hint">{{ done() }} / {{ total() }} addresses · {{ rows().length }} found{{ running() ? '' : stopped() ? ' · stopped' : ' · done' }}</span>
          }
          <span class="spacer"></span>
          @if (rows().length) {
            <label class="inline hint"><input type="checkbox" [(ngModel)]="loginOnly" /> Only devices with SSH or Telnet</label>
          }
        </div>
        @if (err()) { <p class="err">{{ err() }}</p> }
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
                  <tr [class.dim]="!r.protocol" (click)="toggle(r, $event)">
                    <td class="c"><input type="checkbox" [checked]="r.selected" [disabled]="!r.protocol"
                      [title]="r.protocol ? '' : 'No SSH or Telnet port answered - cannot be added as a session'" /></td>
                    <td><app-device-icon [kind]="r.kind" /></td>
                    <td><b>{{ r.name }}</b>@if (r.exists) { <span class="tag">in sessions</span> }
                      @if (r.dns && r.dns !== r.name) { <div class="hint">{{ r.dns }}</div> }</td>
                    <td class="mono">{{ r.ip }}</td>
                    <td>{{ kindLabel(r.kind) }}</td>
                    <td class="sys" [title]="r.sysDescr || r.banner">@if (r.vendor) { <b>{{ r.vendor }}</b> }{{ r.vendor && r.os ? ' · ' : '' }}{{ r.os }}</td>
                    <td>@for (s of r.services; track s) { <span class="svc" [class.login]="s === 'ssh' || s === 'telnet'">{{ svcLabel(s) }}</span> }</td>
                    @if (deviceTypes().length) {
                      <td (click)="$event.stopPropagation()">
                        <select [(ngModel)]="r.deviceType" [disabled]="!r.protocol">
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
        }
      </div>
      <div class="modal-foot">
        @if (rows().length) {
          <label class="inline">Add to folder</label>
          <input class="folder" [(ngModel)]="folder" list="disc-folders" placeholder="(top level)" />
          <datalist id="disc-folders">@for (f of folders(); track f) { <option [value]="f"></option> }</datalist>
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
    .comm { flex: 0 0 200px; width: 200px; }
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
    .sys { max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .svc { display: inline-block; font-size: 10.5px; padding: 0 5px; margin: 1px 3px 1px 0; border-radius: 8px; border: 1px solid var(--border); color: var(--muted); }
    .svc.login { color: #fff; background: #0d9488; border-color: transparent; }
    .tag { font-size: 10.5px; margin-left: 6px; padding: 0 6px; border-radius: 8px; background: var(--hover); color: var(--muted); font-weight: 400; }
    .folder { width: 220px; }
    .err { color: var(--danger); }
    select { max-width: 150px; }
  `],
})
export class DiscoverDialog implements OnDestroy {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  spec = '';
  useSnmp = true;
  community = 'public';
  acked = this.loadAck();
  ackNow = false;
  loginOnly = false;
  folder = 'Discovered';
  readonly running = signal(false);
  readonly stopped = signal(false);
  readonly total = signal(0);
  readonly done = signal(0);
  readonly rows = signal<Row[]>([]);
  readonly err = signal('');
  readonly adding = signal(false);
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

  shown() { return this.loginOnly ? this.rows().filter(r => r.protocol) : this.rows(); }
  selectedCount() { return this.rows().filter(r => r.selected).length; }
  allSelected() { const a = this.shown().filter(r => r.protocol); return a.length > 0 && a.every(r => r.selected); }
  kindLabel(k: string) { return DEVICE_KINDS[k as DeviceKind]?.label ?? 'Unknown device'; }
  svcLabel(s: string) {
    return ({ ssh: 'SSH', telnet: 'Telnet', http: 'HTTP', https: 'HTTPS', smb: 'SMB', rdp: 'RDP', printer: 'Print', ipp: 'IPP', sip: 'SIP', snmp: 'SNMP' } as Record<string, string>)[s] ?? s;
  }

  private loadAck() { try { return localStorage.getItem(ACK_KEY) === '1'; } catch { return false; } }

  async start() {
    if (this.running() || !this.spec.trim() || (!this.acked && !this.ackNow)) return;
    if (!this.acked) { this.acked = true; try { localStorage.setItem(ACK_KEY, '1'); } catch { /* ignore */ } }
    this.err.set('');
    this.rows.set([]);
    this.stopped.set(false);
    this.unsub.forEach(u => u());
    this.unsub = [];
    try {
      const r = await this.backend.discoverStart(this.spec.trim(), this.useSnmp ? this.community.trim() : '');
      this.id = r.id;
      this.total.set(r.total);
      this.done.set(0);
      this.running.set(true);
      this.unsub.push(
        this.backend.on('discover:found:' + r.id, (d: DiscoverResult) => this.found(d)),
        this.backend.on('discover:progress:' + r.id, (p: { done: number }) => this.done.set(p.done)),
        this.backend.on('discover:done:' + r.id, (stopped: boolean) => { this.running.set(false); this.stopped.set(!!stopped); }),
      );
    } catch (e) { this.err.set(errText(e)); }
  }

  stop() { if (this.id) void this.backend.discoverStop(this.id); }

  ngOnDestroy() { this.stop(); this.unsub.forEach(u => u()); }

  private found(d: DiscoverResult) {
    const k = this.known();
    const exists = [d.ip, d.dns, d.name].some(x => !!x && k.has(x.toLowerCase()));
    const row: Row = { ...d, services: d.services ?? [], exists, selected: !!d.protocol && !exists, deviceType: this.guessType(d) };
    this.rows.update(l => [...l, row].sort((a, b) => ipNum(a.ip) - ipNum(b.ip)));
  }

  /** Picks a device type whose name matches what the device reports (VOSS, EXOS, Linux, …). */
  private guessType(d: DiscoverResult): string {
    const types = this.deviceTypes();
    if (!types.length || !d.protocol) return '';
    const text = `${d.sysDescr} ${d.os} ${d.vendor} ${d.banner}`.toLowerCase();
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
      const t = types.find(t => words.some(w => t.name.toLowerCase().includes(w)));
      if (t) return t.name;
    }
    return '';
  }

  toggle(r: Row, ev: Event) {
    if (!r.protocol || (ev.target as HTMLElement).tagName === 'SELECT') return;
    r.selected = !r.selected;
    this.rows.update(l => [...l]);
  }

  selectAll(v: boolean) {
    const ips = new Set(this.shown().map(r => r.ip));
    this.rows.update(l => l.map(r => ips.has(r.ip) && r.protocol ? { ...r, selected: v } : r));
  }

  async add() {
    const pick = this.rows().filter(r => r.selected && r.protocol);
    if (!pick.length) return;
    this.adding.set(true);
    const folder = this.folder.trim().replace(/^\/+|\/+$/g, '');
    let added = 0;
    const failed: string[] = [];
    try {
      if (folder && !this.folders().includes(folder)) await this.backend.createFolder(folder).catch(() => {});
      for (const r of pick) {
        const s = await this.backend.newSessionDefaults();
        const base = (r.name || r.ip).replace(/[\\/:*?"<>|]+/g, '-').trim() || r.ip;
        Object.assign(s, {
          name: base, folder, path: '', protocol: r.protocol, hostname: r.ip, port: r.port,
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

function ipNum(ip: string) { return ip.split('.').reduce((n, p) => n * 256 + Number(p), 0); }
