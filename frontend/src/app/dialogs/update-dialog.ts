import { ChangeDetectorRef, Component, OnDestroy, OnInit, inject, signal } from '@angular/core';
import { Backend, errText } from '../core/backend';
import { UpdateInfo, UpdateStatus, formatBytes } from '../core/models';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';

/** One line of release notes (light Markdown: headings, bullets, text). */
interface NoteLine { kind: 'h' | 'li' | 'p'; text: string; }

/**
 * Help → Check for Updates (also Options → Updates → Check for Updates…):
 * the installed version, the latest stable release with its changes, and a
 * newer nightly build if one exists (with a backup step before installing it).
 */
@Component({
  selector: 'app-update-dialog',
  imports: [Icon],
  template: `
  <div class="modal-backdrop">
    <div class="modal upd">
      <div class="modal-head"><span class="mh-ic"><app-icon name="down" [size]="17" /></span>
        <span class="mh-t">Software Update<span class="mh-s">{{ st() ? 'Termwright ' + base(st()!.current) + (st()!.runningNightly ? ' (nightly ' + sha(st()!.current) + ')' : '') + ' is installed' : 'Checking for updates…' }}</span></span></div>
      <div class="modal-body">
        @if (checking()) {
          <p>Checking for updates…</p>
        } @else if (st(); as s) {
          <p class="installed cl-only">Installed: <b>Termwright {{ base(s.current) }}</b>
            @if (s.runningNightly) { <span class="tag nightly">nightly {{ sha(s.current) }}</span> }</p>

          <section class="card">
            <div class="title"><span class="tag stable">Stable</span>
              @if (s.stable; as i) {
                @if (i.newer) { <b>Termwright {{ i.latest }} is available</b> } @else { <b>Termwright {{ i.latest }}</b> is the latest stable release }
                <span class="hint">{{ date(i.published) }}</span>
              } @else { <span class="err">{{ s.stableErr }}</span> }
            </div>
            @if (s.stable; as i) {
              @if (!i.newer) {
                <p class="ok">{{ s.runningNightly ? 'Your nightly build already includes this release.' : 'You are up to date.' }}</p>
              } @else {
                @if (notes(i).length) {
                  <div class="notes">
                    <div class="nh">What's new</div>
                    @for (l of notes(i); track $index) {
                      @switch (l.kind) { @case ('h') { <div class="nh">{{ l.text }}</div> } @case ('li') { <div class="li">{{ l.text }}</div> } @default { <div>{{ l.text }}</div> } }
                    }
                  </div>
                }
                @if (i.assetName) {
                  <button type="button" class="primary" [disabled]="busy()" (click)="download(i)">Download {{ i.latest }} ({{ size(i.assetSize) }})</button>
                } @else { <p class="hint">No installer for this computer in that release - open the release page.</p> }
              }
            }
          </section>

          @if (s.nightly; as n) {
            @if (n.newer) {
              <section class="card">
                <div class="title"><span class="tag nightly">Nightly</span> <b>Build {{ sha(n.latest) }}</b>@if (n.version) { <span class="hint">based on {{ n.version }}</span> } <span class="hint">{{ date(n.published) }}</span>
                  @if (modern()) { <button type="button" class="fold" (click)="nightlyOpen.set(!nightlyOpen())">{{ nightlyOpen() ? '▾ Hide' : '▸ Show' }}</button> }</div>
                @if (modern() && !nightlyOpen()) {
                  <div class="hint">Newest code, not released yet - back up before installing.</div>
                } @else {
                <div class="warn">
                  <b>Untested build.</b> Nightly builds contain the newest code before it is released. They may crash, and they may change or
                  lose your settings and saved sessions. <b>Back up before you install one</b>; passwords stay in the OS keychain and are not affected.
                </div>
                <div class="row">
                  <button type="button" [disabled]="busy()" (click)="backup()">Back Up Now</button>
                  @if (backupPath()) { <span class="hint">Saved to <span class="mono">{{ backupPath() }}</span></span> }
                </div>
                <label class="ack"><input type="checkbox" [checked]="ack()" (change)="ack.set(!ack())" /> I have a backup and accept the risk</label>
                @if (n.assetName) {
                  <button type="button" [disabled]="busy() || !ack()" (click)="download(n)">Download nightly ({{ size(n.assetSize) }})</button>
                }
                }
              </section>
            } @else if (s.runningNightly) {
              <p class="hint">You have the latest nightly build.</p>
            }
          } @else if (s.nightlyErr) { <p class="hint">Nightly builds: {{ s.nightlyErr }}</p> }

          @if (err()) { <p class="err">{{ err() }}</p> }
          @if (total() > 0 || done() > 0) {
            <progress [max]="total() || 1" [value]="done()"></progress>
            <div class="hint">{{ size(done()) }} / {{ size(total()) }}</div>
          }
          @if (saved()) {
            <p class="hint">Saved to {{ saved() }}.<br />
              @switch (os()) {
                @case ('darwin') { Quit Termwright, then drag Termwright from the opened disk image to Applications (replace). }
                @case ('windows') { The installer replaces the current version; close Termwright when it asks. }
                @default { Install the package (e.g. sudo apt install ./{{ savedName() }}), then restart Termwright. }
              }
            </p>
          }
        }
      </div>
      <div class="modal-foot">
        @if (st()?.stable?.page) { <button type="button" (click)="backend.openURL(st()!.stable!.page)">Release Notes</button> }
        <span class="spacer"></span>
        <button type="button" [class.primary]="modern() && !saved()" (click)="ui.close()">{{ modern() && !saved() ? 'Done' : 'Close' }}</button>
        @if (saved()) { <button type="button" class="primary" (click)="openInstaller()">Open Installer</button> }
      </div>
    </div>
  </div>`,
  styles: [`
    .upd { width: min(640px, 92vw); }
    .installed { margin: 0 0 10px; }
    .card { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; margin-bottom: 10px; display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
    .title { display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap; }
    .tag { font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; padding: 1px 7px; border-radius: 10px; color: #fff; }
    .tag.stable { background: #16a34a; }
    .tag.nightly { background: #d97706; }
    .ok { margin: 0; color: var(--muted); }
    .notes { align-self: stretch; max-height: 220px; overflow: auto; font-size: calc(12px * var(--ui-scale, 1)); background: var(--input-bg); padding: 8px 10px; border-radius: 6px; user-select: text; line-height: 1.5; }
    .nh { font-weight: 600; margin: 4px 0 2px; }
    .li { padding-left: 14px; position: relative; }
    .li::before { content: '•'; position: absolute; left: 3px; color: var(--muted); }
    .row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .ack { font-size: calc(12px * var(--ui-scale, 1)); display: flex; gap: 6px; align-items: center; }
    progress { width: 100%; accent-color: var(--accent); }
    .err { color: var(--danger); white-space: pre-wrap; }
    .mono { font-family: var(--mono, monospace); word-break: break-all; }
    .fold { margin-left: auto; height: 22px; padding: 0 6px; border: 0 !important; background: transparent !important; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); }
    :host-context(.modern) {
      .upd { width: min(560px, 92vw); }
      .card { border-color: var(--line); border-radius: var(--r-lg); padding: 12px 14px; background: var(--surface); }
      .card + .card { background: var(--surface-2); }
      .title { align-items: center; width: 100%; }
      .tag { border-radius: 5px; padding: 2px 7px; font-weight: 700; }
      .tag.stable { background: color-mix(in srgb, var(--ok) 16%, transparent); color: var(--ok); }
      .tag.nightly { background: rgba(229, 169, 59, .18); color: #e5a93b; }
      .notes { background: transparent; padding: 0; font-size: calc(12.5px * var(--ui-scale, 1)); }
      .notes > .nh:first-child { display: none; }
      .warn { border-radius: var(--r); }
    }
  `],
})
export class UpdateDialog implements OnInit, OnDestroy {
  protected ui = inject(Ui);
  protected backend = inject(Backend);
  readonly modern = () => this.ui.settings()?.layout !== 'classic';
  readonly nightlyOpen = signal(false);
  private cdr = inject(ChangeDetectorRef);
  readonly checking = signal(true);
  readonly st = signal<UpdateStatus | null>(null);
  readonly err = signal('');
  readonly busy = signal(false);
  readonly ack = signal(false);
  readonly backupPath = signal('');
  readonly done = signal(0);
  readonly total = signal(0);
  readonly saved = signal('');
  readonly savedName = signal('');
  readonly os = signal('');
  readonly size = formatBytes;
  private noteCache = new Map<string, NoteLine[]>();
  private unsub = () => {};

