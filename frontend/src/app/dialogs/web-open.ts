import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, WebServerState, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';
import { Autofocus } from '../core/autofocus';

const store = {
  get(k: string) { try { return localStorage.getItem(k) ?? ''; } catch { return ''; } },
  set(k: string, v: string) { try { localStorage.setItem(k, v); } catch { /* ignore */ } },
};

/**
 * Open Web Page: a browser window of its own whose traffic goes through a
 * session - out from its SSH server, or to a device through its jump host -
 * with a temporary profile that is deleted when the window closes.
 */
@Component({
  selector: 'app-web-open',
  imports: [FormsModule, Icon, Autofocus],
  template: `
  @if (req(); as r) {
  <div class="modal-backdrop" (click)="ui.close()">
    <form class="modal web" (click)="$event.stopPropagation()" (ngSubmit)="open()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="globe" [size]="17" /></span>
        <span class="mh-t">{{ r.through === 'device' ? 'Open Web Interface' : 'Open Web Page Through ' + name() }}
          <span class="mh-s">{{ r.through === 'device' ? name() + ' - ' + route() : 'The page loads as if opened on ' + name() }}</span></span></div>
      <div class="modal-body">
        <div class="grid">
          <label>Address</label>
          <input name="url" class="mono" [(ngModel)]="url" appAutofocus spellcheck="false" autocomplete="off"
            placeholder="https://10.0.0.1 · http://nas.lan:8080 · http://localhost:9090" />
          @if (direct() !== null) {
            <label>Browser</label>
            <select name="br" [(ngModel)]="browser">
              @for (b of browsers(); track b.id) { <option [value]="b.id">{{ b.name }}</option> }
              @empty { <option value="">No Chrome, Edge, Chromium, Brave or Firefox found</option> }
            </select>
          }
        </div>
        <div class="route">
          <app-icon name="route" [size]="15" />
          <div>
            @if (direct()) {
              <b>Directly from this computer</b>, the way its SSH session connects - a new {{ browserName() }} window with a temporary profile, so a system or company web proxy (which cannot reach a lab or management address) does not get in the way.
            } @else {
              <b>Through {{ route() }}</b> - a new {{ browserName() }} window with a temporary profile sends all its traffic (and name lookups) through this SSH connection;
              the web server only sees a connection from {{ exitHost() }}.
            }
          </div>
        </div>
        @if (r.through === 'device' && ws() !== undefined) {
          <div class="wsrv">
            <app-icon name="globe" [size]="15" />
            <div class="wst">
              <div><b>Web server on {{ name() }}</b>
                @if (ws() === null) { <span class="muted"> - checking…</span> }
                @else { <span class="wchip" [class.on]="ws()!.http">HTTP {{ ws()!.http ? 'on' : 'off' }}</span><span class="wchip" [class.on]="ws()!.https">HTTPS {{ ws()!.https ? 'on' : 'off' }}</span> }</div>
              @if (wsErr()) { <div class="err small">{{ wsErr() }}</div> }
              @else if (ws() && (!ws()!.http || !ws()!.https)) { <div class="muted small">Termwright can turn both on with the device's own commands and save its configuration - it shows them first.</div> }
            </div>
            @if (ws() && (!ws()!.http || !ws()!.https)) { <button type="button" (click)="enableWeb()" [disabled]="wsBusy()">{{ wsBusy() ? 'Turning on…' : 'Turn On HTTP and HTTPS…' }}</button> }
            @else if (ws() || wsErr()) { <button type="button" class="sm" (click)="checkWeb()" [disabled]="wsBusy()">Check Again</button> }
          </div>
        }
        @if (direct() !== null) {
          <p class="hint">Closing the window ends the tunnel and deletes the profile - no cookies or logins are kept. A device with a self-signed certificate shows the browser's warning; continue there if you trust it.</p>
        }
        @if (error()) { <p class="err">{{ error() }}</p> }
      </div>
      <div class="modal-foot">
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="submit" class="primary" [disabled]="busy() || !url.trim() || (direct() === false && !browsers().length)">{{ busy() ? 'Opening…' : 'Open' }}</button>
      </div>
    </form>
  </div>
  }`,
  styles: [`
    .web { width: min(600px, 94vw); }
    .route { display: flex; gap: 10px; align-items: flex-start; margin-top: 14px; padding: 10px 12px; border-radius: 8px; line-height: 1.45;
      font-size: calc(12.5px * var(--ui-scale, 1)); border: 1px solid var(--border); background: color-mix(in srgb, var(--accent) 7%, transparent);
      app-icon { color: var(--accent); margin-top: 2px; flex: none; } }
    .hint { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); margin: 10px 0 0; line-height: 1.45; }
    .err { color: var(--danger); margin: 10px 0 0; }
    .wsrv { display: flex; gap: 10px; align-items: center; margin-top: 10px; padding: 10px 12px; border-radius: 8px; border: 1px solid var(--border); font-size: calc(12.5px * var(--ui-scale, 1));
      > app-icon { color: var(--accent); flex: none; align-self: flex-start; margin-top: 2px; } .wst { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; line-height: 1.45; }
      .err { margin: 0; } .small { font-size: calc(11.5px * var(--ui-scale, 1)); } button { flex: none; } }
    .wchip { display: inline-block; margin-left: 8px; padding: 0 7px; border-radius: 5px; font-size: calc(11px * var(--ui-scale, 1)); background: color-mix(in srgb, var(--danger) 14%, transparent); color: var(--danger);
      &.on { background: color-mix(in srgb, var(--success, #1fa45a) 16%, transparent); color: var(--success, #1fa45a); } }
    .muted { color: var(--muted); }
  `],
})
export class WebOpenDialog {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  readonly req = this.ui.webOpen;
  readonly browsers = signal<{ id: string; name: string }[]>([]);
  readonly hop = signal('');
  readonly busy = signal(false);
  readonly error = signal('');
  url = '';
  browser = store.get('termwright.webBrowser');
  /** The device's web server: undefined = its device type cannot say, null = checking. */
  readonly ws = signal<WebServerState | null | undefined>(undefined);
  readonly wsErr = signal('');
  readonly wsBusy = signal(false);
  async checkWeb() {
    const r = this.req(); if (!r) return;
    this.ws.set(null); this.wsErr.set('');
    try { const st = await this.backend.webServerCheck(r.session); this.ws.set(st?.known ? st : undefined); this.fitScheme(); }
    catch (e) { this.ws.set({ known: true, http: false, https: false }); this.wsErr.set('Could not read it: ' + errText(e)); }
  }
  /** Turns HTTP and HTTPS on (the device's commands, then its save command), after asking. */
  async enableWeb() {
    const r = this.req(), st = this.ws(); if (!r || !st) return;
    const cmds = [...(st.on ?? []), ...(st.save ?? [])];
    if (!(await this.ui.confirm('Turn on the web server?', `Termwright logs in to ${this.name()} and runs:\n\n${cmds.join('\n')}\n\nThis changes the device's configuration and saves it.`, 'Turn On and Save'))) return;
    this.wsBusy.set(true); this.wsErr.set('');
    try { const s2 = await this.backend.webServerEnable(r.session, true); this.ws.set(s2?.known ? s2 : st); this.fitScheme(); }
    catch (e) { this.wsErr.set(errText(e)); }
    finally { this.wsBusy.set(false); }
  }
  /** The address follows what the device serves: https when HTTPS is on, else http. */
  private fitScheme() {
    const st = this.ws(); if (!st || !this.url) return;
    if (st.https && this.url.startsWith('http://')) this.url = 'https://' + this.url.slice(7);
    else if (!st.https && st.http && this.url.startsWith('https://')) this.url = 'http://' + this.url.slice(8);
  }

