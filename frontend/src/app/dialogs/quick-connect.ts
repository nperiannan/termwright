import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Autofocus } from '../core/autofocus';
import { Backend, errText } from '../core/backend';
import { BAUD_RATES, PROTOCOLS, defaultPort } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { SerialPortPicker } from '../components/serial-port-picker';
import { Icon } from '../components/icon';
import { parseConnectLine } from '../core/connect-string';

@Component({
  selector: 'app-quick-connect',
  imports: [FormsModule, Autofocus, SerialPortPicker, Icon],
  template: `
  <div class="modal-backdrop">
    @if (modern()) {
    <form class="modal qc" (ngSubmit)="connect()" autocomplete="off">
      <div class="modal-head"><span class="mh-ic"><app-icon name="quick" [size]="18" /></span>
        <span class="mh-t">Quick Connect<span class="mh-s">Connect without saving - or save it as a session</span></span></div>
      <div class="modal-body">
        <label class="line"><input name="line" class="mono" [ngModel]="line" (ngModelChange)="fromLine($event)" appAutofocus spellcheck="false"
            placeholder="voss admin@10.10.0.7 · ssh user@host:2222 · telnet host 23 · serial COM3 115200" /><span>↵ Connect</span></label>
        <div class="hint lh">Type a connect line, or fill in the fields - they follow each other.</div>
        <div class="f"><span>Protocol</span>
          <div class="seg">
            @for (p of mainProtos; track p) { <button type="button" [class.on]="protocol === p" (click)="protocol = p; onProto()">{{ p === 'SSH2' ? 'SSH' : p }}</button> }
            <select name="otherp" [ngModel]="mainProtos.includes(protocol) ? '' : protocol" (ngModelChange)="$event && (protocol = $event) && onProto()">
              <option value="">More…</option>@for (p of otherProtos; track p) { <option [value]="p">{{ p }}</option> }</select>
          </div></div>
        @if (protocol === 'Serial') {
          <div class="f"><span>Serial port</span><app-serial-port-picker [(value)]="comPort" /></div>
          <div class="f"><span>Speed</span>
            <div class="row"><select name="baud2" [(ngModel)]="baud" [disabled]="autoBaud">@for (b of bauds; track b) { <option [ngValue]="b">{{ b }}</option> }</select>
              <button type="button" (click)="detectBaud()" [disabled]="!comPort || detecting()">{{ detecting() ? 'Detecting…' : 'Detect Speed' }}</button></div>
            @if (baudMsg()) { <div class="hint">{{ baudMsg() }}</div> }</div>
        } @else if (protocol !== 'Local Shell') {
          <div class="two"><div class="f grow"><span>Address</span><input name="host2" class="mono" [(ngModel)]="host" placeholder="10.1.1.1 or host.example.com" /></div>
            <div class="f port"><span>Port</span><input name="port2" type="number" class="mono" [(ngModel)]="port" /></div></div>
        }
        @if (protocol !== 'RDP' && protocol !== 'VNC' && protocol !== 'Local Shell') {
          <div class="f"><span>Device type</span>
            <div class="chipset">
              @for (d of ui.settings()?.deviceTypes ?? []; track d.name) { <button type="button" [class.on]="device === d.name" (click)="device = d.name; onDevice()">{{ d.name }}</button> }
              <button type="button" [class.on]="!device" (click)="device = ''; onDevice()">None</button>
            </div></div>
        }
        @if (protocol !== 'Raw' && protocol !== 'Local Shell') {
          <div class="two">
            <div class="f grow"><span>Username</span>
              @if (device && inherit) { <div class="fake"><span>{{ devUser() || 'asked when connecting' }}</span><em>from {{ device }}</em><button type="button" class="link" (click)="inherit = false">change</button></div> }
              @else { <input name="user2" [(ngModel)]="user" placeholder="asked when connecting" /> }</div>
            @if (!device || !inherit) {
              <div class="f grow"><span>Password</span><input name="pw2" type="password" autocomplete="new-password" [(ngModel)]="pw" [disabled]="noPw" placeholder="asked when connecting" /></div>
            }
          </div>
        }
        <div class="checks savebox">
          <label><input type="checkbox" name="save2" [(ngModel)]="save" />
            <span class="tx"><span>Save as a session</span><span class="hint">{{ save ? (name || host || comPort || 'new session') + ' · in ' + (ui.currentFolder() || 'the top level') : 'connect once, without saving' }}</span></span></label>
        </div>
        @if (save) { <div class="f"><span>Session name</span><input name="sname2" [(ngModel)]="name" [placeholder]="host || comPort" /></div> }
      </div>
      <div class="modal-foot">
        <button type="button" class="link" (click)="moreOptions()">More options…</button>
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="submit" class="primary"><app-icon name="quick" [size]="14" /> Connect</button>
      </div>
    </form>
    } @else {
    <form class="modal" (ngSubmit)="connect()" autocomplete="off">
      <div class="modal-head">Quick Connect</div>
      <div class="modal-body">
        <div class="grid">
          <label>Device type</label>
          <select name="dev" [(ngModel)]="device" (ngModelChange)="onDevice()">
            <option value="">(none)</option>
            @for (d of ui.settings()?.deviceTypes ?? []; track d.name) { <option [value]="d.name">{{ d.name }}</option> }
          </select>
          <label>Protocol</label>
          <select name="protocol" [(ngModel)]="protocol" (ngModelChange)="onProto()">
            @for (p of protocols; track p) { <option [value]="p">{{ p }}</option> }
          </select>
          @if (protocol === 'Serial') {
            <label>Port</label>
            <app-serial-port-picker [(value)]="comPort" />
            <label>Baud rate</label>
            <div>
              <div class="row">
                <select name="baud" [(ngModel)]="baud" [disabled]="autoBaud">@for (b of bauds; track b) { <option [ngValue]="b">{{ b }}</option> }</select>
                <button type="button" (click)="detectBaud()" [disabled]="!comPort || detecting()">{{ detecting() ? 'Detecting…' : 'Detect Speed' }}</button>
              </div>
              <div class="checks" style="margin-top: 6px"><label><input type="checkbox" name="autobaud" [(ngModel)]="autoBaud" /> Detect the speed when connecting</label></div>
              @if (baudMsg()) { <div class="hint">{{ baudMsg() }}</div> }
            </div>
          } @else if (protocol !== 'Local Shell') {
            <label>Hostname</label><input name="host" [(ngModel)]="host" required appAutofocus placeholder="host, user@host or host:port" />
            <label>Port</label><input name="port" type="number" class="short" [(ngModel)]="port" />
          }
          @if (device && protocol !== 'Raw') {
            <label>Login</label>
            <div class="checks">
              <label><input type="checkbox" name="inherit" [(ngModel)]="inherit" />
                Use the device type's username and password{{ loginNote(devType()?.name, devUser()) }}</label>
            </div>
          }
          @if (protocol !== 'Raw' && protocol !== 'Local Shell' && (!device || !inherit)) {
            <label>Username</label><input name="user" [(ngModel)]="user" placeholder="(prompt)" />
            @if (save) {
              <label>Password</label>
              <div class="row">
                <input name="pw" type="password" autocomplete="new-password" [(ngModel)]="pw" [disabled]="noPw" placeholder="(asked when connecting)" />
                <label><input type="checkbox" name="nopw" [(ngModel)]="noPw" (ngModelChange)="$event && (pw = '')" /> No password</label>
              </div>
            }
          }
          <label></label>
          <div class="checks">
            <label><input type="checkbox" name="save" [(ngModel)]="save" /> Save session</label>
          </div>
          @if (save) { <label>Session name</label><input name="sname" [(ngModel)]="name" [placeholder]="host || comPort" /> }
        </div>
      </div>
      <div class="modal-foot">
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="submit" class="primary">Connect</button>
      </div>
    </form>
    }
  </div>`,
  styles: [`
    .qc { width: 500px; }
    .qc .modal-body { display: flex; flex-direction: column; gap: 12px; }
    .line { display: flex; align-items: center; gap: 8px; height: 44px; padding: 0 12px; border: 1px solid var(--accent); border-radius: var(--r-lg);
      background: var(--input-bg); box-shadow: 0 0 0 3px var(--accent-soft);
      input { flex: 1; min-width: 0; border: 0; background: transparent; height: 100%; padding: 0; font-size: calc(14px * var(--ui-scale, 1)); &:focus { border: 0; } }
      span { color: var(--faint); font-size: calc(12px * var(--ui-scale, 1)); white-space: nowrap; } }
    .lh { margin-top: -6px; }
    .f { display: flex; flex-direction: column; gap: 5px; min-width: 0; > span { font-size: calc(12px * var(--ui-scale, 1)); color: var(--muted); font-weight: 500; }
      input, select { height: 32px; } }
    .two { display: flex; gap: 10px; .grow { flex: 1; } .port { width: 90px; flex: none; } }
    .fake { height: 32px; display: flex; align-items: center; gap: 8px; padding: 0 10px; border: 1px solid var(--border); border-radius: var(--r-sm); background: var(--input-bg);
      span { flex: 1; } em { font-style: normal; color: var(--faint); font-size: calc(11px * var(--ui-scale, 1)); } }
    .link { border: 0 !important; background: transparent !important; padding: 0; height: auto; color: var(--accent); &:hover { text-decoration: underline; } }
    .savebox { border-top: 1px solid var(--line); padding-top: 6px; .tx { display: flex; flex-direction: column; } }
    .mono { font-family: var(--mono); }
  `],
})
export class QuickConnect {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  readonly protocols = PROTOCOLS;
  readonly bauds = BAUD_RATES;
  readonly detecting = signal(false);
  readonly baudMsg = signal('');
  autoBaud = false;
  protocol = 'SSH2';
  host = '';
  port = 22;
  user = '';
  comPort = '';
  baud = 9600;
  save = false;
  name = '';

