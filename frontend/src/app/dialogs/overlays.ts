import { menuBar } from '../core/menu';
import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Autofocus } from '../core/autofocus';
import { Backend } from '../core/backend';
import { AppInfo } from '../core/models';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';

/** Confirm / input dialogs, context menu and toasts. */
@Component({
  selector: 'app-overlays',
  imports: [FormsModule, Autofocus, Icon],
  template: `
    @if (ui.confirmReq(); as c) {
      <div class="modal-backdrop">
        <form class="modal small" (ngSubmit)="closeConfirm(true)">
          <div class="modal-head"><span class="mh-ic" [class.dz]="c.danger"><app-icon [name]="c.danger ? 'alert' : 'check'" [size]="17" /></span><span class="mh-t">{{ c.title }}</span></div>
          <div class="modal-body"><p class="pre">{{ c.message }}</p></div>
          <div class="modal-foot"><span class="spacer"></span>
            <button type="button" (click)="closeConfirm(false)">Cancel</button>
            <button type="submit" class="primary" [class.danger]="c.danger" appAutofocus>{{ c.ok }}</button>
          </div>
        </form>
      </div>
    }
    @if (ui.choiceReq(); as c) {
      <div class="modal-backdrop">
        <div class="modal choice" role="dialog" aria-modal="true">
          <div class="modal-head"><span class="mh-ic" [class.dz]="c.danger"><app-icon [name]="c.danger ? 'alert' : 'connect'" [size]="17" /></span><span class="mh-t">{{ c.title }}</span></div>
          <div class="modal-body"><p class="pre">{{ c.message }}</p>
            @if (c.items.length) { <ul class="cl">@for (i of c.items; track i) { <li>{{ i }}</li> }</ul> }</div>
          <div class="modal-foot choice-foot">
            @for (o of c.options; track o.value) {
              <button type="button" [class.primary]="o.primary" [class.danger]="o.danger" (click)="closeChoice(o.value)">{{ o.label }}</button>
            }
            <button type="button" class="ghost" (click)="closeChoice(null)">Cancel</button>
          </div>
        </div>
      </div>
    }
    @if (ui.inputReq(); as r) {
      <div class="modal-backdrop">
        <form class="modal small" (ngSubmit)="closeInput(true)">
          <div class="modal-head"><span class="mh-ic"><app-icon [name]="inputIcon(r.title, r.secret)" [size]="17" /></span><span class="mh-t">{{ r.title }}</span></div>
          <div class="modal-body"><label class="field">{{ r.label }}
            <input name="v" [(ngModel)]="r.value" [type]="r.secret ? 'password' : 'text'" [attr.autocomplete]="r.secret ? 'off' : null" appAutofocus /></label></div>
          <div class="modal-foot"><span class="spacer"></span>
            <button type="button" (click)="closeInput(false)">Cancel</button>
            <button type="submit" class="primary">{{ modern() ? okFor(r.title) : 'OK' }}</button>
          </div>
        </form>
      </div>
    }
    @if (ui.menu(); as m) {
      <div class="menu-catcher" (click)="ui.menu.set(null)" (contextmenu)="ui.menu.set(null); $event.preventDefault()"></div>
      <div class="ctx-menu" [style.left.px]="m.x" [style.top.px]="m.y">
        @for (it of m.items; track $index) {
          @if (it.separator) { <div class="sep"></div> }
          @else { <div class="item" [class.disabled]="it.disabled" (click)="run(it)"><span>{{ it.label }}</span>@if (it.hint) {<span class="hint-key">{{ it.hint }}</span>}</div> }
        }
      </div>
    }
    <div class="toasts">
      @for (t of ui.toasts(); track t.id) {
        <div class="toast" [class]="t.level">
          @if (t.actions?.length) { <button type="button" class="tx" title="Close" (click)="ui.dismissToast(t.id)">✕</button> }
          {{ t.message }}
          @if (t.actions?.length) {
            <div class="tacts">@for (a of t.actions; track a.label) { <button type="button" (click)="ui.dismissToast(t.id); a.run()">{{ a.label }}</button> }</div>
          }
        </div>
      }
    </div>
  `,
  styles: [`.field { display: flex; flex-direction: column; gap: 6px; } .pre { white-space: pre-wrap; margin: 0; }
    .modal.choice { width: min(520px, calc(100vw - 32px)); max-width: none; }
    .choice-foot { display: flex; flex-direction: column; align-items: stretch; gap: 8px; }
    .choice-foot .spacer { display: none; }
    .choice-foot button { width: 100%; height: auto; min-height: 34px; padding: 8px 14px; white-space: normal; line-height: 1.3; text-align: center; margin: 0; }
    .choice-foot button.danger { background: var(--danger); border-color: var(--danger); color: #fff; }
    .choice-foot button.ghost { background: transparent; border-color: transparent; color: var(--muted); }
    .choice-foot button.ghost:hover { color: var(--text); background: var(--hover); }
    .cl { margin: 10px 0 0; padding: 6px 8px 6px 24px; max-height: 220px; overflow: auto; font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); background: var(--input-bg); border-radius: 6px; user-select: text; }
    .cl li { padding: 1px 0; }
    .mh-ic.dz { background: rgba(229, 72, 77, .14) !important; color: var(--danger) !important; }
    :host-context(.modern) .field { font-size: calc(12px * var(--ui-scale, 1)); font-weight: 500; color: var(--muted); input { font-size: calc(13px * var(--ui-scale, 1)); color: var(--text); min-height: 32px; } }`],
})
export class Overlays {
  protected ui = inject(Ui);
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  /** The button names what it does: Create, Rename, Save… */
  okFor(title: string) {
    const t = title.toLowerCase();
    return /^(new|create)\b/.test(t) ? 'Create' : /^rename\b/.test(t) ? 'Rename' : /^add\b/.test(t) ? 'Add' : /bookmark|save/.test(t) ? 'Save' : 'OK';
  }
  inputIcon(title: string, secret?: boolean) {
    const t = title.toLowerCase();
    return secret ? 'key' : /folder/.test(t) ? 'folder' : /bookmark/.test(t) ? 'star' : /rename|edit/.test(t) ? 'edit' : /new|add/.test(t) ? 'plus' : 'edit';
  }
  closeConfirm(v: boolean) {
    const c = this.ui.confirmReq();
    this.ui.confirmReq.set(null);
    c?.resolve(v);
  }

