import { ChangeDetectorRef, Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { askRef, isRef, refOf } from '../core/secret-ref';
import { Backend } from '../core/backend';
import { ACTIONS, DeviceType, KeyBinding, PROTOCOLS, Settings, comboOf } from '../core/models';
import { COLOR_KEYS, SCHEME_NAMES, fullScheme } from '../core/themes';
import { Ui } from '../core/ui.service';

interface DtPw { orig: string; pw: string; empty: boolean; state: string; forget: boolean; }
type Page = 'general' | 'terminal' | 'schemes' | 'keywords' | 'keymap' | 'buttons' | 'updates' | 'devices' | 'alerts';

/** Global Options dialog. */
@Component({
  selector: 'app-options-dialog',
  imports: [FormsModule],
  template: `
  @if (s) {
  <div class="modal-backdrop">
    <form class="modal wide" (ngSubmit)="save()">
      <div class="modal-head">Global Options</div>
      <div class="modal-split">
        <nav class="cat">
          <a [class.on]="page() === 'general'" (click)="page.set('general')">General</a>
          <a [class.on]="page() === 'terminal'" (click)="page.set('terminal')">Terminal &amp; Appearance</a>
          <a class="sub" [class.on]="page() === 'schemes'" (click)="page.set('schemes')">Color Schemes</a>
          <a [class.on]="page() === 'keywords'" (click)="page.set('keywords')">Keyword Highlighting</a>
          <a [class.on]="page() === 'keymap'" (click)="page.set('keymap')">Key Map</a>
          <a [class.on]="page() === 'buttons'" (click)="page.set('buttons')">Button Bar</a>
          <a [class.on]="page() === 'devices'" (click)="page.set('devices')">Device Types</a>
          <a [class.on]="page() === 'alerts'" (click)="page.set('alerts')">Alerts</a>
          <a [class.on]="page() === 'updates'" (click)="page.set('updates')">Updates</a>
        </nav>
        <div class="modal-body">
          @switch (page()) {
            @case ('general') {
              <div class="grid">
                <label>Sessions folder</label>
                <div class="row"><input name="sd" [(ngModel)]="s.sessionsDir" /><button type="button" (click)="browse('sessionsDir')">Browse...</button></div>
                <label></label>
                <div class="hint">Sessions are stored as one .ini file per session in the SecureCRT-compatible layout.
                  You can point this at an existing "Config/Sessions" folder to share sessions (back it up first).</div>
                <label>Logs folder</label>
                <div class="row"><input name="ld" [(ngModel)]="s.logDir" /><button type="button" (click)="browse('logDir')">Browse...</button></div>
                <label>Downloads folder</label>
                <div class="row"><input name="dd" [(ngModel)]="s.downloadDir" /><button type="button" (click)="browse('downloadDir')">Browse...</button></div>
                <label>Python interpreter</label>
                <input name="py" [(ngModel)]="s.pythonPath" placeholder="(automatic: python3, or py -3 on Windows)" />
                <label></label>
                <div class="hint">Used to run SecureCRT-style Python (.py) scripts. Python 3 must be installed.</div>
                <label></label><div class="hint">Files received with ZMODEM (remote "sz") are saved here.</div>
                <label>UI theme</label>
                <select name="theme" [(ngModel)]="s.theme">
                  <option value="system">Follow system</option><option value="dark">Dark</option><option value="light">Light</option>
                </select>
                <label>Accent color</label>
                <div class="row"><input type="color" name="acc" class="swatch" [ngModel]="s.accentColor || '#4f9cf7'" (ngModelChange)="s.accentColor = $event" />
                  <button type="button" (click)="s.accentColor = ''">Default</button></div>
                <label>Behavior</label>
                <div class="checks">
                  <label><input type="checkbox" name="cc" [(ngModel)]="s.confirmClose" /> Confirm before closing connected tabs / quitting</label>
                  <label><input type="checkbox" name="cpaste" [(ngModel)]="s.confirmPaste" /> Confirm before pasting more than one line</label>
                  <label><input type="checkbox" name="rt" [(ngModel)]="s.restoreTabs" /> Reopen the sessions that were open when Termwright was closed</label>
                  <label><input type="checkbox" name="sm" [(ngModel)]="s.showSessionManager" /> Show Session Manager</label>
                  <label><input type="checkbox" name="bb" [(ngModel)]="s.showButtonBar" /> Show Button Bar</label>
                  <label><input type="checkbox" name="cw" [(ngModel)]="s.showCommandWindow" /> Show Command Window</label>
                  <label><input type="checkbox" name="cbar" [(ngModel)]="s.showConnectBar" /> Show Connect Bar in the toolbar</label>
                  <label><input type="checkbox" name="iwm" [(ngModel)]="s.inWindowMenu" /> Show the menu bar inside the window (macOS also keeps the top-of-screen menu)</label>
                </div>
                <label>Data folder</label><div class="hint mono">{{ dataDir() }}</div>
              </div>
            }
            @case ('terminal') {
              <div class="grid">
                <label>Font</label><input name="ff" [(ngModel)]="s.fontFamily" />
                <label>Font size</label><input name="fs" type="number" min="6" max="48" class="short" [(ngModel)]="s.fontSize" />
                <label>Color scheme</label>
                <select name="sch" [(ngModel)]="s.terminalScheme">
                  <option value="Auto">Auto (match the UI theme: Default / Light)</option>
                  @for (c of allSchemes(); track c) { <option [value]="c">{{ c }}</option> }
                </select>
                <label></label>
                <div class="checks">
                  <label><input type="checkbox" name="fts" [(ngModel)]="s.forceTerminalScheme" /> Use this scheme for all sessions (ignore schemes set in Session Options or imported sessions)</label>
                </div>
                <label>Cursor</label>
                <div class="row">
                  <select name="cs" [(ngModel)]="s.cursorStyle"><option value="block">Block</option><option value="underline">Underline</option><option value="bar">Vertical bar</option></select>
                  <label><input type="checkbox" name="cb" [(ngModel)]="s.cursorBlink" /> Blink</label>
                </div>
                <label>Scrollback lines</label><input name="sb" type="number" min="100" class="short" [(ngModel)]="s.scrollback" />
                <label>Mouse</label>
                <div class="checks">
                  <label><input type="checkbox" name="cos" [(ngModel)]="s.copyOnSelect" /> Copy on select</label>
                  <label><input type="checkbox" name="rcp" [(ngModel)]="s.rightClickPaste" /> Paste on right-click (Shift+right-click opens the menu)</label>
                </div>
                <label>Paste line delay</label>
                <div class="row"><input name="pld" type="number" min="0" class="short" [(ngModel)]="s.pasteLineDelayMs" /> ms between lines (0 = off; useful for slow network devices)</div>
              </div>
            }
            @case ('schemes') {
              <div class="grid">
                <label>Scheme</label>
                <div class="row">
                  <select name="schEdit" [ngModel]="schemeName()" (ngModelChange)="schemeName.set($event)">
                    @for (c of allSchemes(); track c) { <option [value]="c">{{ c }}{{ isCustom(c) ? ' (custom)' : '' }}</option> }
                  </select>
                  <button type="button" (click)="copyScheme()">Copy as new...</button>
                  <button type="button" [disabled]="!isCustom(schemeName())" (click)="deleteScheme()">Delete</button>
                  <button type="button" (click)="importSchemes()">Import...</button>
                  <button type="button" [disabled]="!s.customSchemes?.length" (click)="exportSchemes()">Export...</button>
                </div>
              </div>
              <div class="scheme-preview" [style.background]="colors()['background']" [style.color]="colors()['foreground']">
                <div>user&#64;router:~$ <span [style.background]="colors()['cursor']">&nbsp;</span></div>
                <div class="sw">
                  @for (k of ansi; track k) { <span [style.color]="colors()[k]">■</span> }
                </div>
                <div><span [style.background]="colors()['selectionBackground']">selected text</span>
                  <span [style.color]="colors()['red']"> ERROR</span>&nbsp;<span [style.color]="colors()['green']">OK</span>
                  <span [style.color]="colors()['yellow']"> WARN</span> <span [style.color]="colors()['blue']"> 10.0.0.1</span></div>
              </div>
              @if (isCustom(schemeName())) {
                <div class="color-grid">
                  @for (k of colorKeys; track k) {
                    <label><input type="color" [name]="'c-' + k" class="swatch" [ngModel]="colors()[k]" (ngModelChange)="setColor(k, $event)" /> {{ k }}</label>
                  }
                </div>
              } @else {
                <p class="hint">Built-in schemes can't be edited. Use <b>Copy as new</b> to make an editable copy.</p>
              }
            }
            @case ('keywords') {
              <div class="grid">
                <label>Default set</label>
                <select name="dks" [(ngModel)]="s.defaultKeywordSet">
                  <option value="">None</option>
                  @for (k of s.keywordSets ?? []; track $index) { <option [value]="k.name">{{ k.name }}</option> }
                </select>
                <label>Edit set</label>
                <div class="row">
                  <select name="kse" [ngModel]="kwIndex()" (ngModelChange)="kwIndex.set(+$event)">
                    @for (k of s.keywordSets ?? []; track $index) { <option [ngValue]="$index">{{ k.name }}</option> }
                  </select>
                  <button type="button" (click)="addSet()">New...</button>
                  <button type="button" [disabled]="!curSet()" (click)="renameSet()">Rename...</button>
                  <button type="button" [disabled]="!curSet()" (click)="deleteSet()">Delete</button>
                </div>
              </div>
              @if (curSet(); as set) {
                <table class="list" style="margin-top: 12px">
                  <thead><tr><th>Text or pattern</th><th>Regex</th><th>Match case</th><th>Color</th><th></th></tr></thead>
                  <tbody>
                    @for (k of set.keywords; track $index) {
                      <tr>
                        <td><input [name]="'kt' + $index" [(ngModel)]="k.text" class="mono" /></td>
                        <td class="c"><input type="checkbox" [name]="'kr' + $index" [(ngModel)]="k.regex" /></td>
                        <td class="c"><input type="checkbox" [name]="'kc' + $index" [(ngModel)]="k.caseSensitive" /></td>
                        <td class="c"><input type="color" [name]="'kk' + $index" [(ngModel)]="k.color" class="swatch" /></td>
                        <td><button type="button" class="icon-btn" (click)="set.keywords.splice($index, 1)">✕</button></td>
                      </tr>
                    }
                  </tbody>
                </table>
                <button type="button" (click)="set.keywords.push({ text: '', regex: false, caseSensitive: false, color: '#6b5310' })">Add keyword</button>
                <p class="hint">Sessions can pick a set under Session Options → Terminal. Highlighting is skipped in full-screen apps (vi, less).</p>
              }
            }
            @case ('keymap') {
              <p class="hint">Click a <b>Keys</b> box and press the combination. Each binding can send text (\\r = Enter), run an action or run a script.
                Bindings apply while a terminal has focus and take priority over the remote application.</p>
              <table class="list">
                <thead><tr><th>Keys</th><th>Send text</th><th>Action</th><th>Script</th><th></th></tr></thead>
                <tbody>
                  @for (k of s.keyMap ?? []; track $index) {
                    <tr>
                      <td><input [name]="'kk' + $index" [value]="k.keys" readonly class="mono keycap" placeholder="press keys"
                                 (keydown)="capture($event, k)" /></td>
                      <td><input [name]="'ks' + $index" [(ngModel)]="k.send" class="mono" /></td>
                      <td><select [name]="'ka' + $index" [(ngModel)]="k.action">
                        <option value="">-</option>
                        @for (a of actions; track a[0]) { <option [value]="a[0]">{{ a[1] }}</option> }
                      </select></td>
                      <td><input [name]="'kx' + $index" [(ngModel)]="k.script" placeholder="path/to/script.js" /></td>
                      <td><button type="button" class="icon-btn" (click)="s.keyMap!.splice($index, 1)">✕</button></td>
                    </tr>
                  }
                </tbody>
              </table>
              <button type="button" (click)="(s.keyMap ??= []).push({ keys: '', send: '', action: '', script: '' })">Add binding</button>
            }
            @case ('devices') {
              <p class="hint">A device type is a login profile. Pick it in Quick Connect, the Connect Bar (<code>voss 10.1.1.1</code>) or a
                session's Connection page, and only the address is needed. <b>Login steps</b> answer username/password prompts and run for
                Telnet/Serial; <b>after-login steps</b> run for every protocol. In Send, <code>\\s</code> = username, <code>\\w</code> = password
                (from the device type, the session, or asked once).</p>
              <div class="row dt-pick">
                <select name="dtSel" [ngModel]="dtIndex()" (ngModelChange)="dtIndex.set(+$event)">
                  @for (d of s.deviceTypes ?? []; track $index) { <option [ngValue]="$index">{{ d.name }}</option> }
                </select>
                <button type="button" (click)="addDevice()">New</button>
                <button type="button" (click)="copyDevice()" [disabled]="!curDevice()">Duplicate</button>
                <button type="button" (click)="removeDevice()" [disabled]="!curDevice()">Delete</button>
              </div>
              @if (curDevice(); as d) {
                <div class="grid">
                  <label>Name</label><input name="dtn" [(ngModel)]="d.name" />
                  <label>Default protocol</label>
                  <div class="row">
                    <select name="dtp" [(ngModel)]="d.protocol">@for (p of protocols; track p) { <option [value]="p">{{ p }}</option> }</select>
                    Port <input name="dtport" type="number" class="short" [(ngModel)]="d.port" placeholder="default" />
                  </div>
                  <label>Username</label><input name="dtu" [(ngModel)]="d.username" placeholder="e.g. admin (blank = session's, or asked)" />
                  @if (pwOf(d); as p) {
                    <label>Password</label>
                    <div class="row wrap">
                      <input name="dtpw" [type]="isRef(p.pw) ? 'text' : 'password'" autocomplete="new-password" [(ngModel)]="p.pw" [disabled]="p.empty" [placeholder]="pwPh(p)" />
                      <button type="button" title="Read the password from 1Password, Bitwarden, HashiCorp Vault or a command" (click)="pickRef(p)">Password manager...</button>
                      <label class="inl"><input type="checkbox" name="dtnp" [(ngModel)]="p.empty" (ngModelChange)="$event && (p.pw = '')" /> No password</label>
                      @if ((p.state === 'set' || refOf(p.state)) && !p.forget && !p.pw) { <button type="button" (click)="p.forget = true">Forget</button> }
                    </div>
                    <label></label><div class="hint">Saved in the OS keychain. Sessions with this device type use this username and password
                      unless they untick "Use the device type's username and password". Tick <b>No password</b> for devices like EXOS where
                      <code>admin</code> has an empty password - you will not be asked again.</div>
                  }
                  @if (d.credential) {
                    <label>Credential</label>
                    <select name="dtc" [(ngModel)]="d.credential"><option value="">(device type / session password)</option>
                      @for (c of s.credentials ?? []; track c.name) { <option [value]="c.name">{{ c.name }}</option> }</select>
                  }
                  <label>Prompt (regex)</label><input name="dtpr" class="mono" [(ngModel)]="d.prompt" placeholder="e.g. :\\d+[>#] ?$" />
                  <label></label>
                  <div class="checks"><label><input type="checkbox" name="dticr" [(ngModel)]="d.initialCr" /> Send initial carriage return</label></div>
                </div>
                @for (part of stepParts; track part.key) {
                  <h4>{{ part.title }}</h4>
                  <table class="list">
                    <thead><tr><th>Expect</th><th>Send</th><th class="c">Hide</th><th class="c">CR</th><th></th></tr></thead>
                    <tbody>
                      @for (a of steps(d, part.key); track $index) {
                        <tr>
                          <td><input [name]="part.key + 'e' + $index" [(ngModel)]="a.expect" /></td>
                          <td><input class="mono" [name]="part.key + 's' + $index" [(ngModel)]="a.send" /></td>
                          <td class="c"><input type="checkbox" [name]="part.key + 'h' + $index" [(ngModel)]="a.hide" /></td>
                          <td class="c"><input type="checkbox" [name]="part.key + 'c' + $index" [(ngModel)]="a.trailingCr" /></td>
                          <td class="nowrap">
                            <button type="button" class="icon-btn" [disabled]="$index === 0" (click)="moveStep(d, part.key, $index, -1)">↑</button>
                            <button type="button" class="icon-btn" (click)="steps(d, part.key).splice($index, 1)">✕</button>
                          </td>
                        </tr>
                      }
                    </tbody>
                  </table>
                  <button type="button" (click)="steps(d, part.key).push({ expect: '', send: '', hide: false, trailingCr: true, credential: '' })">Add step</button>
                }
              }
            }
            @case ('alerts') {
              <p class="hint">Get notified when output in <b>any</b> tab matches - even tabs you are not looking at. Each pattern fires at most
                once every 5 seconds per tab. The tab gets a red dot until you open it.</p>
              <table class="list">
                <thead><tr><th class="c">On</th><th>Pattern</th><th class="c">Regex</th><th class="c">Aa</th><th class="c">Sound</th><th class="c">Desktop</th><th></th></tr></thead>
                <tbody>
                  @for (a of s.alerts ?? []; track $index) {
                    <tr>
                      <td class="c"><input type="checkbox" [name]="'ae' + $index" [(ngModel)]="a.enabled" /></td>
                      <td><input class="mono" [name]="'ap' + $index" [(ngModel)]="a.pattern" placeholder="%LINK-3- or CRITICAL" /></td>
                      <td class="c"><input type="checkbox" [name]="'ar' + $index" [(ngModel)]="a.regex" /></td>
                      <td class="c"><input type="checkbox" [name]="'ac' + $index" [(ngModel)]="a.caseSensitive" title="Case sensitive" /></td>
                      <td class="c"><input type="checkbox" [name]="'as' + $index" [(ngModel)]="a.sound" /></td>
                      <td class="c"><input type="checkbox" [name]="'ad' + $index" [(ngModel)]="a.desktop" title="Operating-system notification when Termwright is in the background" /></td>
                      <td><button type="button" class="icon-btn" (click)="s.alerts!.splice($index, 1)">✕</button></td>
                    </tr>
                  }
                </tbody>
              </table>
              <button type="button" (click)="(s.alerts ??= []).push({ pattern: '', regex: false, caseSensitive: false, enabled: true, sound: true, desktop: true })">Add alert</button>
            }
            @case ('updates') {
              <p class="hint">Help → <b>Check for Updates…</b> shows the installed version, the latest stable release with its changes, and any newer nightly build. These settings decide when you are told about them.</p>
              <div class="grid">
                <label>Notify me about</label>
                <select name="uch" [(ngModel)]="s.updateChannel">
                  <option value="stable">Stable releases (recommended)</option>
                  <option value="nightly">Stable releases and nightly builds</option>
                  <option value="off">Nothing - I check myself</option>
                </select>
                <label></label>
                <div class="checks"><label><input type="checkbox" name="uos" [(ngModel)]="s.updateOnStart" [disabled]="s.updateChannel === 'off'" /> Check at start-up (once a day)</label></div>
                @if (s.updateChannel === 'nightly') {
                  <label></label>
                  <div class="warn">Nightly builds are untested builds of the latest code. They can crash or damage your settings and saved sessions - back up first (the update window has a <b>Back Up Now</b> button).</div>
                }
              </div>
              <button type="button" (click)="checkNow()">Check for Updates…</button>
              <details class="adv">
                <summary>Advanced - only for your own fork</summary>
                <div class="grid">
                  <label>Repository</label><input name="urepo" [(ngModel)]="s.updateRepo" placeholder="owner/name" />
                  <label>GitHub token</label>
                  <div class="row"><input type="password" name="utok" [(ngModel)]="token" [placeholder]="hasToken() ? '(saved in the OS keychain - type to replace)' : 'only for a private fork'" autocomplete="off" />
                    <button type="button" (click)="saveToken()">Save</button>
                    @if (hasToken()) { <button type="button" (click)="clearToken()">Remove</button> }</div>
                  <label></label>
                  <div class="hint">Leave these alone to get the official Termwright releases. For a private fork use a fine-grained token with read-only <b>Contents</b> access to that repository.</div>
                </div>
              </details>
            }
            @case ('buttons') {
              <p class="hint">Text is sent as typed; use \\r for Enter, \\t Tab, \\x03 Ctrl+C, \\e Escape.</p>
              <table class="list">
                <thead><tr><th>Label</th><th>Send</th><th>All tabs</th><th>Action</th><th>Script</th><th></th></tr></thead>
                <tbody>
                  @for (b of s.buttons; track $index) {
                    <tr>
                      <td><input [name]="'bl' + $index" [(ngModel)]="b.label" /></td>
                      <td><input [name]="'bs' + $index" [(ngModel)]="b.send" class="mono" /></td>
                      <td class="c"><input type="checkbox" [name]="'ba' + $index" [(ngModel)]="b.toAll" /></td>
                      <td><select [name]="'bx' + $index" [(ngModel)]="b.action"><option value="">-</option>
                        @for (a of actions; track a[0]) { <option [value]="a[0]">{{ a[1] }}</option> }</select></td>
                      <td><input [name]="'bj' + $index" [(ngModel)]="b.script" placeholder="script.js" /></td>
                      <td class="nowrap">
                        <button type="button" class="icon-btn" [disabled]="$index === 0" (click)="move($index, -1)">↑</button>
                        <button type="button" class="icon-btn" [disabled]="$index === s.buttons.length - 1" (click)="move($index, 1)">↓</button>
                        <button type="button" class="icon-btn" (click)="s.buttons.splice($index, 1)">✕</button>
                      </td>
                    </tr>
                  }
                </tbody>
              </table>
              <button type="button" (click)="s.buttons.push({ label: 'New', send: '\\\\r', toAll: false, action: '', script: '' })">Add button</button>
            }
          }
        </div>
      </div>
      <div class="modal-foot">
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="submit" class="primary">OK</button>
      </div>
    </form>
  </div>
  }`,
})
export class OptionsDialog implements OnInit {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private cdr = inject(ChangeDetectorRef);
  readonly page = signal<Page>('general');
  readonly dataDir = signal('');
  s: Settings | null = null;
  readonly kwIndex = signal(0);
  readonly actions = Object.entries(ACTIONS);
  readonly colorKeys = COLOR_KEYS;
  readonly ansi = COLOR_KEYS.slice(4);
  readonly schemeName = signal('Default');
  private schemeRev = signal(0);
  readonly allSchemes = computed(() => {
    this.schemeRev();
    return [...SCHEME_NAMES, ...(this.s?.customSchemes ?? []).map(c => c.name)];
  });
  readonly colors = computed(() => {
    this.schemeRev();
    return fullScheme(this.schemeName(), this.s?.customSchemes ?? []);
  });

  readonly protocols = PROTOCOLS;
  readonly stepParts = [{ key: 'loginSteps' as const, title: 'Login steps (Telnet / Serial / Raw)' }, { key: 'afterLogin' as const, title: 'After-login steps (all protocols)' }];
  readonly dtIndex = signal(0);
  curDevice() { return this.s?.deviceTypes?.[this.dtIndex()] ?? null; }
  /** Password edits per device type, saved to the keychain on OK. */
  private readonly dtPw = new Map<DeviceType, DtPw>();
  private readonly dtRemoved: string[] = [];
  pwOf(d: DeviceType): DtPw {
    let x = this.dtPw.get(d);
    if (!x) {
      const p: DtPw = { orig: d.name.trim(), pw: '', empty: false, state: '', forget: false };
      this.dtPw.set(d, (x = p));
      if (p.orig) this.backend.deviceTypePasswordState(p.orig).then(st => { p.state = st; p.empty = st === 'empty'; this.cdr.markForCheck(); }).catch(() => {});
    }
    return x;
  }
  readonly isRef = isRef;
  readonly refOf = refOf;
  async pickRef(p: DtPw) {
    const r = await askRef(this.ui, this.backend, isRef(p.pw) ? p.pw : refOf(p.state));
    if (r) { p.pw = r; p.empty = false; p.forget = false; this.cdr.markForCheck(); }
  }
  pwPh(p: DtPw) {
    if (refOf(p.state) && !p.forget) return 'from ' + refOf(p.state);
    if (p.empty) return 'no password';
    if (p.forget) return 'will be removed';
    return p.state === 'set' ? '•••••• saved (type to change)' : 'not saved (asked when connecting)';
  }
  steps(d: DeviceType, k: 'loginSteps' | 'afterLogin') { return (d[k] ??= []); }
  moveStep(d: DeviceType, k: 'loginSteps' | 'afterLogin', i: number, dir: number) {
    const l = this.steps(d, k);
    [l[i], l[i + dir]] = [l[i + dir], l[i]];
  }
  addDevice() {
    if (!this.s) return;
    (this.s.deviceTypes ??= []).push({ name: 'New device', protocol: 'SSH2', port: 0, username: '', credential: '', initialCr: false,
      loginSteps: [{ expect: 'login:', send: '\\s', hide: false, trailingCr: true, credential: '' }, { expect: 'assword:', send: '\\w', hide: true, trailingCr: true, credential: '' }],
      afterLogin: [], prompt: '' });
    this.dtIndex.set(this.s.deviceTypes.length - 1);
  }
  copyDevice() {
    const d = this.curDevice();
    if (!d || !this.s) return;
    this.s.deviceTypes!.push({ ...structuredClone(d), name: d.name + ' (copy)' });
    this.dtIndex.set(this.s.deviceTypes!.length - 1);
  }
  removeDevice() {
    if (!this.s?.deviceTypes) return;
    const gone = this.s.deviceTypes[this.dtIndex()];
    if (gone) { const p = this.pwOf(gone); if (p.orig && p.state) this.dtRemoved.push(p.orig); }
    this.s.deviceTypes.splice(this.dtIndex(), 1);
    this.dtIndex.set(0);
  }

  token = '';
  readonly hasToken = signal(false);

  async saveToken() {
    if (!this.token.trim()) return;
    try { await this.backend.setUpdateToken(this.token); this.token = ''; this.hasToken.set(true); this.ui.toast('Token saved', 'success'); }
    catch (e) { this.ui.error(e); }
  }

  async clearToken() {
    try { await this.backend.setUpdateToken(''); this.hasToken.set(false); } catch (e) { this.ui.error(e); }
  }

  async checkNow() {
    await this.save();
    this.ui.open('update');
  }

  isCustom(n: string) { return !!this.s?.customSchemes?.some(c => c.name === n); }

  async copyScheme() {
    if (!this.s) return;
    const name = await this.ui.input('New Color Scheme', 'Name', this.schemeName() + ' (copy)');
    if (!name || this.allSchemes().includes(name)) return;
    this.s.customSchemes = [...(this.s.customSchemes ?? []), { name, colors: fullScheme(this.schemeName(), this.s.customSchemes ?? []) }];
    this.schemeName.set(name);
    this.schemeRev.update(v => v + 1);
  }

  deleteScheme() {
    if (!this.s) return;
    const n = this.schemeName();
    this.s.customSchemes = (this.s.customSchemes ?? []).filter(c => c.name !== n);
    if (this.s.terminalScheme === n) this.s.terminalScheme = 'Default';
    this.schemeName.set('Default');
    this.schemeRev.update(v => v + 1);
  }

  setColor(k: string, v: string) {
    const c = this.s?.customSchemes?.find(x => x.name === this.schemeName());
    if (c) { c.colors = { ...c.colors, [k]: v }; this.schemeRev.update(x => x + 1); }
  }

  async exportSchemes() {
    const p = await this.backend.chooseSaveFile('Export Color Schemes', 'termwright-schemes.json', [{ displayName: 'JSON', pattern: '*.json' }]).catch(() => '');
    if (p) await this.backend.saveTextFile(p, JSON.stringify(this.s?.customSchemes ?? [], null, 2)).then(() => this.ui.toast('Exported to ' + p, 'success'), e => this.ui.error(e));
  }

  async importSchemes() {
    const p = await this.backend.chooseOpenFile('Import Color Schemes', [{ displayName: 'JSON', pattern: '*.json' }]).catch(() => '');
    if (!p || !this.s) return;
    try {
      const list = JSON.parse(await this.backend.readTextFile(p)) as { name: string; colors: Record<string, string> }[];
      const ok = (Array.isArray(list) ? list : [list]).filter(x => x && typeof x.name === 'string' && x.colors && typeof x.colors === 'object');
      const existing = new Set(this.allSchemes());
      for (const sc of ok) {
        let name = sc.name;
        while (existing.has(name)) name += ' (imported)';
        existing.add(name);
        const colors: Record<string, string> = {};
        for (const k of COLOR_KEYS) if (typeof sc.colors[k] === 'string' && /^#[0-9a-f]{6}$/i.test(sc.colors[k])) colors[k] = sc.colors[k];
        this.s.customSchemes = [...(this.s.customSchemes ?? []), { name, colors }];
      }
      this.schemeRev.update(v => v + 1);
      this.ui.toast(`Imported ${ok.length} scheme(s)`, 'success');
    } catch (e) { this.ui.error(e); }
  }

  capture(ev: KeyboardEvent, k: KeyBinding) {
    if (ev.key === 'Tab' && !ev.ctrlKey && !ev.altKey && !ev.metaKey) return; // keep keyboard navigation
    ev.preventDefault();
    ev.stopPropagation();
    if (ev.key === 'Backspace' && !ev.ctrlKey && !ev.altKey && !ev.metaKey && !ev.shiftKey) { k.keys = ''; return; }
    const c = comboOf(ev);
    if (c) k.keys = c;
  }

  curSet() { return this.s?.keywordSets?.[this.kwIndex()] ?? null; }

  async addSet() {
    const name = await this.ui.input('New Keyword Set', 'Name');
    if (!name || !this.s) return;
    this.s.keywordSets = [...(this.s.keywordSets ?? []), { name, keywords: [] }];
    this.kwIndex.set(this.s.keywordSets.length - 1);
    this.cdr.markForCheck();
  }

  async renameSet() {
    const set = this.curSet();
    if (!set || !this.s) return;
    const name = await this.ui.input('Rename Keyword Set', 'Name', set.name);
    if (!name) return;
    if (this.s.defaultKeywordSet === set.name) this.s.defaultKeywordSet = name;
    set.name = name;
    this.cdr.markForCheck();
  }

  deleteSet() {
    if (!this.s?.keywordSets) return;
    const set = this.curSet();
    this.s.keywordSets = this.s.keywordSets.filter(k => k !== set);
    if (set && this.s.defaultKeywordSet === set.name) this.s.defaultKeywordSet = '';
    this.kwIndex.set(0);
  }

  ngOnInit() {
    const cur = this.ui.settings();
    if (cur) this.s = structuredClone(cur);
    if (this.s) this.schemeName.set(!this.s.terminalScheme || this.s.terminalScheme === 'Auto' ? 'Default' : this.s.terminalScheme);
    this.schemeRev.update(v => v + 1);
    this.backend.getAppInfo().then(i => this.dataDir.set(i.dataDir)).catch(() => {});
    this.backend.hasUpdateToken().then(v => this.hasToken.set(v)).catch(() => {});
  }

  async browse(field: 'sessionsDir' | 'logDir' | 'downloadDir') {
    const d = await this.backend.chooseDirectory('Choose Folder').catch(() => '');
    if (d && this.s) this.s[field] = d;
    this.cdr.markForCheck();
  }

  move(i: number, d: number) {
    const b = this.s!.buttons;
    [b[i], b[i + d]] = [b[i + d], b[i]];
  }

  private async saveDevicePasswords() {
    const list = (this.s?.deviceTypes ?? []).filter(d => d.name.trim());
    const kept = new Set(list.map(d => d.name.trim().toLowerCase()));
    for (const n of this.dtRemoved) if (!kept.has(n.toLowerCase())) await this.backend.setDeviceTypePassword(n, '', false);
    for (const d of list) {
      const p = this.dtPw.get(d);
      if (!p) continue;
      const name = d.name.trim();
      if (p.orig && p.state && p.orig.toLowerCase() !== name.toLowerCase()) await this.backend.renameDeviceTypePassword(p.orig, name);
      if (p.pw) { await this.backend.setDeviceTypePassword(name, p.pw, false); d.credential = ''; }
      else if (p.empty !== (p.state === 'empty')) { await this.backend.setDeviceTypePassword(name, '', p.empty); if (p.empty) d.credential = ''; }
      else if (p.forget) await this.backend.setDeviceTypePassword(name, '', false);
    }
  }

  async save() {
    if (!this.s) return;
    this.s.fontSize = Number(this.s.fontSize) || 14;
    this.s.scrollback = Number(this.s.scrollback) || 10000;
    this.s.pasteLineDelayMs = Number(this.s.pasteLineDelayMs) || 0;
    this.s.keyMap = (this.s.keyMap ?? []).filter(k => k.keys && (k.send || k.action || k.script));
    await this.saveDevicePasswords().catch(e => this.ui.error(e));
    this.s.deviceTypes = (this.s.deviceTypes ?? []).filter(d => d.name.trim()).map(d => ({ ...d, name: d.name.trim(), port: Number(d.port) || 0,
      loginSteps: (d.loginSteps ?? []).filter(x => x.expect || x.send), afterLogin: (d.afterLogin ?? []).filter(x => x.expect || x.send) }));
    this.s.alerts = (this.s.alerts ?? []).filter(a => a.pattern.trim());
    await this.ui.saveSettings(this.s);
    this.ui.close();
  }
}
