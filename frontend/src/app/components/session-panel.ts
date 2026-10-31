import { filesPanelFor } from '../core/device-kind';
import { Component, HostListener, computed, effect, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { BAUD_RATES, PROTOCOLS, Session, SessionNode, defaultPort } from '../core/models';
import { accountChoice, defaultLogin, defaultText, setAccountChoice, userOf } from '../core/accounts';
import { envList } from '../core/env';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Icon } from './icon';
import { SerialPortPicker } from './serial-port-picker';
import { DeviceIcon, deviceKindOf } from './device-icon';
import { FolderPicker } from './folder-picker';
import { factList, factsAge } from '../core/facts';
import { SessionIcon, SessionKind } from './session-icon';
import { storeSession } from '../dialogs/session-editor';

type Edit = { session: Session; oldPath: string; tabId?: string };

/** Session options in a side panel (modern layout): the fields used every
 *  day, the rest one click away in the full Session Options dialog. */
@Component({
  selector: 'app-session-panel',
  imports: [FormsModule, Icon, SerialPortPicker, DeviceIcon, SessionIcon, FolderPicker],
  template: `
    @if (s) {
    <form class="sp" (ngSubmit)="save(false)">
      <header>
        @if (devKind(); as k) { <app-device-icon [kind]="k" [size]="34" /> } @else { <app-session-icon [kind]="protoKind()" [size]="34" /> }
        <div class="t">
          <div class="h">{{ s.name || (edit().oldPath ? 'Session options' : 'New session') }}</div>
          <div class="p">{{ s.folder ? s.folder.split('/').join(' / ') : 'Top level' }}</div>
        </div>
        <button type="button" class="ic" title="Close (Esc)" (click)="close()"><app-icon name="x" /></button>
      </header>
      <div class="body">
        <label class="f"><span>Name</span><input name="name" [(ngModel)]="s.name" placeholder="e.g. core-sw1" /></label>
        <div class="f"><span>Folder</span><app-folder-picker [(value)]="s.folder" /></div>
        <div class="f"><span>Protocol</span>
          <div class="seg">
            @for (p of main; track p) { <button type="button" [class.on]="s.protocol === p" (click)="setProto(p)">{{ p === 'SSH2' ? 'SSH' : p }}</button> }
            <select name="other" [class.on]="!main.includes(s.protocol)" [ngModel]="main.includes(s.protocol) ? '' : s.protocol" (ngModelChange)="$event && setProto($event)">
              <option value="">More…</option>
              @for (p of others; track p) { <option [value]="p">{{ p === 'WOL' ? 'Wake-on-LAN' : p }}</option> }
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
          @case ('WOL') {
            <div class="two">
              <label class="f grow"><span>MAC address</span><input name="mac" class="mono" [(ngModel)]="s.mac" placeholder="00:11:22:33:44:55" spellcheck="false" /></label>
              <label class="f port"><span>UDP port</span><input name="wolp" type="number" min="1" max="65535" [ngModel]="s.wolPort || 9" (ngModelChange)="s.wolPort = +$event === 9 ? 0 : +$event" /></label>
            </div>
            <div class="two">
              <label class="f grow"><span>Network</span><input name="wolnet" class="mono" [(ngModel)]="s.wolNetwork" placeholder="192.168.1.0/24" spellcheck="false" /></label>
              <label class="f grow"><span>IP address <small>(or network)</small></span><input name="host" class="mono" [ngModel]="s.hostname" (ngModelChange)="wolIp($event)" placeholder="found by its MAC" spellcheck="false" /></label>
            </div>
            <p class="note">Double-click sends the magic packet to the broadcast address of its network only (from the network, or the IP address's network). With an IP address the device is pinged until it answers, and every {{ pingMins(s) }} afterwards (green = up, orange = down); without one the network is searched for the MAC address and the address found is saved. More options: searches, interval, ping, jump host.</p>
          }
          @default {
            <div class="two">
              <label class="f grow"><span>Address</span><input name="host" [(ngModel)]="s.hostname" placeholder="10.1.1.1 or host.example.com" spellcheck="false" /></label>
              <label class="f port"><span>Port</span><input name="port" type="number" min="1" max="65535" [(ngModel)]="s.port" /></label>
            </div>
          }
        }
        @if (s.protocol === 'SSH2') {
          <label class="f"><span>Open as</span>
            <select name="openAs" [(ngModel)]="s.openAs">
              <option value="">Terminal</option><option value="sftp">SFTP file browser only</option>
            </select></label>
        }
        @if (s.protocol !== 'RDP' && s.protocol !== 'VNC' && s.protocol !== 'Local Shell') {
          @if (s.protocol !== 'WOL') {
          <label class="f"><span>Device type</span>
            <select name="dt" [ngModel]="s.deviceType" (ngModelChange)="s.deviceType = $event; onDeviceType()">
              <option value="">(none)</option>
              @for (d of deviceTypes(); track d) { <option [value]="d">{{ d }}</option> }
            </select></label>
          }
        }
        @if (canLogin()) {
          <label class="f"><span>Account</span>
            <select name="acct" [ngModel]="acctChoice()" (ngModelChange)="setAcct($event)">
              <option value="">Default · {{ defText() }}</option>
              @for (a of accounts(); track a.name) { <option [value]="a.name">{{ a.name }}{{ a.username ? ' · ' + a.username : '' }}</option> }
              <option value="-">Own username and password</option>
              <option value="+">+ New Account…</option>
            </select></label>
          @if (!hasLogin()) {
            <div class="note">@if (acctUser(); as au) { Logs in as <b>{{ au.user || '(asked)' }}</b> {{ au.how }} } <button type="button" class="link" (click)="ui.open('passwords')">{{ au()?.account ? 'Edit account' : 'Accounts' }}</button></div>
          }
        }
        <div class="two">
          @if (hasLogin()) {
            <label class="f grow"><span>Username</span><input name="user" [(ngModel)]="s.username" [placeholder]="'asked when connecting'" spellcheck="false" autocomplete="off" /></label>
          }
          <label class="f" [class.grow]="!hasLogin()" [class.envf]="hasLogin()"><span>Environment</span>
            <select name="env" [ngModel]="s.environment || ''" (ngModelChange)="s.environment = $event" [style.color]="envColor()">
              <option value="">(none)</option>
              @for (v of envs(); track v.id) { <option [value]="v.id">{{ v.label }}</option> }
            </select></label>
        </div>
        @if (hasLogin()) {
          <div class="f"><span>Password</span>
            @if (pwOpen()) {
              <input name="pw" type="password" [(ngModel)]="pw" placeholder="saved in the system keychain" autocomplete="new-password" />
            } @else {
              <button type="button" class="pwbox" (click)="pwOpen.set(true)" title="Change the saved password">
                <span class="dots">{{ pwState() === 'set' ? '••••••••••' : 'asked when connecting' }}</span>
                @if (pwState() === 'set') { <span class="kc"><app-icon name="lock" [size]="12" /> In keychain</span> }
              </button>
            }
          </div>
        }

        @if (s.protocol === 'SSH2' && s.openAs !== 'sftp') {
          <div class="checks swc">
            <label title="A file browser beside the terminal, opposite the Session Manager (View → Files Panel shows or hides it for one tab)">
              <input type="checkbox" name="filesPanel" [(ngModel)]="s.filesPanel" />
              <span class="tx">Files panel beside the terminal<small>follows the terminal's folder; double-click opens a file here</small></span></label>
            <label><input type="checkbox" name="arcp" [(ngModel)]="s.autoReconnect" /><span class="tx">Reconnect automatically<small>when the connection drops</small></span></label>
          </div>
        }
        @if (edit().oldPath && (s.protocol === 'SSH2' || s.protocol === 'Telnet')) {
          <div class="facts">
            <div class="fh"><span>Device facts</span><span class="age">{{ factsAge(ui.factsOf(edit().oldPath)) }}</span>
              <button type="button" class="link" (click)="ui.refreshFacts(edit().oldPath, true)" title="Log in in the background and read the facts now">Refresh</button>
              <button type="button" class="link" (click)="more('facts')" title="Change, remove or add device facts">Edit</button></div>
            @for (f of facts(); track f[0]) { <div class="fr"><span>{{ f[0] }}</span><b [class.mono]="f[0] === 'MAC'">{{ f[1] }}</b></div> }
            @empty { <div class="fr none">{{ s.deviceType ? 'Not read yet - they are read after connecting.' : 'Pick a device type to read model, version, MAC and uptime.' }}</div> }
          </div>
        }
        <div class="adv" [class.open]="advOpen()">
          <button type="button" class="advh" (click)="advOpen.set(!advOpen())"><span class="tw">›</span> Advanced</button>
          @if (advOpen()) {
            @for (a of advanced(); track a.page) {
              <button type="button" class="arow" (click)="more(a.page)"><app-icon [name]="a.icon" [size]="14" /><span>{{ a.label }}</span><span class="v">{{ a.value }}</span><span class="chev">›</span></button>
            }
            <button type="button" class="arow all" (click)="more('')"><app-icon name="options" [size]="14" /><span>All session options…</span><span class="chev">›</span></button>
          }
        </div>
        @if (error()) { <div class="err">{{ error() }}</div> }
      </div>
      <footer>
        <button type="button" (click)="close()">Cancel</button>
        <button type="button" class="more" title="Open every option of this session (all pages)" (click)="more('')"><app-icon name="options" [size]="14" /> More Options…</button>
        <span class="grow"></span>
        <button type="submit">Save</button>
        <button type="button" class="primary sc" (click)="save(true)"><app-icon name="quick" [size]="14" /> {{ s.protocol === 'WOL' ? 'Save &amp; Wake' : 'Save &amp; Connect' }}</button>
      </footer>
    </form>
    }
  `,
  styles: [`
    :host { flex: none; width: min(510px, 48vw); min-width: 340px; display: flex; min-height: 0; border-left: 1px solid var(--line); background: var(--surface); }
    .sp { flex: 1; display: flex; flex-direction: column; min-width: 0; }
    header { display: flex; align-items: center; gap: 10px; padding: 16px 14px 12px 18px; border-bottom: 1px solid var(--line); margin-bottom: 10px;
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
      select { flex: 0 0 76px; min-height: 0; cursor: pointer; padding: 0 4px; }
      button:hover:not(.on) { color: var(--text); background: transparent; }
      .on { background: var(--surface); color: var(--text); box-shadow: 0 1px 2px rgba(0, 0, 0, .18), 0 0 0 1px var(--line); } }
    .chip { display: flex; align-items: center; gap: 8px; height: 32px; padding: 0 10px; border: 1px solid var(--line); border-radius: var(--r);
      background: var(--surface-2); color: var(--text); text-align: left;
      app-icon { color: var(--ok); } .chg { margin-left: auto; color: var(--accent); font-size: calc(12px * var(--ui-scale, 1)); } }
    .envf { width: 150px; flex: none; }
    .swc { border: 1px solid var(--line); border-radius: var(--r-lg); padding: 8px 12px 2px; }
    .facts { border: 1px solid var(--line); border-radius: var(--r-lg); padding: 8px 12px; display: flex; flex-direction: column; gap: 5px;
      .fh { display: flex; align-items: center; gap: 8px; font-weight: 600; .age { color: var(--faint); font-weight: 400; font-size: calc(11.5px * var(--ui-scale, 1)); } .link:first-of-type { margin-left: auto; } }
      .fr { display: flex; gap: 10px; font-size: calc(12.5px * var(--ui-scale, 1)); span { width: 64px; flex: none; color: var(--muted); } b { font-weight: 500; overflow-wrap: anywhere; } }
      .fr.none { color: var(--muted); }
      .mono { font-family: var(--mono); } }
    .pwbox { width: 100%; height: 32px; display: flex; align-items: center; gap: 8px; padding: 0 10px; border: 1px solid var(--border); border-radius: var(--r);
      background: var(--input-bg); text-align: left; .dots { flex: 1; color: var(--muted); letter-spacing: .05em; }
      .kc { display: inline-flex; align-items: center; gap: 4px; color: var(--ok); font-size: calc(11.5px * var(--ui-scale, 1)); } }
    .note { margin: 0; color: var(--muted); line-height: 1.5; b { color: var(--text); font-weight: 600; } }
    .link { border: 0; background: transparent; padding: 0; color: var(--accent); height: auto; &:hover { background: transparent; text-decoration: underline; } }
    .adv { margin-top: 4px; border: 1px solid var(--line); border-radius: var(--r-lg); overflow: hidden; }
    .advh { width: 100%; height: 36px; display: flex; align-items: center; gap: 8px; padding: 0 12px; border: 0; border-radius: 0; background: var(--surface-2); font-weight: 600; text-align: left;
      .tw { display: inline-block; transition: transform .12s; color: var(--muted); } }
    .adv.open .advh .tw { transform: rotate(90deg); }
    .arow app-icon { color: var(--muted); }
    .arow.all span { color: var(--accent); }
    .arow { width: 100%; height: 36px; display: flex; align-items: center; gap: 8px; padding: 0 12px; border: 0; border-top: 1px solid var(--line); border-radius: 0;
      background: transparent; text-align: left;
      &:hover { background: var(--hover); }
      .v { margin-left: auto; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 55%; }
      .chev { color: var(--faint); } }
    .err { color: var(--danger); }
    footer .more { display: inline-flex; align-items: center; gap: 5px; white-space: nowrap; }
    footer { display: flex; align-items: center; gap: 8px; padding: 12px 14px 12px 18px; border-top: 1px solid var(--line);
      .sc { display: inline-flex; align-items: center; gap: 6px; }
      .grow { flex: 1; } .link { border: 0; background: transparent; padding: 0; color: var(--accent); height: auto; &:hover { background: transparent; text-decoration: underline; } } }
  `],
})
export class SessionPanel {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  readonly edit = input.required<Edit>();