  device = '';
  inherit = true;
  pw = '';
  noPw = false;

  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  readonly mainProtos = ['SSH2', 'Telnet', 'Serial', 'RDP'];
  readonly otherProtos = PROTOCOLS.filter(p => !['SSH2', 'Telnet', 'Serial', 'RDP'].includes(p));
  line = '';

  /** The connect line fills the fields. */
  fromLine(text: string) {
    this.line = text;
    const p = parseConnectLine(text, this.ui);
    if (!p) return;
    this.protocol = p.protocol;
    this.port = p.port;
    if (p.deviceType !== this.device) { this.device = p.deviceType; this.inherit = true; }
    if (p.protocol === 'Serial') { if (p.comPort) this.comPort = p.comPort; this.baud = p.baud; return; }
    this.host = p.host;
    const typed = /@/.test(text) || /\s-l\s/.test(text);
    if (typed) { this.user = p.user; if (this.device) this.inherit = false; }
  }

  /** The full Session Options dialog with what was filled in. */
  async moreOptions() {
    const s = await this.backend.newSessionDefaults();
    Object.assign(s, { protocol: this.protocol, hostname: this.host.trim(), port: Number(this.port) || defaultPort(this.protocol), username: this.user,
      comPort: this.comPort, baudRate: this.baud, deviceType: this.device, ownLogin: !!this.device && !this.inherit, name: this.name || this.host.trim(), folder: this.ui.currentFolder() });
    this.ui.openSessionDialog(s, '', undefined, '', this.pw);
  }

