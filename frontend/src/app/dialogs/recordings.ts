import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { RecordingInfo } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';

/** Recordings: the session recordings by session and date; play, show in folder, settings. */
@Component({
  selector: 'app-recordings',
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop" (click)="ui.close()">
    <div class="modal recs" (click)="$event.stopPropagation()">
      <div class="modal-head">Recordings</div>
      <div class="modal-body">
        <div class="top">
          <input class="q" [ngModel]="query()" (ngModelChange)="query.set($event)" placeholder="Filter by session, host or date" spellcheck="false" />
          <label class="inline"><input type="checkbox" [ngModel]="all()" (ngModelChange)="setAll($event)" /> Record every session</label>
          <label class="inline">Keep <input type="number" class="days" min="0" [ngModel]="keep()" (ngModelChange)="setKeep(+$event)" /> days (0 = always)</label>
        </div>
        <div class="list">
          <table>
            <thead><tr><th>Session</th><th>Started</th><th>Length</th><th>Markers</th><th>Size</th><th></th></tr></thead>
            <tbody>
              @for (r of shown(); track r.path) {
                <tr (dblclick)="play(r)">
                  <td><b>{{ r.title || r.session }}</b><div class="sub">{{ r.session }}{{ r.host ? ' · ' + r.host : '' }}</div></td>
                  <td>{{ when(r.started) }}</td>
                  <td>{{ len(r.duration) }}</td>
                  <td>{{ r.markers || '' }}</td>
                  <td>{{ size(r.size) }}</td>
                  <td class="acts"><button type="button" class="primary" (click)="play(r)">Play</button><button type="button" (click)="reveal(r)">Show</button></td>
                </tr>
              } @empty {
                <tr><td colspan="6" class="empty">{{ loading() ? 'Loading…' : list().length ? 'No recording matches.' :
                  'No recordings yet. Right-click a tab → Record Session, tick "Record every session" above, or turn on recording in a session (Session Options → Log File) or a folder (Folder Defaults).' }}</td></tr>
              }
            </tbody>
          </table>
        </div>
        <p class="hint">Recordings keep what the devices showed with its timing (asciinema format, .cast) - not what you typed. Saved in {{ dir() }}.</p>
      </div>
      <div class="modal-foot">
        <button type="button" (click)="folder()">Open Folder</button>
        <button type="button" (click)="openFile()">Play File…</button>
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Close</button>
      </div>
    </div>
  </div>`,
  styles: [`
    .recs { width: min(900px, 95vw); }
    .top { display: flex; gap: 14px; align-items: center; margin-bottom: 8px; flex-wrap: wrap; .q { flex: 1; min-width: 200px; } }
    .inline { display: inline-flex; gap: 6px; align-items: center; white-space: nowrap; font-size: 12.5px; }
    .days { width: 64px; }
    .list { height: 48vh; overflow: auto; border: 1px solid var(--border); border-radius: 6px; }
    table { width: 100%; } th { position: sticky; top: 0; background: var(--panel); z-index: 1; text-align: left; }
    td { font-size: 12.5px; vertical-align: middle; } .sub { color: var(--muted); font-size: 11px; }
    .acts { text-align: right; white-space: nowrap; button { height: 22px; font-size: 11.5px; padding: 0 8px; margin-left: 4px; } }
    .empty { padding: 16px; color: var(--muted); white-space: normal; }
    .spacer { flex: 1; }
  `],
})
export class Recordings {
  protected ui = inject(Ui);
  private tabs = inject(Tabs);
  private backend = inject(Backend);
  readonly list = signal<RecordingInfo[]>([]);
  readonly loading = signal(true);
  readonly query = signal('');
  readonly dir = signal('');
  readonly all = computed(() => !!this.ui.settings()?.recordSessions);
  readonly keep = computed(() => this.ui.settings()?.recordKeepDays ?? 0);
  readonly shown = computed(() => {
    const q = this.query().trim().toLowerCase();
    return q ? this.list().filter(r => [r.session, r.title, r.host, this.when(r.started)].join(' ').toLowerCase().includes(q)) : this.list();
  });

  constructor() {
    this.backend.recordingDir().then(d => this.dir.set(d)).catch(() => {});
    this.backend.recordings().then(l => this.list.set(l ?? [])).catch(e => this.ui.error(e)).finally(() => this.loading.set(false));
  }

  setAll(on: boolean) { this.ui.patchSettings({ recordSessions: on }); }
  setKeep(n: number) { this.ui.patchSettings({ recordKeepDays: Math.max(0, Math.floor(n) || 0) }); }

  when(t: string) { return t ? new Date(t).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }) : ''; }
  len(s: number) {
    const t = Math.round(s), h = Math.floor(t / 3600), m = Math.floor((t % 3600) / 60);
    return h ? `${h} h ${m} min` : m ? `${m} min ${t % 60} s` : `${t} s`;
  }
  size(b: number) { return b > 1 << 20 ? (b / (1 << 20)).toFixed(1) + ' MB' : Math.max(1, Math.round(b / 1024)) + ' KB'; }

  play(r: RecordingInfo) {
    this.ui.close();
    void this.tabs.openPlayer(r.path, (r.title || r.session.split('/').pop() || '') + ' ' + this.when(r.started));
  }
  async reveal(r: RecordingInfo) { await this.backend.showInFolder(r.path).catch(e => this.ui.error(e)); }
  async folder() { await this.backend.showRecordingFolder().catch(e => this.ui.error(e)); }
  async openFile() {
    const p = await this.backend.chooseRecording().catch(e => { this.ui.error(e); return ''; });
    if (!p) return;
    this.ui.close();
    void this.tabs.openPlayer(p);
  }
}