  readonly main = ['SSH2', 'Telnet', 'Serial', 'RDP'];
  readonly others = PROTOCOLS.filter(p => !['SSH2', 'Telnet', 'Serial', 'RDP'].includes(p));
  readonly bauds = BAUD_RATES;
  /** Linux device types turn the Files panel on (it stays a choice). */
  onDeviceType() { if (this.s) filesPanelFor(this.s); }
  readonly envs = envList;
  /** Every folder of the Session Manager, for the Folder field. */
  readonly folders = computed(() => {
    const out: string[] = [];
    const walk = (n: SessionNode) => (n.children ?? []).forEach(c => { if (c.isFolder) { out.push(c.path); walk(c); } });
    const r = this.ui.tree();
    if (r) walk(r);
    return out;
  });
  readonly deviceTypes = computed(() => (this.ui.settings()?.deviceTypes ?? []).map(d => d.name));
  readonly error = signal('');
  readonly pwOpen = signal(false);
  readonly pwState = signal('');
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

  canLogin() { return !!this.s && !['Serial', 'Local Shell', 'VNC', 'WOL'].includes(this.s.protocol); }
  /** The session's own username / password fields are used: "Own", or a Default that resolves to its own login. */
  hasLogin() {
    if (!this.canLogin()) return false;
    const c = accountChoice(this.s!);
    return c === '-' || (c === '' && defaultLogin(this.ui.settings(), this.s!).kind === 'own');
  }
  accounts() { return this.ui.settings()?.credentials ?? []; }
  acctChoice() { return this.s ? accountChoice(this.s) : ''; }
  setAcct(v: string) {
    if (!this.s) return;
    if (v === '+') { const keep = accountChoice(this.s); setAccountChoice(this.s, keep + ' '); setTimeout(() => setAccountChoice(this.s!, keep)); this.ui.open('passwords'); return; }
    setAccountChoice(this.s, v);
  }
  defText() { return this.s ? defaultText(defaultLogin(this.ui.settings(), this.s)) : ''; }
  au() { return this.acctUser(); }
  /** Who logs in when it is not the session's own login. */
  acctUser(): { user: string; how: string; account?: string } | null {
    if (!this.s) return null;
    const c = accountChoice(this.s);
    if (c && c !== '-') return { user: userOf(this.ui.settings(), c), how: 'with the account ' + c, account: c };
    const d = defaultLogin(this.ui.settings(), this.s);
    if (d.kind === 'account') return { user: d.user ?? '', how: `with the account ${d.name} (from the ${d.from})`, account: d.name };
    if (d.kind === 'device') return { user: d.user ?? '', how: `with the ${d.from}'s login` };
    return null;
  }
  deviceUser() { return (this.ui.settings()?.deviceTypes ?? []).find(d => d.name === this.s?.deviceType)?.username ?? ''; }
  pwText() { return this.pwState() === 'set' ? 'Saved in the system keychain' : 'Asked when connecting'; }

