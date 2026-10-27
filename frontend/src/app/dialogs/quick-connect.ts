import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Autofocus } from '../core/autofocus';
import { Backend, errText } from '../core/backend';
import { BAUD_RATES, PROTOCOLS, defaultPort } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { SerialPortPicker } from '../components/serial-port-picker';

@Component({
  selector: 'app-quick-connect',
  imports: [FormsModule, Autofocus, SerialPortPicker],
  template: `
  <div class="modal-backdrop">
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
  </div>`,
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
