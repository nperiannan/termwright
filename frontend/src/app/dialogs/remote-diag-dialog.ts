import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';

/**
 * Collect Remote Desktop Logs: a report for finding out why an RDP or VNC
 * session does not connect - a connection test from this computer and the
 * state and logs of the service on the machine, read over SSH. Read-only; the
 * sudo password (only when sudo asks for one) is sent to the command and never shown.
 */
@Component({
  selector: 'app-remote-diag',
  imports: [FormsModule, Icon],
  template: `
  @if (req(); as r) {
  <div class="modal-backdrop" (click)="close()">
    <form class="modal diag" (click)="$event.stopPropagation()" (ngSubmit)="run()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="search" [size]="17" /></span>
        <span class="mh-t">Collect Remote Desktop Logs
          <span class="mh-s">{{ r.name }} · {{ r.protocol === 'SSH2' ? 'RDP and VNC services of this machine' : r.protocol }}</span></span></div>
      <div class="modal-body">
        @if (!report() && !busy()) {
          <p class="lead">Finds out why this desktop does not connect. It
            @if (r.protocol !== 'SSH2') { tries the connection from this computer (the first bytes of the {{ r.protocol }} handshake) and }
            reads the state and the logs of the {{ r.protocol === 'SSH2' ? 'RDP and VNC services' : r.protocol + ' service' }} on the machine over SSH.
            Nothing is changed and no password is in the report.</p>
          <label class="sudo">sudo password
            <input type="password" name="sudo" [(ngModel)]="sudo" autocomplete="off" placeholder="only if sudo asks for a password (leave empty otherwise)" />
          </label>
          <p class="hint">Some logs (the services' journal, the VNC and RDP logs) can only be read with sudo. Without it the report lists the commands to run on the machine.</p>
        }
        @if (busy()) { <p class="lead busy"><span class="spin"></span> Collecting - connecting, testing and reading the logs (up to a minute)…</p> }
        @if (error()) { <p class="err">{{ error() }}</p> }
        @if (report()) { <pre class="report" translate="no">{{ report() }}</pre> }
        @if (report() && !busy()) {
          <div class="fix" [class.hot]="blocked()">
            <div class="fh">Fix</div>
            @if (blocked()) {
              <p class="hint now">The report shows the VNC server blocking this address after wrong passwords. Restarting the VNC service clears the block.</p>
            } @else {
              <p class="hint now">Restarting a service repairs one that hangs (and clears a VNC block after wrong passwords). Anyone connected to that desktop is disconnected.</p>
            }
            <div class="fixrow">
              <input type="password" name="fsudo" [(ngModel)]="fixSudo" autocomplete="off" placeholder="sudo password (only if sudo asks for one)" />
              @for (w of fixes(); track w) {
                <button type="button" [class.primary]="w === 'VNC' && blocked()" [disabled]="fixing()" (click)="restart(w)">{{ fixing() === w ? 'Restarting…' : 'Restart ' + w + ' service' }}</button>
              }
            </div>
          </div>
        }
      </div>
      <div class="modal-foot">
        @if (report()) {
          <button type="button" (click)="copy()">{{ copied() ? 'Copied' : 'Copy' }}</button>
          <button type="button" (click)="save()">Save…</button>
        }
        <span class="spacer"></span>
        @if (report() || error()) { <button type="button" (click)="again()">Collect Again</button> }
        <button type="button" (click)="close()">{{ report() ? 'Close' : 'Cancel' }}</button>
        @if (!report()) { <button type="submit" class="primary" [disabled]="busy()">{{ busy() ? 'Collecting…' : 'Collect' }}</button> }
      </div>
    </form>
  </div>
  }`,
  styles: [`
    .diag { width: min(820px, 96vw); }
    .lead { margin: 0 0 12px; line-height: 1.5; }
    .hint { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); margin: 8px 0 0; line-height: 1.45; }
    .sudo { display: block; }
    .sudo input { display: block; width: 100%; margin-top: 6px; }
    .err { color: var(--danger); margin: 10px 0 0; white-space: pre-wrap; }
    .report { margin: 0; max-height: min(56vh, 560px); overflow: auto; padding: 10px 12px; border-radius: 8px; border: 1px solid var(--border);
      background: color-mix(in srgb, var(--fg) 4%, transparent); font-family: var(--mono, ui-monospace, monospace); font-size: calc(11.5px * var(--ui-scale, 1)); line-height: 1.4; white-space: pre-wrap; word-break: break-word; user-select: text; }
    .fix { margin-top: 12px; padding: 10px 12px; border-radius: 8px; border: 1px solid var(--border); }
    .fix.hot { border-color: var(--accent); }
    .fh { font-weight: 600; margin-bottom: 4px; }
    .fix .hint.now { margin: 0 0 8px; }
    .fixrow { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; input { flex: 1 1 220px; min-width: 0; } }
    .busy { display: flex; gap: 10px; align-items: center; }
    .spin { width: 14px; height: 14px; border-radius: 50%; border: 2px solid var(--border); border-top-color: var(--accent); animation: sp .8s linear infinite; }
    @keyframes sp { to { transform: rotate(360deg); } }
  `],
})
export class RemoteDiagDialog {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  readonly req = this.ui.remoteDiag;
  sudo = '';
  readonly busy = signal(false);
  readonly report = signal('');
  readonly error = signal('');
  readonly copied = signal(false);
  fixSudo = '';
  readonly fixing = signal<'RDP' | 'VNC' | ''>('');
  readonly fixes = (): ('RDP' | 'VNC')[] => {
    const p = this.req()?.protocol;
    return p === 'RDP' ? ['RDP'] : p === 'VNC' ? ['VNC'] : ['RDP', 'VNC'];
  };
  readonly blocked = () => this.report().includes('blocked this address');

