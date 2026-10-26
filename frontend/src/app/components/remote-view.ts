import { Component, ElementRef, OnDestroy, effect, inject, input, signal, untracked, viewChild } from '@angular/core';
import { Backend, errText } from '../core/backend';
import { Tabs, TabView } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { SessionIcon } from './session-icon';

/** RDP tab (the session runs in the computer's Remote Desktop client) or VNC tab (built-in viewer). */
@Component({
  selector: 'app-remote-view',
  imports: [SessionIcon],
  template: `
    @if (tab().kind === 'rdp') {
      <div class="rdp">
        <div class="card">
          <div class="head"><app-session-icon kind="rdp" [size]="28" /><div><div class="t">{{ tab().title }}</div>
            <div class="sub">{{ tab().state === 'disconnected' ? (tab().message || 'Closed') : 'Opened in ' + (r()?.client || 'the Remote Desktop client') }}</div></div></div>
          <div class="rows">
            <div><span>Computer</span><b class="mono">{{ target() }}</b></div>
            @if (r()?.via) {
              <div><span>Route</span><b>{{ r()!.via }} - the client connects to <span class="mono">{{ r()!.addr }}</span> on this computer</b></div>
              <div><span>Tunnel</span><b>{{ conns() === 1 ? '1 connection' : conns() + ' connections' }} open</b></div>
            }
            @if (r()?.session?.username) { <div><span>User</span><b>{{ user() }}</b></div> }
          </div>
          @if (r()?.note) { <p class="note">{{ r()!.note }}</p> }
          @if (r()?.via) { <p class="hint">Keep this tab open while you work: closing it closes the tunnel.</p> }
          <div class="acts">
            <button type="button" class="primary" (click)="reopen()">{{ tab().state === 'disconnected' ? 'Open Again' : 'Open Remote Desktop Again' }}</button>
            @if (r()?.hasPass) { <button type="button" (click)="copyPw()">Copy Password</button> }
            @if (tab().session.path) { <button type="button" (click)="ui.editSession(tab().session.path)">Edit Session</button> }
            <button type="button" (click)="tabs.close(tab().id)">Close</button>
          </div>
        </div>
      </div>
    } @else {
      <div class="vnc">
        <div class="bar">
          <span class="dot" [class]="tab().state"></span>
          <span class="st">{{ statusText() }}</span>
          <span class="spacer"></span>
          <button type="button" (click)="send({ type: 'ctrlaltdel' })" [disabled]="tab().state !== 'connected'" title="Send Ctrl+Alt+Del">Ctrl+Alt+Del</button>
          <button type="button" (click)="pasteClipboard()" [disabled]="tab().state !== 'connected'" title="Send this computer's clipboard to the remote clipboard">Send Clipboard</button>
          <button type="button" [class.on]="viewOnly()" (click)="toggleView()" title="Watch only: keyboard and mouse are not sent">View Only</button>
          <button type="button" [class.on]="scale()" (click)="toggleScale()" title="Fit the remote screen into the tab">Fit</button>
          <button type="button" (click)="tabs.reconnect(tab().id)">Reconnect</button>
        </div>
        <iframe #frame src="vnc.html" title="VNC" (load)="loaded()"></iframe>
      </div>
    }`,
  styles: [`
    :host { display: block; position: absolute; inset: 0; background: var(--bg); }
    .rdp { height: 100%; display: flex; align-items: center; justify-content: center; padding: 20px; }
    .card { width: min(560px, 100%); padding: 20px 22px; border: 1px solid var(--border); border-radius: 10px; background: var(--panel); }
    .head { display: flex; gap: 12px; align-items: center; margin-bottom: 14px; .t { font-weight: 600; font-size: 15px; } .sub { color: var(--muted); font-size: 12.5px; } }
    .rows { display: grid; gap: 6px; font-size: 13px; > div { display: grid; grid-template-columns: 90px 1fr; gap: 10px; } span { color: var(--muted); } b { font-weight: 500; } }
    .mono { font-family: var(--mono); }
    .note { margin: 12px 0 0; padding: 8px 10px; border-radius: 6px; background: color-mix(in srgb, var(--accent) 12%, transparent); font-size: 12.5px; }
    .hint { color: var(--muted); font-size: 12px; margin: 10px 0 0; }
    .acts { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 16px; }
    .vnc { height: 100%; display: flex; flex-direction: column; }
    .bar { display: flex; align-items: center; gap: 6px; padding: 4px 8px; border-bottom: 1px solid var(--border); background: var(--panel); font-size: 12px;
      button { height: 24px; font-size: 12px; padding: 0 8px; } button.on { border-color: var(--accent); color: var(--accent); } .spacer { flex: 1; } .st { color: var(--muted); } }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--border); &.connected { background: var(--ok); } &.error { background: var(--danger); } &.connecting { background: #f59e0b; } }
    iframe { flex: 1; border: 0; width: 100%; background: #111; }
  `],
})
export class RemoteView implements OnDestroy {
  readonly tab = input.required<TabView>();
  protected ui = inject(Ui);
  protected tabs = inject(Tabs);
  private backend = inject(Backend);
  private frame = viewChild<ElementRef<HTMLIFrameElement>>('frame');
  readonly conns = signal(0);
  readonly viewOnly = signal(false);
  readonly scale = signal(true);
  readonly desktop = signal('');
  private ready = false;
  private timer?: ReturnType<typeof setInterval>;
  private lastRemote: unknown = null;
  private onMsg = (e: MessageEvent) => this.message(e);

