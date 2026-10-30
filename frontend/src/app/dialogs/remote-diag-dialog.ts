import { Component, effect, inject, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';

interface Finding { level: 'error' | 'warn' | 'info' | 'ok'; text: string; detail: string }
interface DiagResult { findings: Finding[]; checks: Finding[]; fix: '' | 'RDP' | 'VNC'; report: string }

/**
 * Diagnose Remote Desktop: finds out why an RDP or VNC session does not
 * connect and says so in a few lines - a connection test from this computer
 * and the state of the service on the machine, read over SSH - with the full
 * report behind it as the evidence. Nothing is changed; the Fix box restarts a
 * service after asking. The sudo password (only when sudo asks for one) is sent
 * to the command and never shown.
 */
@Component({
  selector: 'app-remote-diag',
  imports: [FormsModule, Icon],
  template: `
  @if (req(); as r) {
  <div class="modal-backdrop" (click)="close()">
    <form class="modal diag" (click)="$event.stopPropagation()" (ngSubmit)="run()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="search" [size]="17" /></span>
        <span class="mh-t">Diagnose Remote Desktop
          <span class="mh-s">{{ r.name }} · {{ r.protocol === 'SSH2' ? 'RDP and VNC services of this machine' : r.protocol }}</span></span></div>
      <div class="modal-body">
        @if (busy()) { <p class="lead busy"><span class="spin"></span> {{ fixing() ? 'Restarting the service and checking again…' : 'Diagnosing - testing the connection and checking the machine (up to a minute)…' }}</p> }
        @if (error()) { <p class="err">{{ error() }}</p> }
        @if (res(); as d) {
          @if (!busy()) {
            <div class="finds">
              @for (f of d.findings; track $index) {
                <div class="find" [class]="f.level">
                  <span class="dot">{{ f.level === 'ok' ? '✓' : f.level === 'info' ? 'i' : '!' }}</span>
                  <div><div class="ft">{{ f.text }}</div>@if (f.detail) { <div class="fd">{{ f.detail }}</div> }</div>
                </div>
              }
            </div>
            <div class="fix" [class.hot]="d.fix">
              <div class="fh">Restart a service</div>
              <p class="hint now">{{ d.fix ? 'A restart should fix this. Anyone connected to that desktop is disconnected.' : 'Repairs a service that hangs (and clears a VNC block after wrong passwords). Anyone connected to that desktop is disconnected.' }}</p>
              <div class="fixrow">
                <input type="password" name="sudo" [(ngModel)]="sudo" autocomplete="off" placeholder="sudo password (only if sudo asks for one)" />
                @for (w of fixes(); track w) {
                  <button type="button" [class.primary]="w === d.fix" [disabled]="busy()" (click)="restart(w)">Restart {{ w }} service</button>
                }
              </div>
            </div>
            <button type="button" class="link" (click)="details.set(!details())">{{ details() ? 'Hide details' : 'Show details (what was checked, and the full report)' }}</button>
          }
          @if (details() && !busy()) {
            <div class="checks">
              @for (c of d.checks; track $index) {
                <div class="chk" [class]="c.level"><span class="ci">{{ c.level === 'ok' ? '✓' : c.level === 'info' ? 'i' : '✗' }}</span><span>{{ c.text }}</span></div>
              }
            </div>
            <pre class="report" translate="no">{{ d.report }}</pre>
          }
        }
      </div>
      <div class="modal-foot">
        @if (res() && !busy()) {
          <button type="button" (click)="copy()">{{ copied() ? 'Copied' : 'Copy Report' }}</button>
          <button type="button" (click)="save()">Save Report…</button>
        }
        <span class="spacer"></span>
        <button type="button" (click)="close()">Close</button>
        <button type="submit" class="primary" [disabled]="busy()">{{ res() || error() ? 'Diagnose Again' : 'Diagnose' }}</button>
      </div>
    </form>
  </div>
  }`,
  styles: [`
    .diag { width: min(780px, 96vw); }
    .lead { margin: 0 0 12px; line-height: 1.5; }
    .hint { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); margin: 8px 0 0; line-height: 1.45; }
    .err { color: var(--danger); margin: 10px 0 0; white-space: pre-wrap; }
    .finds { display: flex; flex-direction: column; gap: 8px; }
    .find { display: flex; gap: 10px; align-items: flex-start; padding: 10px 12px; border-radius: 8px; border: 1px solid var(--border); }
    .find .dot { flex: none; width: 22px; height: 22px; border-radius: 50%; display: grid; place-items: center; font-weight: 700; font-size: calc(12px * var(--ui-scale, 1)); color: #fff; background: var(--muted); }
    .find.error { border-color: var(--danger); background: color-mix(in srgb, var(--danger) 9%, transparent); } .find.error .dot { background: var(--danger); }
    .find.warn { border-color: var(--warn, #d9a441); background: color-mix(in srgb, var(--warn, #d9a441) 9%, transparent); } .find.warn .dot { background: var(--warn, #d9a441); }
    .find.ok { border-color: var(--ok, #3fb68b); background: color-mix(in srgb, var(--ok, #3fb68b) 9%, transparent); } .find.ok .dot { background: var(--ok, #3fb68b); }
    .find:first-child .ft { font-size: calc(14px * var(--ui-scale, 1)); }
    .ft { font-weight: 600; line-height: 1.4; user-select: text; }
    .fd { color: var(--muted); margin-top: 3px; line-height: 1.45; font-size: calc(12.5px * var(--ui-scale, 1)); user-select: text; }
    .find.error .ft { color: var(--danger); }
    .checks { margin: 10px 0 0; display: flex; flex-direction: column; gap: 3px; }
    .chk { display: flex; gap: 8px; align-items: baseline; font-size: calc(12.5px * var(--ui-scale, 1)); user-select: text; }
    .chk .ci { flex: none; width: 14px; text-align: center; font-weight: 700; }
    .chk.ok { color: var(--ok, #3fb68b); } .chk.error { color: var(--danger); } .chk.warn { color: var(--warn, #d9a441); } .chk.info { color: var(--muted); }
    .report { margin: 10px 0 0; max-height: min(36vh, 380px); overflow: auto; padding: 10px 12px; border-radius: 8px; border: 1px solid var(--border);
      background: color-mix(in srgb, var(--fg) 4%, transparent); font-family: var(--mono, ui-monospace, monospace); font-size: calc(11.5px * var(--ui-scale, 1)); line-height: 1.4; white-space: pre-wrap; word-break: break-word; user-select: text; }
    .fix { margin: 12px 0 8px; padding: 10px 12px; border-radius: 8px; border: 1px solid var(--border); }
    .fix.hot { border-color: var(--accent); }
    .fh { font-weight: 600; margin-bottom: 4px; }
    .fix .hint.now { margin: 0 0 8px; }
    .fixrow { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; input { flex: 1 1 220px; min-width: 0; } }
    .link { background: none; border: 0; padding: 4px 0; color: var(--accent); cursor: pointer; font: inherit; }
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
  readonly res = signal<DiagResult | null>(null);
  readonly error = signal('');
  readonly copied = signal(false);
  readonly details = signal(false);
  readonly fixing = signal<'RDP' | 'VNC' | ''>('');
  readonly fixes = (): ('RDP' | 'VNC')[] => {
    const p = this.req()?.protocol;
    return p === 'RDP' ? ['RDP'] : p === 'VNC' ? ['VNC'] : ['RDP', 'VNC'];
  };

  constructor() {
    // one click on the menu item runs the diagnosis; no question first
    effect(() => { if (this.req()) untracked(() => void this.run()); });
  }

  close() { this.sudo = ''; this.ui.close(); }

  async run() {
    const r = this.req();
    if (!r || this.busy()) return;
    this.busy.set(true);
    this.error.set('');
    try {
      const d = await this.backend.diagnoseRemoteDesktop(r.path, this.sudo) as DiagResult;
      this.res.set(d);
      // open the details by themselves when the diagnosis found no cause
      this.details.set(!d.findings.some(f => f.level === 'error' || f.level === 'ok'));
    } catch (e) { this.error.set(errText(e)); }
    this.busy.set(false);
  }

  /** Restarts the service, then diagnoses again so the result is visible. */
  async restart(which: 'RDP' | 'VNC') {
    const r = this.req();
    if (!r || this.busy()) return;
    const ok = await this.ui.confirm('Restart ' + which + ' service',
      `Restart the ${which === 'VNC' ? 'TigerVNC' : 'xrdp'} service on the machine? Anyone connected to its ${which} desktop is disconnected and their open programs in that desktop session may be lost.`, 'Restart', true);
    if (!ok) return;
    this.busy.set(true);
    this.fixing.set(which);
    this.error.set('');
    try {
      const done = await this.backend.restartRemoteDesktopService(r.path, which, this.sudo);
      let d: DiagResult | null = null;
      try { d = await this.backend.diagnoseRemoteDesktop(r.path, this.sudo) as DiagResult; } catch (e) { this.error.set(errText(e)); }
      if (d) {
        d.report = '== Restart ' + which + ' service ==\n' + done.trimEnd() + '\n\n' + d.report;
        d.findings = [{ level: 'info', text: which + ' service restarted.', detail: 'The result below is from a new diagnosis.' }, ...d.findings];
        this.res.set(d);
        this.details.set(false);
      }
    } catch (e) { this.error.set(errText(e)); }
    this.fixing.set('');
    this.busy.set(false);
  }

  async copy() {
    await this.backend.clipboardSet(this.res()?.report ?? '').catch(() => {});
    this.copied.set(true);
    setTimeout(() => this.copied.set(false), 1500);
  }

  async save() {
    const r = this.req();
    const d = new Date().toISOString().slice(0, 16).replace(/[-:T]/g, '');
    const name = `${(r?.name ?? 'remote-desktop').replace(/[\\/:*?"<>|]/g, '-')} diagnosis ${d}.txt`;
    try {
      const p = await this.backend.saveTextAs('Save Remote Desktop Diagnosis', name, this.res()?.report ?? '');
      if (p) this.ui.toast('Saved ' + p, 'success');
    } catch (e) { this.ui.error(e); }
  }
}