  pingMins(s: Session) { const v = s.wolPingSecs || 120; return v < 0 ? 'never' : v >= 120 ? Math.round(v / 60) + ' minutes' : v + ' seconds'; }

  private autoNet = '';
  /** The IP address of a Wake-on-LAN session: its network is filled in (the mask can still be changed) unless it was typed. */
  wolIp(v: string) {
    const s = this.s;
    if (!s) return;
    s.hostname = v;
    const ip = (v || '').trim();
    if (!/^\d+\.\d+\.\d+\.\d+$/.test(ip) || (s.wolNetwork && s.wolNetwork !== this.autoNet)) return;
    void this.backend.wakeNetworkFor(ip).then(n => {
      if (n && (!s.wolNetwork || s.wolNetwork === this.autoNet)) { s.wolNetwork = n; this.autoNet = n; }
    }).catch(() => {});
  }

  setProto(p: string) {
    if (!this.s) return;
    this.s.protocol = p;
    if (p === 'WOL') { this.s.deviceType = ''; if (!this.s.wolNetwork && this.s.hostname) this.s.wolNetwork = this.s.hostname.replace(/\.\d+$/, '.0/24'); }
    this.s.port = defaultPort(p) || this.s.port;
    if (p === 'Console' && !this.s.consoleMethod) { this.s.consoleMethod = 'opengear-ssh'; this.s.initialCr = true; }
  }

