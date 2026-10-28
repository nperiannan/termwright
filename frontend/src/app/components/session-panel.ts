import { Component, HostListener, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { BAUD_RATES, PROTOCOLS, Session, defaultPort } from '../core/models';
import { ENVIRONMENTS } from '../core/env';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Icon } from './icon';
import { SerialPortPicker } from './serial-port-picker';
import { storeSession } from '../dialogs/session-editor';

type Edit = { session: Session; oldPath: string; tabId?: string };

/** Session options in a side panel (modern layout): the fields used every
 *  day, the rest one click away in the full Session Options dialog. */
@Component({
  selector: 'app-session-panel',
  imports: [FormsModule, Icon, SerialPortPicker],
  template: `
    @if (s) {
    <form class="sp" (ngSubmit)="save(false)">
      <header>
        <div class="t">
          <div class="h">{{ edit().oldPath ? 'Session options' : 'New session' }}</div>
          <div class="p">{{ edit().oldPath || (s.folder ? 'in ' + s.folder : 'top level') }}</div>
        </div>
        <button type="button" class="ic" title="Close (Esc)" (click)="close()"><app-icon name="x" /></button>
      </header>
      <div class="body">
        <label class="f"><span>Name</span><input name="name" [(ngModel)]="s.name" placeholder="e.g. core-sw1" /></label>
        <div class="f"><span>Protocol</span>
          <div class="seg">
            @for (p of main; track p) { <button type="button" [class.on]="s.protocol === p" (click)="setProto(p)">{{ p === 'SSH2' ? 'SSH' : p }}</button> }
            <select name="other" [class.on]="!main.includes(s.protocol)" [ngModel]="main.includes(s.protocol) ? '' : s.protocol" (ngModelChange)="$event && setProto($event)">
              <option value="">More…</option>
              @for (p of others; track p) { <option [value]="p">{{ p }}</option> }
            </select>
          </div>
        </div>
        @switch (s.protocol) {
          @case ('Serial') {
            <div class="f"><span>Serial port</span><app-serial-port-picker [(value)]="s.comPort" /></div>
            <label class="f"><span>Speed</span>
              <select name="baud" [ngModel]="s.baudRate" (ngModelChange)="s.baudRate = +$event">@for (b of bauds; track b) { <option [ngValue]="b">{{ b }}</option> }</select></label>
          }
          @case ('Local Shell') { <p class="note">The shell and folder are set in More options.</p> }
          @case ('Console') { <p class="note">The console server and port are set in More options.</p> }
          @default {
            <div class="two">
              <label class="f grow"><span>Address</span><input name="host" [(ngModel)]="s.hostname" placeholder="10.1.1.1 or host.example.com" spellcheck="false" /></label>
              <label class="f port"><span>Port</span><input name="port" type="number" min="1" max="65535" [(ngModel)]="s.port" /></label>
            </div>
          }
        }
        @if (s.protocol !== 'RDP' && s.protocol !== 'VNC' && s.protocol !== 'Local Shell') {
          <label class="f"><span>Device type</span>
            <select name="dt" [(ngModel)]="s.deviceType">
              <option value="">(none)</option>
              @for (d of deviceTypes(); track d) { <option [value]="d">{{ d }}</option> }
            </select></label>
        }
        @if (!hasLogin() && s.deviceType && deviceUser()) {
          <div class="note">Logs in as <b>{{ deviceUser() }}</b> with the {{ s.deviceType }} device type's login.
            <button type="button" class="link" (click)="s.ownLogin = true">Use its own login</button></div>
        }
        @if (hasLogin()) {
          <label class="f"><span>Username</span><input name="user" [(ngModel)]="s.username" [placeholder]="deviceUser() || 'asked when connecting'" spellcheck="false" autocomplete="off" /></label>
          <div class="f"><span>Password</span>
            @if (pwOpen()) {
              <input name="pw" type="password" [(ngModel)]="pw" placeholder="saved in the system keychain" autocomplete="new-password" />
            } @else {
              <button type="button" class="chip" (click)="pwOpen.set(true)" title="Change the saved password">
                <app-icon name="key" [size]="14" />{{ pwText() }}<span class="chg">Change</span></button>
            }
          </div>
        }
        <label class="f"><span>Environment</span>
          <select name="env" [ngModel]="s.environment || ''" (ngModelChange)="s.environment = $event">
            <option value="">(none)</option>
            @for (v of envs; track v.id) { <option [value]="v.id">{{ v.label }}</option> }
          </select></label>

        <div class="adv">
          <div class="advh">Advanced</div>
          @for (a of advanced(); track a.page) {
            <button type="button" class="arow" (click)="more(a.page)"><span>{{ a.label }}</span><span class="v">{{ a.value }}</span><span class="chev">›</span></button>
          }
        </div>
        @if (error()) { <div class="err">{{ error() }}</div> }
      </div>
      <footer>
        <button type="button" class="link" (click)="more('')">More options…</button>
        <span class="grow"></span>
        <button type="submit">Save</button>
        <button type="button" class="primary" (click)="save(true)">Save &amp; Connect</button>
      </footer>
    </form>
    }
  `,
  styles: [`
    :host { flex: none; width: 340px; display: flex; min-height: 0; border-left: 1px solid var(--line); background: var(--surface); }
    .sp { flex: 1; display: flex; flex-direction: column; min-width: 0; }
    header { display: flex; align-items: flex-start; gap: 8px; padding: 14px 14px 10px 18px;
      .t { flex: 1; min-width: 0; } .h { font-weight: 600; font-size: calc(15px * var(--ui-scale, 1)); }
      .p { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } }
    .ic { width: 28px; height: 28px; padding: 0; display: inline-flex; align-items: center; justify-content: center; border: 0; border-radius: var(--r); background: transparent; color: var(--muted);
      &:hover { background: var(--hover); color: var(--text); } }
    .body { flex: 1; overflow: auto; padding: 4px 18px 16px; display: flex; flex-direction: column; gap: 12px; }
    .f { display: flex; flex-direction: column; gap: 5px; min-width: 0;
      > span { font-size: calc(12px * var(--ui-scale, 1)); color: var(--muted); font-weight: 500; }
      input, select { height: 32px; border-radius: var(--r); } }
    .two { display: flex; gap: 8px; .grow { flex: 1; } .port { width: 84px; flex: none; } }
    .seg { display: flex; gap: 2px; padding: 2px; border: 1px solid var(--line); border-radius: var(--r); background: var(--surface-2);
      button, select { flex: 1; min-width: 0; height: 28px; border: 0; border-radius: var(--r-sm); background: transparent; color: var(--muted); padding: 0 6px; font-weight: 500; }
      select { min-height: 0; cursor: pointer; }
      button:hover:not(.on) { color: var(--text); background: transparent; }
      .on { background: var(--surface); color: var(--text); box-shadow: 0 1px 2px rgba(0, 0, 0, .18), 0 0 0 1px var(--line); } }
    .chip { display: flex; align-items: center; gap: 8px; height: 32px; padding: 0 10px; border: 1px solid var(--line); border-radius: var(--r);
      background: var(--surface-2); color: var(--text); text-align: left;
      app-icon { color: var(--ok); } .chg { margin-left: auto; color: var(--accent); font-size: calc(12px * var(--ui-scale, 1)); } }
    .note { margin: 0; color: var(--muted); line-height: 1.5; b { color: var(--text); font-weight: 600; } }
    .link { border: 0; background: transparent; padding: 0; color: var(--accent); height: auto; &:hover { background: transparent; text-decoration: underline; } }
    .adv { margin-top: 4px; border: 1px solid var(--line); border-radius: var(--r-lg); overflow: hidden; }
    .advh { padding: 8px 12px; font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .06em; text-transform: uppercase; color: var(--faint); background: var(--surface-2); }
    .arow { width: 100%; height: 36px; display: flex; align-items: center; gap: 8px; padding: 0 12px; border: 0; border-top: 1px solid var(--line); border-radius: 0;
      background: transparent; text-align: left;
      &:hover { background: var(--hover); }
      .v { margin-left: auto; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 55%; }
      .chev { color: var(--faint); } }
    .err { color: var(--danger); }
    footer { display: flex; align-items: center; gap: 8px; padding: 12px 14px 12px 18px; border-top: 1px solid var(--line);
      .grow { flex: 1; } .link { border: 0; background: transparent; padding: 0; color: var(--accent); height: auto; &:hover { background: transparent; text-decoration: underline; } } }
  `],
})
export class SessionPanel {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  readonly edit = input.required<Edit>();