  readonly name = computed(() => this.req()?.session.name || this.req()?.session.hostname || 'this session');
  /** true: no proxy needed (device reached directly); false: through a tunnel; null: not known yet. */
  readonly direct = computed(() => {
    const r = this.req();
    if (!r) return null;
    if (r.through === 'session') return false;
    const s = r.session;
    return !s.jumpSession && !this.hop() && !s.proxyType;
  });
  readonly route = computed(() => {
    const r = this.req();
    if (!r) return '';
    const s = r.session;
    if (r.through === 'session') return name(s);
    if (s.jumpSession) return 'the jump host ' + s.jumpSession.split('/').pop();
    if (this.hop()) return 'the jump station ' + s.hostname;
    if (s.proxyType) return 'the ' + s.proxyType + ' proxy ' + s.proxyHost;
    return 'directly';
  });
  readonly exitHost = computed(() => {
    const r = this.req();
    if (!r) return '';
    const s = r.session;
    if (r.through === 'session' || this.hop()) return s.hostname;
    if (s.jumpSession) return s.jumpSession.split('/').pop()!;
    return 'the proxy';
  });
  browserName() { return this.browsers().find(b => b.id === this.browser)?.name ?? 'browser'; }

  constructor() {
    const r = this.req();
    this.ui.watchWebBypass();
    if (!r) return;
    if (r.through === 'device' && r.session.deviceType) void this.checkWeb();
    const key = this.urlKey();
    void this.backend.webBrowsers().then(l => {
      this.browsers.set(l ?? []);
      if (!l?.some(b => b.id === this.browser)) this.browser = l?.[0]?.id ?? '';
    }).catch(() => {});
    void this.backend.sftpHopTarget(r.session).then(h => {
      this.hop.set(h || '');
      if (!this.url) this.url = store.get(key) || (r.through === 'device' ? 'https://' + hostOf(h || r.session.hostname) : '');
      this.fitScheme();
    }).catch(() => { if (!this.url) this.url = store.get(key); });
  }

  private urlKey() { const r = this.req()!; return 'termwright.webUrl:' + (r.session.path || r.session.hostname) + ':' + r.through; }

  async open() {
    const r = this.req();
    if (!r) return;
    let u = this.url.trim();
    if (u && !/^https?:\/\//i.test(u)) u = (/^[\w.-]+:(80|8080|8000)\b/.test(u) ? 'http://' : 'https://') + u;
    this.busy.set(true);
    this.error.set('');
    try {
      const res = await this.backend.openWebPage(r.tabId, r.session, r.through, u, this.browser);
      const opened = res.url || u;
      store.set(this.urlKey(), opened);
      if (this.browser) store.set('termwright.webBrowser', this.browser);
      this.ui.close();
      this.ui.toast((res.via ? `Opened ${opened} in ${res.browser} through ${res.via} - close that window to end the tunnel.` : `Opened ${opened} in ${res.browser}.`) + (res.note ? ` (${res.note}.)` : ''), 'success', res.note ? 10000 : 7000);
    } catch (e) { this.error.set(errText(e)); }
    finally { this.busy.set(false); }
  }
}

function name(s: { name?: string; hostname: string }) { return s.name || s.hostname; }
function hostOf(h: string) { h = h.slice(h.lastIndexOf('@') + 1); return h.includes(':') && !h.startsWith('[') ? '[' + h + ']' : h; }