  /** The settings kept in the full dialog, with what they are set to now. */
  readonly advOpen = signal(true);
  readonly advanced = () => {
    const s = this.s;
    if (!s) return [];
    const out: { page: string; label: string; value: string; icon: string }[] = [];
    const term = !['RDP', 'VNC', 'WOL'].includes(s.protocol);
    if (['SSH2', 'Telnet', 'Raw', 'WOL'].includes(s.protocol)) out.push({ page: 'firewall', icon: 'connect', label: 'Jump host / proxy', value: s.jumpSession ? s.jumpSession.split('/').pop()! : s.proxyHost || 'none' });
    if (s.protocol === 'SSH2') out.push({ page: 'forward', icon: 'tile-v', label: 'Port forwarding', value: s.portForwards?.length ? s.portForwards.length + ' rule' + (s.portForwards.length === 1 ? '' : 's') : 'none' });
    if (s.protocol === 'Serial') out.push({ page: 'serial', icon: 'serial', label: 'Serial settings', value: `${s.dataBits || 8}-${['N', 'O', 'E', 'M', 'S'][s.parity] ?? 'N'}-${s.stopBits || 1}` });
    if (term) {
      const n = (s.logonActions?.length ?? 0) + (s.triggers?.length ?? 0);
      out.push({ page: 'logon', icon: 'script', label: 'Logon actions & triggers', value: n ? String(n) : s.deviceType && !s.ownLogin ? 'from device type' : 'none' });
      out.push({ page: 'terminal', icon: 'shell', label: 'Terminal & appearance', value: [s.keywordSet ? s.keywordSet + ' keywords' : '', s.colorScheme].filter(Boolean).join(' · ') || s.emulation || 'default' });
      out.push({ page: 'log', icon: 'log', label: 'Logging & recording', value: s.logFile ? 'on' : 'off' });
    }
    if (['SSH2', 'Telnet'].includes(s.protocol)) out.push({ page: 'connection', icon: 'find', label: 'MAC address (find it when its IP changes)', value: s.mac || 'not saved' });
    if (s.protocol === 'SSH2') out.push({ page: 'auth', icon: 'key', label: 'Keys, certificates & smart cards', value: s.identityFile ? s.identityFile.split(/[\\/]/).pop()! : s.smartCard ? 'smart card' : 'none' });
    return out;
  };

  facts() { return factList(this.ui.factsOf(this.edit().oldPath)); }
  readonly factsAge = factsAge;

  devKind() { return this.s?.protocol === 'WOL' ? null : deviceKindOf(this.s?.deviceType); }
  protoKind(): SessionKind {
    const m: Record<string, SessionKind> = { Serial: 'serial', Console: 'console', 'Local Shell': 'shell', Telnet: 'telnet', Raw: 'raw', RDP: 'rdp', VNC: 'vnc', WOL: 'wol' };
    return m[this.s?.protocol ?? ''] ?? (this.s?.openAs === 'sftp' ? 'sftp' : 'ssh');
  }
  envColor() { return this.envs().find(e => e.id === this.s?.environment)?.color ?? null; }

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
    if (s.protocol === 'WOL' && !s.mac?.trim()) { this.error.set('Please enter the MAC address of the device to wake.'); return; }
    if (s.protocol === 'WOL' && !s.hostname?.trim() && !s.wolNetwork?.trim()) { this.error.set('Please enter the network (e.g. 192.168.1.0) or the IP address of the device - the magic packet goes to that network.'); return; }
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
