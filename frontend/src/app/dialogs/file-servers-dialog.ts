import { Component, OnDestroy, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Tabs } from '../core/tabs.service';
import { Icon } from '../components/icon';
import { FileServers, pct, size } from '../core/file-servers.service';
import { FSAddress, FileServer, FirewallState, FileServersSettings, ServedFile, ServerCommand } from '../core/models';
import { ANY, Fill, Proto, deviceTypes, fill, needsStandardPort, template } from '../core/device-commands';
import { tr } from '../core/i18n';

/**
 * File Servers: TFTP, FTP and HTTP servers on this computer that devices fetch
 * software images and licences from, and send configurations to. One folder,
 * one address; uploads only when turned on; they stop on their own.
 */
@Component({
  selector: 'app-file-servers-dialog',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop" (click)="ui.close()">
    <div class="modal srv" (click)="$event.stopPropagation()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="server" [size]="17" /></span>
        <span class="mh-t">File Servers<span class="mh-s">Let devices fetch and send files to this computer - firmware, configurations, licences</span></span></div>
      @if (s(); as st) {
      <div class="modal-body">
        <div class="row"><span class="lab">Folder</span><span class="path" [title]="st.folder">{{ st.folder }}</span>
          <button type="button" (click)="chooseFolder()" [disabled]="anyRunning()" [title]="anyRunning() ? tr('Stop the servers to change the folder') : ''">Change…</button>
          <button type="button" (click)="showFolder()">Show</button></div>
        <div class="row"><span class="lab">Listen on</span>
          <select [(ngModel)]="st.address" name="addr" (ngModelChange)="save()" [disabled]="anyRunning()">
            @for (a of addresses(); track a.ip) { <option [value]="a.ip">{{ a.label }}{{ a.reaches ? ' · ' + tr('reaches') + ' ' + reachName() : '' }}</option> }
            <option value="*">{{ tr('All addresses of this computer') }}</option>
          </select>
          <span class="muted small">{{ st.address === '*' ? tr('Every network') : tr('Only this address') }}</span></div>
        <div class="cards">
          @for (k of kinds; track k) {
            @let sv = server(k);
            <div class="card" [class.on]="sv?.running">
              <div class="ct"><span class="dot"></span>{{ k.toUpperCase() }}<span class="grow"></span>
                @if (sv?.running) { <button type="button" class="primary" (click)="stop(k)">Stop</button> }
                @else { <button type="button" (click)="start(k)" [disabled]="busy() === k">{{ busy() === k ? tr('Starting…') : tr('Start') }}</button> }</div>
              @if (k === 'tftp') {
                <div class="kv">Port <input type="number" min="1" max="65535" [(ngModel)]="st.tftpPort" name="tp" (change)="save()" (input)="fwSoon()" [disabled]="!!sv?.running"> <span class="muted small">UDP</span></div>
                <label class="ck"><input type="checkbox" [(ngModel)]="st.tftpWrite" name="tw" (ngModelChange)="save()"> Devices can upload files</label>
              } @else if (k === 'ftp') {
                <div class="kv">Port <input type="number" min="1" max="65535" [(ngModel)]="st.ftpPort" name="fp" (change)="save()" (input)="fwSoon()" [disabled]="!!sv?.running"> <span class="muted small">TCP</span></div>
                <div class="kv"><span class="k">User</span><input class="w" [(ngModel)]="st.ftpUser" name="fu" (change)="save()" [disabled]="!!sv?.running" spellcheck="false" aria-label="FTP user name"></div>
                <div class="kv"><span class="k">Password</span><input class="w" type="password" value="xxxxxxxxxxxx" readonly tabindex="-1" aria-label="FTP password (hidden)">
                  <button type="button" class="sm" (click)="copyPassword()">Copy</button>
                  <button type="button" class="sm icon" (click)="newPassword()" [title]="tr('Makes a new password - devices use it from their next login')" aria-label="New password"><app-icon name="refresh" [size]="13" /></button></div>
                <label class="ck"><input type="checkbox" [(ngModel)]="st.ftpWrite" name="fw" (ngModelChange)="save()"> Devices can upload files</label>
              } @else {
                <div class="kv">Port <input type="number" min="1" max="65535" [(ngModel)]="st.httpPort" name="hp" (change)="save()" (input)="fwSoon()" [disabled]="!!sv?.running"> <span class="muted small">TCP</span></div>
                <label class="ck"><input type="checkbox" checked disabled> Downloads only (devices cannot upload)</label>
              }
              @if (sv?.error) { <div class="err small">{{ sv!.error }}</div> }
              @else if (sv?.running) { <div class="muted small">{{ tr('Running on') }} {{ sv!.address || tr('all addresses') }}:{{ sv!.port }}{{ activeOf(k) ? ' · ' + activeOf(k) + ' ' + tr(activeOf(k) === 1 ? 'transfer' : 'transfers') : '' }}</div> }
              @else { <div class="muted small">Stopped</div> }
            </div>
          }
        </div>
        <div class="fwp">
          <div class="fwh"><app-icon name="shield" [size]="15" /><b>{{ tr('Firewall') }}</b><span class="muted small">{{ fw()?.name || '' }}</span><span class="grow"></span>
            @if (pasteTab(); as t) {
              <button type="button" (click)="testFrom(t.id, t.title)" [disabled]="!anyRunning() || fs.test().state === 'wait'"
                [title]="anyRunning() ? tr('Types a command in {0} that fetches a small test file from this computer - you press Enter', t.title) : tr('Start a server first')">
                {{ fs.test().state === 'wait' ? tr('Waiting for {0}…', fs.test().device) : tr('Test from {0}', t.title) }}</button>
            }
            @if (fw()?.canFix && fw()?.app !== 'allowed' && fw()?.on === 'on') { <button type="button" class="primary" (click)="allowFw()" [disabled]="fwBusy()">{{ tr('Allow Termwright…') }}</button> }
            <button type="button" class="icon" (click)="checkFw()" [disabled]="fwBusy()" [title]="tr('Check again')" [attr.aria-label]="tr('Check again')"><app-icon name="refresh" [size]="13" /></button>
          </div>
          @if (fw(); as f) {
            <div class="fwc">
              <div class="t"><b><i [class]="f.on === 'on' ? 'ok' : f.on === 'off' ? 'mu' : 'mu'">{{ f.on === 'on' ? '✓' : '–' }}</i>{{ !f.name ? tr('No firewall found') : f.on === 'on' ? tr('Firewall on') : f.on === 'off' ? tr('Firewall off') : tr('Firewall state unknown') }}</b>
                <span>{{ f.name || tr('nothing to allow on this computer') }}</span></div>
              <div class="t"><b><i [class]="f.app === 'allowed' ? 'ok' : f.app === 'blocked' ? 'bad' : 'w'">{{ f.app === 'allowed' ? '✓' : f.app === 'blocked' ? '✕' : '!' }}</i>{{ f.app === 'allowed' ? tr('Termwright allowed') : f.app === 'blocked' ? tr('Termwright blocked') : f.app === 'ask' ? tr('Not decided yet') : tr('Not known') }}</b>
                <span>{{ f.app === 'blocked' ? tr('devices cannot reach the servers') : f.app === 'ask' ? tr('the system asks when a server starts - choose Allow') : f.app === 'allowed' ? tr('devices can reach the servers') : f.program }}</span></div>
              <div class="t"><b><i class="w">!</i>{{ tr('TFTP ports') }}</b><span>{{ tr('a new UDP port per file - allow the program, not only port {0}', s()?.tftpPort ?? 69) }}</span></div>
              <div class="t"><b><i [class]="fs.test().state === 'ok' ? 'ok' : fs.test().state === 'fail' ? 'bad' : 'mu'">{{ fs.test().state === 'ok' ? '✓' : fs.test().state === 'fail' ? '✕' : fs.test().state === 'wait' ? '…' : '–' }}</i>{{ tr('Device test') }}</b>
                <span>{{ fs.test().state === 'ok' ? tr('{0} reached it', fs.test().device) : fs.test().state === 'fail' ? tr('{0} did not reach it', fs.test().device) : fs.test().state === 'wait' ? tr('press Enter in {0}', fs.test().device) : tr('not run yet') }}</span></div>
            </div>
            @for (n of f.notes ?? []; track n) { <div class="muted small note">{{ n }}</div> }
            @if (f.fix && f.app !== 'allowed') {
              <div class="muted small note"><a href="" (click)="$event.preventDefault(); fwCmd.set(!fwCmd())">{{ fwCmd() ? tr('Hide the command') : tr('Show the command') }}</a>
                @if (!f.canFix) { - {{ tr('run it as an administrator') }} }</div>
              @if (fwCmd()) { <div class="cmdline"><code>{{ f.fix }}</code><button type="button" (click)="copyFw()">Copy</button></div> }
            }
          } @else { <div class="muted small">{{ tr('Checking the firewall…') }}</div> }
        </div>
        <div class="warn"><app-icon name="alert" [size]="14" /><span>{{ tr('TFTP, FTP and HTTP are not encrypted - files and the FTP password cross the network as they are.') }}
          {{ tr('The servers stop on their own after') }}
          <select [(ngModel)]="st.idleMinutes" name="idle" (ngModelChange)="save()" class="inl">
            <option [ngValue]="10">{{ tr('10 minutes') }}</option><option [ngValue]="30">{{ tr('30 minutes') }}</option>
            <option [ngValue]="60">{{ tr('1 hour') }}</option><option [ngValue]="240">{{ tr('4 hours') }}</option><option [ngValue]="-1">{{ tr('never') }}</option>
          </select> {{ tr('without transfers, and when Termwright closes.') }}</span></div>
        <div class="cmdbox">
          <div class="sec">Command for the device <span class="muted small">- {{ tr('for the device type and file you choose') }}</span><span class="grow"></span>
            <div class="seg mini"><button type="button" [class.on]="!put()" (click)="setPut(false)">{{ tr('Device fetches') }}</button><button type="button" [class.on]="put()" (click)="setPut(true)">{{ tr('Device sends') }}</button></div></div>
          <div class="row" style="margin:0">
            <select class="dt" [ngModel]="dtype()" (ngModelChange)="dtype.set($event)" name="dt" aria-label="Device type">
              @for (d of types(); track d) { <option [value]="d">{{ d === any ? tr(any) : d }}</option> }</select>
            <select class="pr" [ngModel]="eproto()" (ngModelChange)="proto.set($event)" name="pr" aria-label="Protocol">
              @for (p of protos(); track p) { <option [value]="p">{{ p.toUpperCase() }}</option> }</select>
            @if (put()) { <input class="grow" [ngModel]="name()" (ngModelChange)="name.set($event)" name="nm" spellcheck="false" aria-label="File name to save as"> }
            @else {
              <select class="grow" [ngModel]="file()" (ngModelChange)="file.set($event)" name="fl" aria-label="File">
                @for (f of files(); track f.name) { <option [value]="f.name">{{ f.name }}  ({{ sz(f.size) }})</option> }
                @if (!files().length) { <option value="">{{ tr('No files in the folder yet - put them there first') }}</option> }
              </select>
              <button type="button" class="icon" (click)="loadFiles()" [title]="tr('Read the folder again')"><app-icon name="refresh" [size]="14" /></button>
            }
          </div>
          @if (editing()) {
            <div class="cmdline"><input class="mono grow" [(ngModel)]="editText" name="et" spellcheck="false" aria-label="Command template">
              <button type="button" class="primary" (click)="saveTemplate()">Save</button><button type="button" (click)="editing.set(false)">Cancel</button></div>
            <div class="muted small">{{ tr('Filled in:') }} {{ '{url} {ip} {port} {file} {base} {name} {user} {password}' }}</div>
          } @else if (command(); as c) {
            <div class="cmdline"><code>{{ c.shown }}</code>
              @if (pasteTab(); as t) { <button type="button" class="primary" (click)="paste(t.id)" [title]="tr('Types it at the prompt of the open tab without Enter')">{{ tr('Paste in') }} {{ t.title }}</button> }
              <button type="button" (click)="copyCommand()">Copy</button>
              <button type="button" (click)="edit()" [title]="tr('Change the command for this device type')">Edit</button></div>
            @if (c.note) { <div class="muted small note">{{ c.note }}</div> }
          } @else {
            <div class="muted small note">{{ tr('No command for this device type and protocol yet.') }} <a href="" (click)="$event.preventDefault(); edit()">{{ tr('Add one') }}</a></div>
          }
        </div>
        <div class="sec">Transfers <span class="grow"></span><button type="button" (click)="fs.clear()" [disabled]="!fs.transfers().length">Clear</button></div>
        @if (fs.transfers().length) {
          <div class="tbl"><table>
            <tr><th>Time</th><th>Server</th><th>Device</th><th></th><th>File</th><th>Progress</th><th>Size</th></tr>
            @for (t of fs.transfers(); track t.id) {
              <tr>
                <td>{{ time(t.start) }}</td><td><span class="tag">{{ t.server }}</span></td><td>{{ t.peer }}</td>
                <td [title]="t.put ? tr('The device sent this file') : tr('The device fetched this file')">{{ t.put ? '↑' : '↓' }}</td>
                <td class="f" [title]="t.file">{{ t.file }}</td>
                @switch (t.state) {
                  @case ('running') { <td><span class="bar"><i [style.width]="pct(t)"></i></span>{{ pct(t) }}{{ rate(t) }}</td> }
                  @case ('done') { <td class="okc">✓ {{ t.put ? tr('Saved') : tr('Sent') }}</td> }
                  @case ('refused') { <td class="bad" [title]="t.error ?? ''">{{ tr(t.error ?? 'Refused') }}</td> }
                  @default { <td class="bad" [title]="t.error ?? ''">{{ tr('Failed') }}: {{ t.error }}</td> }
                }
                <td>{{ t.state === 'running' && t.size > 0 ? sz(t.bytes) + ' / ' + sz(t.size) : sz(t.state === 'done' ? (t.size >= 0 ? t.size : t.bytes) : t.bytes || -1) }}</td>
              </tr>
            }
          </table></div>
        } @else { <p class="muted small">{{ tr('Files devices fetch or send appear here.') }}</p> }
      </div>
      }
      <div class="modal-foot"><button type="button" (click)="stop('')" [disabled]="!anyRunning()">Stop All</button><span class="spacer"></span>
        <button type="button" class="primary" (click)="ui.close()">Close</button></div>
    </div>
  </div>`,
  styles: [`
    .srv { width: min(880px, 96vw); }
    .modal-body { max-height: min(720px, calc(100vh - 150px)); overflow: auto; }
    .cards { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; margin: 12px 0 14px; }
    @media (max-width: 760px) { .cards { grid-template-columns: 1fr; } }
    .card { border: 1px solid var(--line); border-radius: var(--r); padding: 10px 12px; background: var(--surface); display: flex; flex-direction: column; gap: 7px; min-width: 0; }
    .card.on { border-color: color-mix(in srgb, var(--ok) 55%, var(--line)); }
    .ct { display: flex; align-items: center; gap: 8px; font-weight: 600; }
    .grow { flex: 1; min-width: 0; }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--faint); display: inline-block; }
    .on .dot { background: var(--ok); box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 25%, transparent); }
    .muted { color: var(--muted); } .small { font-size: calc(12px * var(--ui-scale, 1)); }
    .err { color: var(--danger); }
    .kv { display: flex; align-items: center; gap: 8px; font-size: calc(12.5px * var(--ui-scale, 1)); flex-wrap: wrap; }
    .kv input { width: 72px; } .kv input.w { width: 100px; flex: 1; min-width: 70px; } .kv .k { min-width: 62px; flex: none; } .kv button.sm { padding: 2px 8px; font-size: calc(11.5px * var(--ui-scale, 1)); }
    label.ck { display: flex; gap: 6px; align-items: center; font-size: calc(12.5px * var(--ui-scale, 1)); }
    .row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
    .lab { width: 92px; flex: none; color: var(--muted); font-size: calc(12.5px * var(--ui-scale, 1)); }
    .row select { flex: 1; min-width: 0; } .row select.dt { flex: none; width: 160px; } .row select.pr { flex: none; width: 90px; }
    .path { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); padding: 5px 8px; border: 1px solid var(--line); border-radius: var(--r-sm); background: var(--input-bg); }
    .sec { font-weight: 600; margin: 4px 0 6px; display: flex; align-items: center; gap: 8px; .muted { font-weight: 400; } }
    .seg.mini button { white-space: nowrap; padding: 2px 10px; font-size: calc(11.5px * var(--ui-scale, 1)); }
    .fwp { border: 1px solid var(--line); border-radius: var(--r); padding: 10px 12px; background: var(--surface-2); margin-bottom: 12px; }
    .fwp a { color: var(--accent); }
    .fwh { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; app-icon { color: var(--accent); display: inline-flex; } button { height: 26px; } }
    .fwc { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; }
    .fwc .t { border: 1px solid var(--line); border-radius: 8px; padding: 7px 9px; background: var(--bg); font-size: calc(12px * var(--ui-scale, 1)); display: flex; flex-direction: column; gap: 2px; min-width: 0;
      b { display: flex; gap: 6px; align-items: center; font-weight: 600; } span { color: var(--muted); }
      i { font-style: normal; width: 14px; text-align: center; } .ok { color: var(--ok); } .bad { color: var(--danger); } .w { color: var(--warn); } .mu { color: var(--muted); } }
    @media (max-width: 760px) { .fwc { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
    .cmdbox { border: 1px solid var(--line); border-radius: var(--r); padding: 10px 12px; background: var(--surface-2); margin-bottom: 14px; }
    .cmdline { display: flex; align-items: center; gap: 6px; margin-top: 8px; }
    .cmdline code { flex: 1; min-width: 0; font-family: var(--mono); font-size: calc(12.5px * var(--ui-scale, 1)); padding: 6px 8px; background: var(--input-bg); border: 1px solid var(--line); border-radius: var(--r-sm); overflow-x: auto; white-space: nowrap; user-select: text; }
    .cmdline input.mono { font-family: var(--mono); }
    .fwp .cmdline { align-items: flex-start; code { white-space: pre; } }
    .note { margin-top: 6px; }
    .warn { display: flex; gap: 8px; align-items: flex-start; font-size: calc(12px * var(--ui-scale, 1)); color: var(--muted); padding: 7px 10px; border-left: 3px solid var(--warn); background: color-mix(in srgb, var(--warn) 9%, transparent); border-radius: var(--r-sm); margin-bottom: 12px; line-height: 1.6;
      app-icon { color: var(--warn); margin-top: 2px; flex: none; } select.inl { padding: 0 4px; height: auto; font-size: inherit; } }
    .tbl { max-height: 240px; overflow: auto; }
    table { width: 100%; border-collapse: collapse; font-size: calc(12px * var(--ui-scale, 1)); }
    th { text-align: left; color: var(--muted); font-weight: 500; padding: 4px 6px; border-bottom: 1px solid var(--line); position: sticky; top: 0; background: var(--panel); }
    td { padding: 5px 6px; border-bottom: 1px solid var(--line); white-space: nowrap; }
    td.f { font-family: var(--mono); max-width: 220px; overflow: hidden; text-overflow: ellipsis; }
    .bar { width: 100px; height: 6px; border-radius: 3px; background: var(--surface-2); overflow: hidden; display: inline-block; vertical-align: middle; margin-right: 6px; }
    .bar i { display: block; height: 100%; background: var(--accent); }
    .tag { font-size: calc(11px * var(--ui-scale, 1)); padding: 1px 6px; border-radius: 9px; border: 1px solid var(--line); }
    .okc { color: var(--ok); } .bad { color: var(--danger); }
  `],
})
export class FileServersDialog implements OnInit, OnDestroy {
  protected ui = inject(Ui);
  protected fs = inject(FileServers);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  protected readonly tr = tr;
  protected readonly pct = pct;
  protected readonly sz = size;
  protected readonly any = ANY;
  readonly kinds: FileServer['kind'][] = ['tftp', 'ftp', 'http'];

  readonly s = signal<FileServersSettings | null>(null);
  readonly addresses = signal<FSAddress[]>([]);
  readonly files = signal<ServedFile[]>([]);
  readonly busy = signal('');
  readonly put = signal(false);
  readonly dtype = signal(ANY);
  readonly proto = signal<Proto>('tftp');
  readonly file = signal('');
  readonly name = signal('');
  readonly editing = signal(false);
  readonly own = signal<ServerCommand[]>([]);
  private readonly password = signal('');
  editText = '';
  private tick?: ReturnType<typeof setInterval>;
  readonly now = signal(Date.now());

  /** The open terminal tab, connected (for Paste and to pick the address and device type). */
  readonly pasteTab = computed(() => { const t = this.tabs.active(); return t && t.kind === 'terminal' && t.state === 'connected' ? t : null; });
  readonly reachName = computed(() => this.tabs.active()?.title ?? '');
  readonly anyRunning = computed(() => this.fs.running().length > 0);
  readonly types = computed(() => deviceTypes(this.own(), (this.ui.settings()?.deviceTypes ?? []).map(d => d.name)));
  readonly protos = computed<Proto[]>(() => {
    const all: Proto[] = this.put() ? ['tftp', 'ftp'] : ['tftp', 'ftp', 'http'];
    const ok = all.filter(p => template(this.own(), this.dtype(), p, this.put(), this.file()));
    return ok.length ? ok : all;
  });

  readonly eproto = computed<Proto>(() => this.protos().includes(this.proto()) ? this.proto() : this.protos()[0]);
  readonly command = computed(() => {
    const s = this.s();
    if (!s) return null;
    const p = this.eproto();
    const t = template(this.own(), this.dtype(), p, this.put(), this.file());
    if (!t) return null;
    const sv = this.server(p);
    const addr = s.address && s.address !== '*' ? s.address : (this.addresses().find(a => a.reaches) ?? this.addresses()[0])?.ip ?? '127.0.0.1';
    const f: Fill = { proto: p, ip: sv?.running && sv.address ? sv.address : addr, port: sv?.running ? sv.port : p === 'tftp' ? s.tftpPort : p === 'ftp' ? s.ftpPort : s.httpPort,
      file: this.file(), name: this.name(), user: s.ftpUser, password: this.password() || 'PASSWORD' };
    if (!this.put() && !f.file) return null;
    const notes: string[] = [];
    if (!sv?.running) notes.push(tr('Start the server first - the command works only while it runs.'));
    if (needsStandardPort(t.text, f)) notes.push(tr('This command cannot name a port - the server must use the standard port.'));
    if (this.put() && !(p === 'tftp' ? s.tftpWrite : s.ftpWrite)) notes.push(tr('Turn on "Devices can upload files" for this server.'));
    if (t.own) notes.push(tr('Your own command for this device type.'));
    const text = fill(t.text, f, this.put());
    const shown = f.proto === 'ftp' && f.password ? text.split(encodeURIComponent(f.password)).join('••••••').split(f.password).join('••••••') : text;
    return { text, shown, note: notes.join(' '), template: t.text, proto: p };
  });

  async ngOnInit() {
    try {
      const v = await this.fs.refresh();
      const st = v.settings;
      this.own.set(st.commands ?? []);
      const t = this.tabs.active();
      const host = t?.session?.hostname ?? '';
      const addrs = await this.backend.fileServersAddresses(host).catch(() => [] as FSAddress[]);
      this.addresses.set(addrs);
      if (!st.address) st.address = (addrs.find(a => a.reaches) ?? addrs[0])?.ip ?? '*';
      this.s.set(st);
      const dt = t?.session?.deviceType ?? '';
      if (dt && this.types().some(x => x.toLowerCase() === dt.toLowerCase())) this.dtype.set(this.types().find(x => x.toLowerCase() === dt.toLowerCase())!);
      this.name.set(`${(t?.title || 'device').replace(/[^\w.-]+/g, '_')}_${new Date().toISOString().slice(0, 10)}.cfg`);
      await this.loadFiles();
      this.password.set(await this.backend.fileServersPassword().catch(() => ''));
      void this.checkFw();
    } catch (e) { this.ui.toast(errText(e), 'error'); }
    this.tick = setInterval(() => this.now.set(Date.now()), 1000);
  }
  ngOnDestroy() { clearInterval(this.tick); }

  server(k: string) { return this.fs.state()?.servers.find(x => x.kind === k) ?? null; }
  activeOf(k: string) { return this.fs.active().filter(t => t.server.toLowerCase() === k).length; }

  async loadFiles() {
    const f = await this.backend.fileServersFiles().catch(() => [] as ServedFile[]);
    this.files.set(f ?? []);
    if (!this.files().some(x => x.name === this.file())) this.file.set(this.files().find(x => /\.(xos|bin|tgz|img|pkg|swi|tar)$/i.test(x.name))?.name ?? this.files()[0]?.name ?? '');
  }

  async save() {
    const s = this.s();
    if (!s) return;
    try { await this.backend.fileServersSave({ ...s, commands: this.own() }); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
    void this.checkFw(); // the command to allow the ports follows them
  }

  async start(k: string) {
    this.busy.set(k);
    try { await this.save(); await this.backend.fileServersStart(k); }
    catch (e) { this.ui.toast(errText(e), 'error', 8000); }
    finally { this.busy.set(''); }
  }
  async stop(k: string) { await this.backend.fileServersStop(k); }

  async chooseFolder() {
    const d = await this.backend.fileServersChooseFolder().catch(() => '');
    const s = this.s();
    if (!d || !s) return;
    this.s.set({ ...s, folder: d });
    await this.save();
    await this.loadFiles();
  }
  async showFolder() { try { await this.backend.fileServersShowFolder(); } catch (e) { this.ui.toast(errText(e), 'error'); } }

  async copyPassword() {
    this.password.set(await this.backend.fileServersPassword());
    await this.backend.clipboardSet(this.password());
    this.ui.toast(tr('FTP password copied'), 'success');
  }
  async newPassword() {
    try { this.password.set(await this.backend.fileServersNewPassword()); this.ui.toast(tr('New FTP password made'), 'success'); }
    catch (e) { this.ui.toast(errText(e), 'error'); }
  }

  setPut(v: boolean) { this.put.set(v); this.editing.set(false); }

  readonly fw = signal<FirewallState | null>(null);
  readonly fwBusy = signal(false);
  readonly fwCmd = signal(false);
  private ports() { const s = this.s(); return [s?.tftpPort ?? 0, s?.ftpPort ?? 0, s?.httpPort ?? 0].map(p => Number(p) || 0) as [number, number, number]; }
  private fwTimer: ReturnType<typeof setTimeout> | undefined;
  /** A port is being typed: the firewall command follows it shortly. */
  fwSoon() { clearTimeout(this.fwTimer); this.fwTimer = setTimeout(() => void this.checkFw(), 400); }
  async checkFw() {
    this.fwBusy.set(true);
    try { this.fw.set(await this.backend.firewallCheck(...this.ports())); } catch { this.fw.set({ name: '', on: 'unknown', app: 'unknown', notes: [], fix: '', canFix: false, program: '' }); }
    this.fwBusy.set(false);
  }
  async allowFw() {
    const f = this.fw(); if (!f) return;
    let pw = '';
    if (f.sudo) { // the sudo password here, or none for the system's own prompt
      const v = await this.ui.input(tr('Allow Termwright'), tr('Let devices reach Termwright through {0}? It runs:', f.name) + '\n\n' + f.fix + '\n\n' + tr('Your sudo password (leave it empty for the system’s own password prompt):'), '', true);
      if (v === null) return;
      pw = v;
    } else if (!(await this.ui.confirm(tr('Allow Termwright'), tr('Let devices reach Termwright through {0}? The system asks for an administrator password and runs:', f.name) + '\n\n' + f.fix, tr('Allow')))) return;
    this.fwBusy.set(true);
    try { this.fw.set(await this.backend.firewallAllow(...this.ports(), pw)); this.ui.toast(tr('The firewall lets devices reach Termwright now'), 'success'); }
    catch (e) { this.ui.toast(errText(e), 'error', 8000); }
    this.fwBusy.set(false);
  }
  async copyFw() { await this.backend.clipboardSet(this.fw()?.fix ?? ''); this.ui.toast(tr('Copied'), 'success'); }
  /** Types a command in the tab that fetches a small test file; the transfer list tells whether it arrived. */
  async testFrom(id: string, title: string) {
    try {
      const name = await this.backend.fileServersTestFile();
      await this.loadFiles();
      const running = (['tftp', 'http', 'ftp'] as Proto[]).filter(p => this.server(p)?.running && template(this.own(), this.dtype(), p, false, name));
      if (!running.length) { this.ui.toast(tr('No command to fetch a file for this device type with the running servers - choose the device type or start TFTP or HTTP'), 'error', 8000); return; }
      this.put.set(false); this.file.set(name); this.proto.set(running[0]);
      const c = this.command();
      if (!c) return;
      void this.tabs.paste(id, c.text);
      this.fs.startTest(title, running[0]);
      this.ui.close();
      this.tabs.focus(id);
      this.ui.toast(tr('Press Enter in {0}: it fetches termwright-test.txt (a few bytes) - you can delete it on the device afterwards', title), 'info', 9000);
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }

  paste(id: string) {
    const c = this.command();
    if (!c) return;
    void this.tabs.paste(id, c.text);
    this.ui.close();
    this.tabs.focus(id);
    this.ui.toast(tr('Pasted - check it, then press Enter in the terminal'), 'info');
  }
  async copyCommand() {
    const c = this.command();
    if (!c) return;
    await this.backend.clipboardSet(c.text);
    this.ui.toast(tr('Copied'), 'success');
  }

  edit() {
    const p = this.eproto();
    this.editText = template(this.own(), this.dtype(), p, this.put(), this.file())?.text ?? '{url}';
    this.editing.set(true);
  }
  async saveTemplate() {
    const p = this.eproto();
    const text = this.editText.trim();
    const keep = this.own().filter(c => !(c.deviceType.toLowerCase() === this.dtype().toLowerCase() && c.protocol === p && !!c.put === this.put()));
    this.own.set(text ? [...keep, { deviceType: this.dtype(), protocol: p, put: this.put(), text }] : keep);
    this.editing.set(false);
    await this.save();
    this.ui.toast(tr(text ? 'Command saved for this device type' : 'Command removed'), 'success');
  }

  time(iso: string) { const d = new Date(iso); return isNaN(+d) ? '' : d.toTimeString().slice(0, 5); }
  rate(t: { bytes: number; start: string }) {
    const secs = (this.now() - +new Date(t.start)) / 1000;
    return secs > 1 ? ' · ' + size(Math.round(t.bytes / secs)) + '/s' : '';
  }
}