  readonly main = ['SSH2', 'Telnet', 'Serial'];
  readonly others = PROTOCOLS.filter(p => !['SSH2', 'Telnet', 'Serial'].includes(p));
  readonly bauds = BAUD_RATES;
  readonly envs = ENVIRONMENTS;
  readonly deviceTypes = computed(() => (this.ui.settings()?.deviceTypes ?? []).map(d => d.name));
  readonly error = signal('');
  readonly pwOpen = signal(false);
  private readonly pwState = signal('');
  s: Session | null = null;
  pw = '';

  constructor() {
    effect(() => {
      const e = this.edit();
      this.s = structuredClone(e.session);
      this.pw = '';
      this.error.set('');
      this.pwState.set('');
      this.pwOpen.set(!e.oldPath);
      if (e.oldPath) {
        this.backend.sessionPasswordState(e.oldPath).then(st => {
          this.pwState.set(st);
          this.pwOpen.set(st !== 'set');
        }).catch(() => this.pwOpen.set(true));
      }
    });
  }

  hasLogin() { return !!this.s && !['Serial', 'Local Shell', 'VNC'].includes(this.s.protocol) && (!this.s.deviceType || this.s.ownLogin || !this.deviceUser()); }
  deviceUser() { return (this.ui.settings()?.deviceTypes ?? []).find(d => d.name === this.s?.deviceType)?.username ?? ''; }
  pwText() { return this.pwState() === 'set' ? 'Saved in the system keychain' : 'Asked when connecting'; }