  async ngOnInit() {
    this.unsub = this.backend.on('update:progress', (p: { done: number; total: number }) => {
      this.done.set(p.done);
      this.total.set(p.total);
    });
    this.backend.getAppInfo().then(i => this.os.set(i.os)).catch(() => {});
    try {
      this.st.set(await this.backend.checkForUpdate());
    } catch (e) { this.err.set(errText(e)); }
    this.checking.set(false);
    this.cdr.markForCheck();
  }

  ngOnDestroy() { this.unsub(); }

  /** "0.11.1~git1a2b3c4" → "0.11.1" */
  base(v: string) { return v.replace(/[~-].*$/, ''); }
  sha(v: string) { return (/([0-9a-f]{7,40})\)?$/.exec(v)?.[1] ?? v).slice(0, 7); }
  date(iso: string) {
    const d = iso ? new Date(iso) : null;
    return d && !isNaN(+d) && d.getFullYear() > 2000 ? d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' }) : '';
  }

  /** Release notes as plain lines: GitHub's generated notes are Markdown with links. */
  notes(i: UpdateInfo): NoteLine[] {
    let r = this.noteCache.get(i.notes);
    if (r) return r;
    r = [];
    for (let raw of (i.notes || '').split(/\r?\n/)) {
      raw = raw.trim();
      if (!raw || /^<!--/.test(raw)) continue;
      const text = raw.replace(/^#+\s*|^[*-]\s+/, '').replace(/\*\*|__|`/g, '')
        .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/ by @[\w-]+ in https?:\/\/\S+$/, '').replace(/https?:\/\/\S+/g, '').trim();
      if (!text || /^full changelog:?$/i.test(text)) continue;
      r.push({ kind: /^#/.test(raw) ? 'h' : /^[*-]\s/.test(raw) ? 'li' : 'p', text });
    }
    if (r[0]?.kind === 'h' && /what'?s changed/i.test(r[0].text)) r.shift();
    this.noteCache.set(i.notes, r);
    return r;
  }

  async backup() {
    this.busy.set(true);
    try {
      this.backupPath.set(await this.backend.backupConfig());
      this.ack.set(true);
    } catch (e) { this.err.set(errText(e)); }
    this.busy.set(false);
  }

  async download(i: UpdateInfo) {
    this.busy.set(true);
    this.err.set('');
    this.done.set(0);
    this.total.set(0);
    try {
      this.saved.set(await this.backend.downloadUpdate(i.assetUrl, i.assetName));
      this.savedName.set(i.assetName);
    } catch (e) { this.err.set(errText(e)); }
    this.busy.set(false);
  }

  async openInstaller() {
    try { await this.backend.openUpdate(this.saved()); } catch (e) { this.ui.error(e); }
  }
}