  private devType() { return (this.ui.settings()?.deviceTypes ?? []).find(x => x.name === this.device); }

  /** Saved-password state of each device type: "set", "empty", "" (asked), looked up once per dialog. */
  readonly pwStates = signal<Record<string, string>>({});
  /** " (admin, no password)" etc. for the "Use the device type's username and password" box. */
  loginNote(name: string | undefined, user: string): string {
    if (!name) return '';
    const st = this.pwStates()[name];
    if (st === undefined) {
      this.pwStates.update(m => ({ ...m, [name]: '?' }));
      this.backend.deviceTypePasswordState(name).then(v => this.pwStates.update(m => ({ ...m, [name]: v }))).catch(() => {});
    }
    const pw = st === 'set' ? 'password saved' : st === 'empty' ? 'no password' : st === '' ? 'password asked when connecting' : '';
    const parts = [user, pw].filter(Boolean);
    return parts.length ? ' (' + parts.join(', ') + ')' : '';
  }

  devUser() { return this.devType()?.username ?? ''; }

  onDevice() {
    const d = this.devType();
    this.inherit = true;
    if (!d) return;
    if (d.protocol) { this.protocol = d.protocol; this.onProto(); }
    if (d.port) this.port = d.port;
  }

  onProto() {
    this.port = defaultPort(this.protocol) || this.port;
  }

  async detectBaud() {
    if (!this.comPort) return;
    this.detecting.set(true); this.baudMsg.set('');
    try {
      const s = await this.backend.newSessionDefaults();
      Object.assign(s, { protocol: 'Serial', comPort: this.comPort, baudRate: this.baud });
      const r = await this.backend.detectSerialBaud(s);
      if (r.baud) { this.baud = r.baud; this.baudMsg.set(`The device answers at ${r.baud} baud.`); }
      else this.baudMsg.set('No readable answer at ' + r.tried.map(t => t.baud).join(', ') + ' baud. Check the cable and that the port is not open in a tab.');
    } catch (e) { this.baudMsg.set(errText(e)); }
    finally { this.detecting.set(false); }
  }

  async connect() {
    let host = this.host.trim();
    const own = !this.device || !this.inherit;
    let user = own ? this.user.trim() : '';
    let port = Number(this.port) || defaultPort(this.protocol);
    const at = host.lastIndexOf('@');
    if (at > 0) { if (own || !user) user ||= host.slice(0, at); host = host.slice(at + 1); }
    const m = /^([^:\[\]]+):(\d+)$/.exec(host);
    if (m) { host = m[1]; port = Number(m[2]); }
    try {
      const s = await this.backend.newSessionDefaults();
      Object.assign(s, {
        protocol: this.protocol, hostname: host, port, username: user, comPort: this.comPort, baudRate: this.baud, serialAutoBaud: this.autoBaud,
        logonActions: [], description: [], deviceType: this.device,
        ownLogin: !!this.device && (!this.inherit || at > 0),
      });
      const setPw = own && (this.pw !== '' || this.noPw);
      if (this.save) {
        s.name = (this.name || host || this.comPort || 'Session').trim();
        s.folder = this.ui.currentFolder();
        if (setPw) s.savePassword = true;
        const saved = await this.backend.saveSession(s, '');
        if (setPw) await this.backend.setSessionPassword(saved.path, this.noPw ? '' : this.pw, this.noPw);
        await this.ui.refreshTree();
        this.ui.close();
        await this.tabs.openSession(saved.path);
      } else {
        s.name = '';
        this.ui.close();
        await this.tabs.openAdHoc(s);
      }
    } catch (e) { this.ui.error(e); }
  }
}
