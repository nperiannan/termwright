import { Injectable, computed, inject, signal } from '@angular/core';
import { Backend } from './backend';
import { Ui } from './ui.service';
import { FileServersState, FileTransfer } from './models';
import { tr } from './i18n';

/**
 * The TFTP, FTP and HTTP servers: their state and transfers, followed from the
 * backend's "fs:state" and "fs:xfer" events (for the File Servers window and
 * the status-bar indicator).
 */
@Injectable({ providedIn: 'root' })
export class FileServers {
  private backend = inject(Backend);
  private ui = inject(Ui);

  readonly state = signal<FileServersState | null>(null);
  readonly transfers = signal<FileTransfer[]>([]);
  /** The pop-up of the status-bar indicator. */
  readonly popup = signal(false);

  readonly running = computed(() => (this.state()?.servers ?? []).filter(s => s.running));
  readonly active = computed(() => this.transfers().filter(t => t.state === 'running'));
  /** Status-bar text, e.g. "TFTP · FTP  ↓ 62%" ('' when nothing runs). */
  readonly summary = computed(() => {
    const r = this.running();
    if (!r.length) return '';
    let s = r.map(x => x.kind.toUpperCase()).join(' · ');
    const a = this.active()[0];
    if (a) s += `  ${a.put ? '↑' : '↓'} ${pct(a)}`;
    return s;
  });

  /** The device test: a device fetches termwright-test.txt; '' = none, 'wait', 'ok', 'fail'. */
  readonly test = signal<{ state: '' | 'wait' | 'ok' | 'fail'; device: string; via: string; since: number }>({ state: '', device: '', via: '', since: 0 });
  private testTimer?: ReturnType<typeof setTimeout>;
  startTest(device: string, via: string) {
    clearTimeout(this.testTimer);
    this.test.set({ state: 'wait', device, via, since: Date.now() });
    this.testTimer = setTimeout(() => {
      if (this.test().state !== 'wait') return;
      this.test.set({ ...this.test(), state: 'fail' });
      this.ui.toast(tr('{0} did not reach {1} on this computer within 2 minutes - the firewall or the route blocks it.', device, via.toUpperCase()), 'error', 10000);
    }, 120000);
  }

  constructor() {
    this.backend.on('fs:state', (s: FileServersState) => {
      const was = this.running().length > 0;
      this.state.set(s);
      if (was && !s.servers.some(x => x.running)) {
        this.popup.set(false);
        if (s.stopped) this.ui.toast(tr('File servers') + ': ' + tr(s.stopped), 'info', 6000);
      }
    });
    this.backend.on('fs:xfer', (t: FileTransfer) => {
      const l = this.transfers().slice();
      const i = l.findIndex(x => x.id === t.id);
      if (i >= 0) l[i] = t; else l.unshift(t);
      this.transfers.set(l.slice(0, 200));
      const k = this.test();
      if (k.state === 'wait' && /termwright-test\.txt$/.test(t.file) && !t.put) {
        clearTimeout(this.testTimer);
        this.test.set({ ...k, state: 'ok' });
        this.ui.toast(tr('{0} reached {1} on this computer ({2}) - the firewall lets it in.', k.device, t.server.toUpperCase(), t.peer), 'success', 8000);
      }
    });
  }

  async refresh() {
    const v = await this.backend.fileServers();
    this.state.set(v.state);
    this.transfers.set(v.transfers ?? []);
    return v;
  }

  async clear() {
    await this.backend.fileServersClear();
    this.transfers.set(this.transfers().filter(t => t.state === 'running'));
  }
}

export function pct(t: FileTransfer) {
  return t.size > 0 ? Math.min(100, Math.floor(t.bytes * 100 / t.size)) + '%' : size(t.bytes);
}

export function size(n: number) {
  if (n < 0) return '-';
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return Math.round(n / 1024) + ' KB';
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB';
  return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB';
}
