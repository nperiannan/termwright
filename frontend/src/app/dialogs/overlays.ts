import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Autofocus } from '../core/autofocus';
import { Backend } from '../core/backend';
import { AppInfo } from '../core/models';
import { Ui } from '../core/ui.service';

/** Confirm / input dialogs, context menu and toasts. */
@Component({
  selector: 'app-overlays',
  imports: [FormsModule, Autofocus],
  template: `
    @if (ui.confirmReq(); as c) {
      <div class="modal-backdrop">
        <form class="modal small" (ngSubmit)="closeConfirm(true)">
          <div class="modal-head">{{ c.title }}</div>
          <div class="modal-body"><p class="pre">{{ c.message }}</p></div>
          <div class="modal-foot"><span class="spacer"></span>
            <button type="button" (click)="closeConfirm(false)">Cancel</button>
            <button type="submit" class="primary" [class.danger]="c.danger" appAutofocus>{{ c.ok }}</button>
          </div>
        </form>
      </div>
    }
    @if (ui.inputReq(); as r) {
      <div class="modal-backdrop">
        <form class="modal small" (ngSubmit)="closeInput(true)">
          <div class="modal-head">{{ r.title }}</div>
          <div class="modal-body"><label class="field">{{ r.label }}
            <input name="v" [(ngModel)]="r.value" [type]="r.secret ? 'password' : 'text'" [attr.autocomplete]="r.secret ? 'off' : null" appAutofocus /></label></div>
          <div class="modal-foot"><span class="spacer"></span>
            <button type="button" (click)="closeInput(false)">Cancel</button>
            <button type="submit" class="primary">OK</button>
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
  styles: [`.field { display: flex; flex-direction: column; gap: 6px; } .pre { white-space: pre-wrap; margin: 0; }`],
})
export class Overlays {
  protected ui = inject(Ui);
  closeConfirm(v: boolean) {
    const c = this.ui.confirmReq();
    this.ui.confirmReq.set(null);
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

/** About and keyboard shortcuts. */
@Component({
  selector: 'app-about',
  template: `
  <div class="modal-backdrop" (click)="ui.close()">
    <div class="modal" (click)="$event.stopPropagation()">
      @if (ui.dialog() === 'about') {
        <div class="modal-head">About Termwright</div>
        <div class="modal-body about">
          <div class="about-head"><img src="icon.svg" alt="" width="64" height="64" />
            <h2>Termwright <small>{{ info()?.version }}</small></h2></div>
          <p>A free, open-source terminal emulator, session manager and file-transfer client for network and
            system engineers - one app for Windows, Linux and macOS.</p>
          <ul class="features">
            <li><b>Connect</b> - SSH2, Telnet, Serial (RTS/CTS, XON/XOFF), local shell and raw TCP; jump hosts, SOCKS/HTTP proxies, port forwarding, X11 and agent forwarding</li>
            <li><b>Organise</b> - device types (login profiles), session manager with folders and folder defaults, workspaces, Quick Open, tabs and tiled layouts; imports sessions from other terminal programs, PuTTY, MobaXterm and OpenSSH config</li>
            <li><b>Automate</b> - logon actions (expect/send), triggers, Python, VBScript and JavaScript scripting (with the common <code>crt</code> object), recorder, command window and command library, output alerts, compare outputs, button bar, key map</li>
            <li><b>Transfer</b> - SFTP browser with bookmarks, sync, resume and server-to-server copy, through jump stations and as root or another user; SCP, ZMODEM, YMODEM, XMODEM</li>
            <li><b>Secure</b> - OS keychain passwords, password managers (1Password, Bitwarden, HashiCorp Vault), Kerberos, SSH certificates and FIDO2 keys, master password, encrypted vault export, key login setup, known-hosts checking</li>
            <li><b>Capture</b> - session logging with timestamps, keyword highlighting, themes and color schemes</li>
          </ul>
          <p>Created by <b>Natarajan Periannan</b>.</p>
          <div class="share">
            <span>Open source on GitHub - share it with your team:</span>
            <a class="mono" (click)="backend.openURL(repo)">{{ repo }}</a>
            <button type="button" (click)="copyRepo()">{{ copied() ? 'Copied' : 'Copy Link' }}</button>
          </div>
          <p class="links">
            <a (click)="backend.openURL(repo + '/blob/main/docs/USER_GUIDE.md')">User Guide</a> ·
            <a (click)="backend.openURL(repo + '/issues')">Report an issue</a> ·
            <a (click)="backend.openURL(repo + '/releases')">Releases</a>
          </p>
          <p class="hint">{{ info()?.os }}/{{ info()?.arch }} · data folder: <span class="mono">{{ info()?.dataDir }}</span><br />
            Licensed under the Apache License 2.0 · built with Go, Wails, Angular and xterm.js.</p>
        </div>
      } @else {
        <div class="modal-head">Keyboard Shortcuts</div>
        <div class="modal-body">
          <table class="list keys">
            @for (k of keys; track k[0]) { <tr><td class="mono">{{ k[0] }}</td><td>{{ k[1] }}</td></tr> }
          </table>
        </div>
      }
      <div class="modal-foot"><span class="spacer"></span><button class="primary" (click)="ui.close()">Close</button></div>
    </div>
  </div>`,
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
  readonly keys: [string, string][] = [
    [this.mod + 'O', 'Connect (focus Session Manager)'], [this.mod + 'Q', 'Quick Connect'], [this.mod + 'L', 'Local shell'],
    [this.mod + 'R', 'Reconnect'], [this.mod + 'W', 'Close tab'], [this.mod + 'C / Ctrl+Insert', 'Copy'],
    [this.mod + 'V / Shift+Insert', 'Paste'], [this.mod + 'F', 'Find in scrollback'], [this.mod + 'S', 'Save scrollback to file'],
    [this.mod + 'M', 'Toggle Session Manager'], [this.mod + 'K', 'Toggle Command Window'], [this.mod + 'B', 'Toggle Button Bar'],
    ['Ctrl+Tab', 'Next tab'], ['Ctrl+= / Ctrl+- / Ctrl+0', 'Zoom in / out / reset'], ['Enter (disconnected tab)', 'Reconnect'],
  ];
  constructor() { this.backend.getAppInfo().then(i => this.info.set(i)).catch(() => {}); }
}
