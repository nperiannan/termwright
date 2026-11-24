import { Injectable, inject, signal } from '@angular/core';
import { Backend } from './backend';
import { Ui } from './ui.service';
import { tr } from './i18n';

/** Test browsers that web steps left running (a runner killed, the app quit while one was open): how many,
 * and closing them after a confirm. Only Playwright's browsers - never the person's own Chrome, Edge or Safari. */
@Injectable({ providedIn: 'root' })
export class TestBrowsers {
  private backend = inject(Backend);
  private ui = inject(Ui);
  readonly count = signal(0);

  async refresh() {
    try { this.count.set((await this.backend.webLeftovers() ?? []).length); } catch { this.count.set(0); }
    return this.count();
  }

  async close() {
    let l: { name: string; started: string }[] = [];
    try { l = await this.backend.webLeftovers() ?? []; } catch { /* none */ }
    this.count.set(l.length);
    if (!l.length) { this.ui.toast(tr('No test browsers are open'), 'info'); return; }
    const items = l.map(x => x.name + (x.started ? ' · ' + tr('started {0}', x.started) : ''));
    const ok = await this.ui.choose(l.length === 1 ? tr('Close 1 test browser?') : tr('Close {0} test browsers?', l.length),
      tr('Only browsers Termwright started for web tests. Your own Chrome, Edge and Safari are not touched. A recording or step-by-step run that is open now is kept.'),
      items, [{ label: tr('Close Them'), value: 'close', danger: true, primary: true }], true);
    if (ok !== 'close') return;
    try {
      const n = await this.backend.webCloseLeftovers();
      this.ui.toast(n === 1 ? tr('Closed 1 test browser') : tr('Closed {0} test browsers', n), 'success');
    } catch (e) { this.ui.toast(String(e), 'error'); }
    await this.refresh();
  }
}