  setProto(p: string) {
    if (!this.s) return;
    this.s.protocol = p;
    this.s.port = defaultPort(p) || this.s.port;
    if (p === 'Console' && !this.s.consoleMethod) { this.s.consoleMethod = 'opengear-ssh'; this.s.initialCr = true; }
  }

  /** The settings kept in the full dialog, with what they are set to now. */
  readonly advanced = () => {
    const s = this.s;
    if (!s) return [];
    const out: { page: string; label: string; value: string }[] = [];
    if (s.protocol === 'SSH2') out.push({ page: 'auth', label: 'Authentication', value: s.identityFile ? 'key file' : s.agentForward ? 'agent' : 'password / key' });
    if (['SSH2', 'Telnet', 'Raw'].includes(s.protocol)) out.push({ page: 'firewall', label: 'Jump host / proxy', value: s.jumpSession || (s.proxyHost ? s.proxyHost : 'none') });
    if (s.protocol === 'SSH2') out.push({ page: 'forward', label: 'Port forwarding', value: (s.portForwards?.length ?? 0) ? String(s.portForwards.length) : 'none' });
    if (s.protocol === 'Serial') out.push({ page: 'serial', label: 'Serial settings', value: `${s.dataBits || 8}-${['N', 'O', 'E', 'M', 'S'][s.parity] ?? 'N'}-${s.stopBits || 1}` });
    if (!['RDP', 'VNC'].includes(s.protocol)) {
      out.push({ page: 'logon', label: 'Logon actions & triggers', value: String((s.logonActions?.length ?? 0) + (s.triggers?.length ?? 0) || 'none') });
      out.push({ page: 'terminal', label: 'Terminal', value: [s.emulation, s.colorScheme].filter(Boolean).join(' · ') || 'default' });
      out.push({ page: 'log', label: 'Log file', value: s.logFile ? 'on' : 'off' });
    }
    return out;
  };

  more(page: string) {
    if (!this.s) return;
    const e = this.edit();
    this.ui.openSessionDialog(structuredClone(this.s), e.oldPath, e.tabId, page, this.pw);
  }

  close() { this.ui.panel.set(null); }

  @HostListener('keydown.escape') esc() { this.close(); }

  async save(connect: boolean) {
    const s = this.s;
    if (!s) return;
    this.error.set('');
    if (!s.name.trim()) { this.error.set('Please enter a session name.'); return; }
    if (['SSH2', 'Telnet', 'Raw', 'RDP', 'VNC'].includes(s.protocol) && !s.hostname.trim()) { this.error.set('Please enter an address.'); return; }
    if (s.protocol === 'Serial' && !s.comPort.trim()) { this.error.set('Please choose a serial port.'); return; }
    if (s.protocol === 'Console' && (!s.consoleServer || !s.consolePort)) { this.more('connection'); return; }
    s.port = Number(s.port) || 0;
    try {
      const e = this.edit();
      const saved = await storeSession(this.ui, this.backend, this.tabs, s, e.oldPath, e.tabId,
        { pw: this.pw, noPw: this.pw === '' && this.pwState() === 'empty', wasEmpty: this.pwState() === 'empty' });
      this.close();
      if (connect) await this.tabs.openSession(saved.path);
      else this.ui.toast(`Saved ${saved.name}`, 'success');
    } catch (err) {
      this.error.set(errText(err));
    }
  }
}
