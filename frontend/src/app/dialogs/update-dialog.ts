import { ChangeDetectorRef, Component, OnDestroy, OnInit, inject, signal } from '@angular/core';
import { Backend, errText } from '../core/backend';
import { UpdateInfo, formatBytes } from '../core/models';
import { Ui } from '../core/ui.service';

/** Help → Check for Updates: shows the newest build and downloads its installer. */
@Component({
  selector: 'app-update-dialog',
  template: `
  <div class="modal-backdrop">
    <div class="modal">
      <div class="modal-head">Software Update</div>
      <div class="modal-body">
        @if (checking()) {
          <p>Checking {{ ui.settings()?.updateRepo }} ({{ ui.settings()?.updateChannel }} channel)…</p>
        } @else if (err()) {
          <p class="err">{{ err() }}</p>
        } @else if (info(); as i) {
          <p>Installed: <b>{{ i.current }}</b><br />Latest {{ ui.settings()?.updateChannel }}: <b>{{ i.latest }}</b>
            @if (i.published) { <span class="hint"> ({{ i.published.slice(0, 10) }})</span> }</p>
          @if (i.newer) {
            <p>A newer build is available.@if (i.assetName) { Installer for this computer: <span class="mono">{{ i.assetName }}</span> }</p>
          } @else {
            <p>You are up to date.</p>
          }
          @if (i.notes) { <pre class="notes">{{ i.notes }}</pre> }
          @if (i.newer && !i.assetName) { <p class="hint">No installer for this platform in that release - open the release page.</p> }
          @if (total() > 0 || done() > 0) {
            <progress [max]="total() || 1" [value]="done()"></progress>
            <div class="hint">{{ size(done()) }} / {{ size(total()) }}</div>
          }
          @if (saved()) {
            <p class="hint">Saved to {{ saved() }}.<br />
              @switch (os()) {
                @case ('darwin') { Quit Termwright, then drag Termwright from the opened disk image to Applications (replace). }
                @case ('windows') { The installer replaces the current version; close Termwright when it asks. }
                @default { Install the package (e.g. sudo apt install ./{{ i.assetName }}), then restart Termwright. }
              }
            </p>
          }
        }
      </div>
      <div class="modal-foot">
        @if (info()?.page) { <button type="button" (click)="backend.openURL(info()!.page)">Release Page</button> }
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Close</button>
        @if (saved()) {
          <button type="button" class="primary" (click)="openInstaller()">Open Installer</button>
        } @else if (info()?.newer && info()?.assetName) {
          <button type="button" class="primary" [disabled]="downloading()" (click)="download()">
            {{ downloading() ? 'Downloading…' : 'Download (' + size(info()!.assetSize) + ')' }}</button>
        }
      </div>
    </div>
  </div>`,
  styles: [`
    .notes { max-height: 220px; overflow: auto; white-space: pre-wrap; font-size: 12px; background: var(--input-bg); padding: 8px; border-radius: 4px; user-select: text; }
    progress { width: 100%; accent-color: var(--accent); }
    .err { color: var(--danger); white-space: pre-wrap; }
  `],
})
export class UpdateDialog implements OnInit, OnDestroy {
  protected ui = inject(Ui);
  protected backend = inject(Backend);
  private cdr = inject(ChangeDetectorRef);
  readonly checking = signal(true);
  readonly info = signal<UpdateInfo | null>(null);
  readonly err = signal('');
  readonly downloading = signal(false);
  readonly done = signal(0);
  readonly total = signal(0);
  readonly saved = signal('');
  readonly os = signal('');
  readonly size = formatBytes;
  private unsub = () => {};

  async ngOnInit() {
    this.unsub = this.backend.on('update:progress', (p: { done: number; total: number }) => {
      this.done.set(p.done);
      this.total.set(p.total);
    });
    this.backend.getAppInfo().then(i => this.os.set(i.os)).catch(() => {});
    try {
      this.info.set(await this.backend.checkForUpdate());
    } catch (e) { this.err.set(errText(e)); }
    this.checking.set(false);
    this.cdr.markForCheck();
  }

  ngOnDestroy() { this.unsub(); }

  async download() {
    const i = this.info();
    if (!i) return;
    this.downloading.set(true);
    try {
      this.saved.set(await this.backend.downloadUpdate(i.assetUrl, i.assetName));
    } catch (e) { this.err.set(errText(e)); }
    this.downloading.set(false);
  }

  async openInstaller() {
    try { await this.backend.openUpdate(this.saved()); } catch (e) { this.ui.error(e); }
  }
}