  r() { return this.tab().remote; }
  target() { const s = this.tab().session; return `${s.hostname}:${s.port || 3389}`; }
  user() { const s = this.tab().session; return (s.rdpDomain ? s.rdpDomain + '\\' : '') + s.username; }

  statusText() {
    const t = this.tab();
    const where = (this.desktop() || t.session.hostname) + (t.remote?.via ? ' ' + t.remote.via : '');
    switch (t.state) {
      case 'connected': return where;
      case 'connecting': return 'Connecting to ' + where + '...';
      default: return t.message || 'Disconnected';
    }
  }

  constructor() {
    window.addEventListener('message', this.onMsg);
    effect(() => {
      const t = this.tab();
      if (t.kind === 'rdp' && t.remote?.via && !this.timer) {
        const poll = () => this.backend.remoteStatus(t.id).then(n => this.conns.set(n)).catch(() => {});
        poll();
        this.timer = setInterval(poll, 4000);
      }
      // VNC: (re)connect when the tab gets a new remote object (Reconnect)
      if (t.kind === 'vnc' && t.remote !== this.lastRemote && this.ready) untracked(() => this.connect());
    });
  }

  ngOnDestroy() {
    window.removeEventListener('message', this.onMsg);
    clearInterval(this.timer);
  }

  send(m: Record<string, unknown>) {
    this.frame()?.nativeElement.contentWindow?.postMessage({ target: 'tw-vnc', ...m }, '*');
  }

  loaded() { /* the viewer posts "ready" when its module has loaded */ }

  private connect() {
    const r = this.tab().remote;
    if (!r?.wsUrl) return;
    if (!this.lastRemote) this.viewOnly.set(!!this.tab().session.vncViewOnly);
    this.lastRemote = r;
    this.tabs.patch(this.tab().id, { state: 'connecting', message: '' });
    this.send({ type: 'connect', url: r.wsUrl, viewOnly: this.viewOnly(), scale: this.scale() });
  }

  private async message(e: MessageEvent) {
    const f = this.frame()?.nativeElement;
    if (!f || e.source !== f.contentWindow || e.data?.source !== 'tw-vnc') return;
    const m = e.data;
    const id = this.tab().id;
    switch (m.type) {
      case 'ready': this.ready = true; this.connect(); break;
      case 'status':
        this.tabs.patch(id, { state: m.state, message: m.message ?? '' });
        if (m.state === 'connected') this.send({ type: 'focus' });
        break;
      case 'name': this.desktop.set(m.name); break;
      case 'clipboard': if (m.text) void this.backend.clipboardSet(m.text); break;
      case 'credentials': {
        const types: string[] = m.types ?? [];
        let c = await this.backend.remoteCredentials(id).catch(() => ({ username: '', password: '' }));
        if (types.includes('username') && !c.username) {
          const u = await this.ui.input('VNC login', 'User name (for macOS Screen Sharing: your Mac account)');
          if (u === null) { this.send({ type: 'disconnect' }); return; }
          c = { ...c, username: u };
        }
        if (types.includes('password') && !c.password) {
          const p = await this.ui.input('VNC login', 'Password for ' + (c.username ? c.username + '@' : '') + this.tab().session.hostname, '', true);
          if (p === null) { this.send({ type: 'disconnect' }); return; }
          c = { ...c, password: p };
        }
        this.send({ type: 'credentials', ...c });
        break;
      }
    }
  }

  async pasteClipboard() {
    try {
      const text = await this.backend.clipboardGet();
      this.send({ type: 'clipboard', text });
      this.ui.toast('Clipboard sent - paste it on the remote computer');
    } catch (e) { this.ui.toast(errText(e), 'error'); }
  }

  toggleView() { this.viewOnly.update(v => !v); this.send({ type: 'viewOnly', value: this.viewOnly() }); }
  toggleScale() { this.scale.update(v => !v); this.send({ type: 'scale', value: this.scale() }); }

  async reopen() {
    try {
      await this.backend.reopenRemote(this.tab().id);
      this.tabs.patch(this.tab().id, { state: 'connected', message: '' });
    } catch (e) { this.ui.error(e); }
  }

  async copyPw() {
    try { await this.backend.copyRemotePassword(this.tab().id); this.ui.toast('Password copied - it is cleared from the clipboard in 60 seconds', 'success'); }
    catch (e) { this.ui.error(e); }
  }
}