  close() { this.sudo = ''; this.fixSudo = ''; this.ui.close(); }

  async run() {
    const r = this.req();
    if (!r || this.busy()) return;
    this.busy.set(true);
    this.error.set('');
    try {
      this.report.set(await this.backend.collectRemoteDesktopLogs(r.path, this.sudo));
    } catch (e) { this.error.set(errText(e)); }
    this.sudo = '';
    this.busy.set(false);
  }

  /** Restarts the service, then collects the report again so the result is visible. */
  async restart(which: 'RDP' | 'VNC') {
    const r = this.req();
    if (!r || this.fixing()) return;
    const ok = await this.ui.confirm('Restart ' + which + ' service',
      `Restart the ${which === 'VNC' ? 'TigerVNC' : 'xrdp'} service on the machine? Anyone connected to its ${which} desktop is disconnected and their open programs in that desktop session may be lost.`, 'Restart', true);
    if (!ok) return;
    this.fixing.set(which);
    this.error.set('');
    const sudo = this.fixSudo;
    try {
      const done = await this.backend.restartRemoteDesktopService(r.path, which, sudo);
      let fresh = '';
      try { fresh = await this.backend.collectRemoteDesktopLogs(r.path, sudo); } catch (e) { fresh = errText(e); }
      this.report.set('== Restart ' + which + ' service ==\n' + done.trimEnd() + '\n\n' + fresh);
      this.ui.toast(which + ' service restarted - the report below is new', 'success');
    } catch (e) { this.error.set(errText(e)); }
    this.fixSudo = '';
    this.fixing.set('');
  }

  again() { this.report.set(''); this.error.set(''); }

  async copy() {
    await this.backend.clipboardSet(this.report()).catch(() => {});
    this.copied.set(true);
    setTimeout(() => this.copied.set(false), 1500);
  }

  async save() {
    const r = this.req();
    const d = new Date().toISOString().slice(0, 16).replace(/[-:T]/g, '');
    const name = `${(r?.name ?? 'remote-desktop').replace(/[\\/:*?"<>|]/g, '-')} logs ${d}.txt`;
    try {
      const p = await this.backend.saveTextAs('Save Remote Desktop Logs', name, this.report());
      if (p) this.ui.toast('Saved ' + p, 'success');
    } catch (e) { this.ui.error(e); }
  }
}