  closeChoice(v: string | null) {
    const c = this.ui.choiceReq();
    this.ui.choiceReq.set(null);
    c?.resolve(v);
  }

  closeInput(ok: boolean) {
    const r = this.ui.inputReq();
    this.ui.inputReq.set(null);
    r?.resolve(ok ? r.value.trim() || null : null);
  }

  run(it: { action?: () => void; disabled?: boolean }) {
    if (it.disabled) return;
    this.ui.menu.set(null);
    it.action?.();
  }
}

/** About and keyboard shortcuts (from the menus: core/shortcuts.json). */
@Component({
  selector: 'app-about',
  template: `
  <div class="modal-backdrop" (click)="ui.close()">
    <div class="modal" (click)="$event.stopPropagation()">
      @if (ui.dialog() === 'about') {
        <div class="modal-head cl-only">About Termwright</div>
        <div class="modal-body about">
          <div class="about-head"><img [src]="ui.lightUi() ? 'icon.svg' : 'icon-dark.svg'" alt="" width="64" height="64" />
            <div><h2>Termwright <small>{{ info()?.version }}</small></h2>
              @if (downloads() > 0) {
                <div class="dl" title="All files of all releases on GitHub, as GitHub counts them (the releases page and the in-app updater)">
                  ⬇ Downloaded {{ downloads().toLocaleString() }} time{{ downloads() === 1 ? '' : 's' }} from GitHub</div>
              }
            </div></div>
          <p>A free, open-source terminal emulator, session manager and file-transfer client for network and
            system engineers - one app for Windows, Linux and macOS.</p>
          @if (modern()) {
            <div class="tiles">
              @for (f of tiles; track f[0]) { <div class="tile"><b>{{ f[0] }}</b><span>{{ f[1] }}</span></div> }
            </div>
            <div class="btns">
              <button type="button" (click)="backend.openURL(repo + '/blob/main/docs/USER_GUIDE.md')">User Guide</button>
              <button type="button" (click)="backend.openURL(repo + '/issues')">Report an Issue</button>
              <button type="button" (click)="backend.openURL(repo + '/releases')">Releases</button>
              <button type="button" (click)="copyRepo()"><app-icon name="copy" [size]="13" />{{ copied() ? 'Copied' : 'Copy Link' }}</button>
            </div>
            <p class="hint">Created by <b>Natarajan Periannan</b> · <a class="mono" (click)="backend.openURL(repo)">{{ repo.replace('https://', '') }}</a></p>
          }
          <ul class="features cl-only">
            <li><b>Connect</b> - SSH2, Telnet, Serial (RTS/CTS, XON/XOFF), local shell and raw TCP; jump hosts, SOCKS/HTTP proxies, port forwarding, X11 and agent forwarding</li>
            <li><b>Organise</b> - device types (login profiles), session manager with folders and folder defaults, workspaces, Quick Open, tabs and tiled layouts; imports sessions from other terminal programs, PuTTY, MobaXterm and OpenSSH config</li>
            <li><b>Automate</b> - logon actions (expect/send), triggers, Python, VBScript and JavaScript scripting (with the common <code>crt</code> object), recorder, command window and command library, output alerts, compare outputs, button bar, key map</li>
            <li><b>Transfer</b> - SFTP browser with bookmarks, sync, resume and server-to-server copy, through jump stations and as root or another user; SCP, ZMODEM, YMODEM, XMODEM</li>
            <li><b>Secure</b> - OS keychain passwords, password managers (1Password, Bitwarden, HashiCorp Vault), Kerberos, SSH certificates and FIDO2 keys, master password, encrypted vault export, key login setup, known-hosts checking</li>
            <li><b>Capture</b> - session logging with timestamps, keyword highlighting, themes and color schemes</li>
          </ul>
          <p class="cl-only">Created by <b>Natarajan Periannan</b>.</p>
          <div class="share cl-only">
            <span>Open source on GitHub - share it with your team:</span>
            <a class="mono" (click)="backend.openURL(repo)">{{ repo }}</a>
            <button type="button" (click)="copyRepo()">{{ copied() ? 'Copied' : 'Copy Link' }}</button>
          </div>
          <p class="links cl-only">
            <a (click)="backend.openURL(repo + '/blob/main/docs/USER_GUIDE.md')">User Guide</a> ·
            <a (click)="backend.openURL(repo + '/issues')">Report an issue</a> ·
            <a (click)="backend.openURL(repo + '/releases')">Releases</a>
          </p>
          <p class="hint">{{ info()?.os }}/{{ info()?.arch }} · data folder: <span class="mono">{{ info()?.dataDir }}</span><br />
            Licensed under the Apache License 2.0 · built with Go, Wails, Angular and xterm.js.</p>
        </div>
      } @else {
        <div class="modal-head"><span class="mh-ic"><app-icon name="keyboard" [size]="17" /></span>
          <span class="mh-t">Keyboard Shortcuts<span class="mh-s">Every menu command{{ isMacOS ? ' - shown for macOS' : '' }}</span></span></div>
        <div class="modal-body">
          @if (modern()) {
            <input type="search" class="kq" [value]="kq()" (input)="kq.set($any($event.target).value)" placeholder="Find a command or a key" />
            <div class="kgroups">
              @for (g of keyGroups(); track g.title) {
                <div class="kg"><div class="kt">{{ g.title }}</div>
                  <div class="kc">@for (k of g.items; track k[1]) { <div class="kr"><span>{{ k[1] }}</span><span class="caps">@for (c of caps(k[0]); track $index) { <kbd>{{ c }}</kbd> }</span></div> }</div></div>
              } @empty { <div class="hint">Nothing matches.</div> }
            </div>
          }
          <table class="list keys cl-only">
            @for (k of keys; track k[0]) { <tr><td class="mono">{{ k[0] }}</td><td>{{ k[1] }}</td></tr> }
          </table>
        </div>
      }
      <div class="modal-foot">@if (modern() && ui.dialog() !== 'about') { <span class="hint">Add your own keys in Settings → Key Map</span> }<span class="spacer"></span><button class="primary" (click)="ui.close()">Close</button></div>
    </div>
  </div>`,
  imports: [Icon],
  styles: [`
    :host-context(.modern) {
      .about-head { margin-top: 4px; }
      .tiles { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin: 4px 0 12px; }
      .tile { border: 1px solid var(--line); border-radius: var(--r-lg); padding: 9px 11px; background: var(--surface-2); display: flex; flex-direction: column; gap: 2px;
        b { font-size: calc(12.5px * var(--ui-scale, 1)); } span { color: var(--muted); font-size: calc(11.5px * var(--ui-scale, 1)); line-height: 1.4; } }
      .btns { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 10px; button { display: inline-flex; align-items: center; gap: 6px; } }
      .hint a { color: var(--accent); cursor: pointer; }
      .kq { width: 100%; height: 34px; padding-left: 32px; margin-bottom: 12px; border-radius: var(--r-lg); background: var(--input-bg) no-repeat 11px 50% / 15px 15px
        url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%238d9099' stroke-width='2' stroke-linecap='round'%3E%3Ccircle cx='11' cy='11' r='7'/%3E%3Cpath d='m20 20-4-4'/%3E%3C/svg%3E"); }
      .kgroups { columns: 2; column-gap: 14px; max-height: 58vh; overflow: auto; }
      .kg { break-inside: avoid; margin-bottom: 12px; }
      .kt { font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .06em; text-transform: uppercase; color: var(--faint); margin-bottom: 6px; }
      .kc { border: 1px solid var(--line); border-radius: var(--r-lg); padding: 2px 10px; background: var(--surface); }
      .kr { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-height: 30px; border-top: 1px solid var(--line); font-size: calc(12.5px * var(--ui-scale, 1)); }
      .kr:first-child { border-top: 0; }
      .caps { display: inline-flex; gap: 3px; flex: none; }
      kbd { min-width: 22px; height: 22px; padding: 0 5px; border-radius: 5px; border: 1px solid var(--border); border-bottom-width: 2px; background: var(--surface-2);
        display: inline-grid; place-items: center; font: calc(11.5px * var(--ui-scale, 1)) var(--ui-font); color: var(--muted); }
    }
  `],
})
export class About {
  protected ui = inject(Ui);
  protected backend = inject(Backend);
  readonly repo = 'https://github.com/nperiannan/termwright';
  readonly info = signal<AppInfo | null>(null);
  readonly copied = signal(false);
  async copyRepo() {
    try { await navigator.clipboard.writeText(this.repo); this.copied.set(true); setTimeout(() => this.copied.set(false), 2000); }
    catch { this.ui.toast(this.repo, 'info', 8000); }
  }
  readonly mod = navigator.platform.toLowerCase().includes('mac') ? '⌘' : 'Ctrl+Shift+';
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  readonly tiles: [string, string][] = [
    ['Connect', 'SSH2, Telnet, Serial, RDP, VNC, local shell; jump hosts and proxies'], ['Organise', 'folders, device types, workspaces, import from other programs'],
    ['Automate', 'logon actions, triggers, scripts, command library, alerts'], ['Transfer', 'SFTP with bookmarks and sync, SCP, ZMODEM, YMODEM, XMODEM'],
    ['Secure', 'OS keychain, password managers, Kerberos, FIDO2, master password'], ['Capture', 'logs, recordings, highlighting, device facts, screenshots'],
  ];
  readonly kq = signal('');
  /** The shortcuts by menu, filtered by the search box (command or key). */
  keyGroups() {
    const q = this.kq().trim().toLowerCase();
    const groups = menuBar(this.isMacOS).map(m => ({ title: m.title, items: m.items.filter(i => i.label && i.keys).map(i => [i.keys!, i.label!.replace(/\.\.\.$/, '')] as [string, string]) }));
    groups.push({ title: 'Also', items: [[this.mod + 'C / Ctrl+Insert', 'Copy'], [this.mod + 'V / Shift+Insert', 'Paste'], ['Enter', 'Reconnect (disconnected tab)']] });
    return groups.map(g => ({ ...g, items: g.items.filter(k => !q || k[1].toLowerCase().includes(q) || k[0].toLowerCase().includes(q)) })).filter(g => g.items.length);
  }
  /** "⌘⇧P" → ⌘ ⇧ P; "Ctrl+Shift+P" → Ctrl Shift P. */
  caps(k: string) {
    if (k.includes(' / ')) return [k];
    if (/[A-Za-z]{2,}\+/.test(k)) return k.split('+').filter(Boolean);
    return Array.from(k);
  }
  readonly isMacOS = navigator.platform.toLowerCase().includes('mac');
  /** Every menu item with a shortcut, by menu, then a few that are not in the menus. */
  readonly keys: [string, string][] = [
    ...menuBar(this.isMacOS).flatMap(m => m.items.filter(i => i.label && i.keys).map(i => [i.keys!, m.title + ' → ' + i.label!.replace(/\.\.\.$/, '')] as [string, string])),
    [this.mod + 'C / Ctrl+Insert', 'Copy (also)'], [this.mod + 'V / Shift+Insert', 'Paste (also)'], ['Enter (disconnected tab)', 'Reconnect'],
  ];
  /** Download count of all releases (GitHub API; -1 / 0 = not shown). */
  readonly downloads = signal(0);
  constructor() {
    this.backend.getAppInfo().then(i => this.info.set(i)).catch(() => {});
    this.backend.downloadCount().then(n => this.downloads.set(n)).catch(() => {});
  }
}
