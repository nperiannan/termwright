import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Icon } from '../components/icon';

interface Row { a: string | null; b: string | null; kind: 'same' | 'del' | 'add' | 'chg'; na: number; nb: number; }

/** Line diff (LCS on lines; fine for configs and command output up to ~20k lines). */
export function diffLines(a: string[], b: string[], norm: (s: string) => string): Row[] {
  const A = a.map(norm), B = b.map(norm);
  // trim common head/tail so the LCS table stays small
  let head = 0;
  while (head < A.length && head < B.length && A[head] === B[head]) head++;
  let tail = 0;
  while (tail < A.length - head && tail < B.length - head && A[A.length - 1 - tail] === B[B.length - 1 - tail]) tail++;
  const ma = A.slice(head, A.length - tail), mb = B.slice(head, B.length - tail);
  const n = ma.length, m = mb.length;
  const rows: Row[] = [];
  for (let i = 0; i < head; i++) rows.push({ a: a[i], b: b[i], kind: 'same', na: i + 1, nb: i + 1 });
  if (n * m > 25_000_000) { // too big for a table: show as a block change
    for (let i = 0; i < Math.max(n, m); i++) {
      rows.push({ a: i < n ? a[head + i] : null, b: i < m ? b[head + i] : null, kind: 'chg', na: head + i + 1, nb: head + i + 1 });
    }
  } else {
    const L = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
    for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) L[i][j] = ma[i] === mb[j] ? L[i + 1][j + 1] + 1 : Math.max(L[i + 1][j], L[i][j + 1]);
    let i = 0, j = 0;
    const pend: Row[] = [];
    const flush = () => { // pair deletions with additions as changes
      const dels = pend.filter(r => r.kind === 'del'), adds = pend.filter(r => r.kind === 'add');
      for (let k = 0; k < Math.max(dels.length, adds.length); k++) {
        const d = dels[k], ad = adds[k];
        rows.push({ a: d?.a ?? null, b: ad?.b ?? null, kind: d && ad ? 'chg' : d ? 'del' : 'add', na: d?.na ?? 0, nb: ad?.nb ?? 0 });
      }
      pend.length = 0;
    };
    while (i < n || j < m) {
      if (i < n && j < m && ma[i] === mb[j]) { flush(); rows.push({ a: a[head + i], b: b[head + j], kind: 'same', na: head + i + 1, nb: head + j + 1 }); i++; j++; }
      else if (j < m && (i >= n || L[i][j + 1] >= L[i + 1][j])) { pend.push({ a: null, b: b[head + j], kind: 'add', na: 0, nb: head + j + 1 }); j++; }
      else { pend.push({ a: a[head + i], b: null, kind: 'del', na: head + i + 1, nb: 0 }); i++; }
    }
    flush();
  }
  for (let k = 0; k < tail; k++) rows.push({ a: a[a.length - tail + k], b: b[b.length - tail + k], kind: 'same', na: a.length - tail + k + 1, nb: b.length - tail + k + 1 });
  return rows;
}

/** Compare Outputs: side-by-side diff of two tabs' scrollback or two files. */
@Component({
  selector: 'app-compare',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop">
    <div class="modal huge">
      <div class="modal-head"><span class="mh-ic"><app-icon name="compare" [size]="17" /></span>
        <span class="mh-t">Compare Outputs<span class="mh-s">What changed between two outputs, side by side</span></span></div>
      <div class="modal-body">
        <div class="pick">
          @for (side of [0, 1]; track side) {
            <div class="src">
              <select [ngModel]="srcs()[side]" (ngModelChange)="setSrc(side, $event)">
                <option value="">Choose…</option>
                @for (t of termTabs(); track t.id) { <option [value]="'tab:' + t.id">Tab: {{ t.title }}</option> }
                @if (srcs()[side].startsWith('path:')) { <option [value]="srcs()[side]">{{ pathName(srcs()[side]) }}</option> }
                @for (g of givenOpts; track g.v) { <option [value]="g.v">{{ g.label }}</option> }
                <option value="file">File…</option>
              </select>
              <span class="hint">{{ labels()[side] }}</span>
            </div>
          }
        </div>
        <div class="opts">
          <label><input type="checkbox" [ngModel]="ignoreWs()" (ngModelChange)="ignoreWs.set($event)" /> Ignore whitespace</label>
          <label><input type="checkbox" [ngModel]="ignoreTime()" (ngModelChange)="ignoreTime.set($event)" /> Ignore timestamps / uptime counters</label>
          <label title="Removes the device prompt (e.g. core-sw1:1#) at the start of lines, so two switches' outputs line up">
            <input type="checkbox" [ngModel]="ignorePrompt()" (ngModelChange)="ignorePrompt.set($event)" /> Ignore prompts</label>
          @if (ignorePrompt()) {
            <input class="mono prompt-re" [ngModel]="promptRe()" (ngModelChange)="promptRe.set($event)" title="Prompt pattern (regular expression, matched at the start of each line)" spellcheck="false" />
          }
          <label><input type="checkbox" [ngModel]="onlyDiff()" (ngModelChange)="onlyDiff.set($event)" /> Only differences</label>
          <span class="stat">{{ stats() }}</span>
        </div>
        <div class="diff mono">
          @for (r of shown(); track $index) {
            <div class="dr {{ r.kind }}">
              <span class="n">{{ r.na || '' }}</span><span class="t">{{ r.a ?? '' }}</span>
              <span class="n">{{ r.nb || '' }}</span><span class="t">{{ r.b ?? '' }}</span>
            </div>
          } @empty { <div class="hint pad">Choose two tabs or files to compare - e.g. "show running-config" before and after a change.</div> }
        </div>
      </div>
      <div class="modal-foot"><span class="spacer"></span><button type="button" (click)="ui.close()">Close</button></div>
    </div>
  </div>`,
  styles: [`
    .modal.huge { width: 1200px; height: calc(100vh - 60px); }
    .modal-body { display: flex; flex-direction: column; min-height: 0; flex: 1; }
    .pick { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
    .src { display: flex; flex-direction: column; gap: 2px; }
    .opts { display: flex; gap: 16px; align-items: center; margin: 8px 0; font-size: calc(12px * var(--ui-scale, 1)); label { display: flex; gap: 4px; align-items: center; } .stat { margin-left: auto; color: var(--muted); } }
    .diff { flex: 1; overflow: auto; border: 1px solid var(--border); border-radius: 4px; font-size: calc(12px * var(--ui-scale, 1)); background: var(--input-bg); }
    .dr { display: grid; grid-template-columns: 44px 1fr 44px 1fr; white-space: pre; line-height: 18px;
      .n { color: var(--muted); text-align: right; padding-right: 6px; user-select: none; }
      .t { overflow: hidden; text-overflow: ellipsis; padding-right: 8px; }
      &.del .t:nth-child(2), &.chg .t:nth-child(2) { background: rgba(224, 108, 117, .22); }
      &.add .t:nth-child(4), &.chg .t:nth-child(4) { background: rgba(152, 195, 121, .22); } }
    .pad { padding: 12px; }
    .prompt-re { width: 220px; font-size: calc(11px * var(--ui-scale, 1)); padding: 2px 6px; }
  `],
})
export class Compare {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  readonly termTabs = computed(() => this.tabs.tabs().filter(t => t.kind === 'terminal'));
  readonly srcs = signal<[string, string]>(['', '']);
  readonly labels = signal<[string, string]>(['', '']);
  private readonly texts = signal<[string[] | null, string[] | null]>([null, null]);
  readonly ignoreWs = signal(true);
  readonly ignoreTime = signal(true);
  readonly onlyDiff = signal(false);
  readonly ignorePrompt = signal(false);
  /** Default: a host-like word (with optional :slot / (config) parts) ending in # > $ or %, e.g. "core-sw1:1#", "* X460.3 #", "user@host:~$". */
  readonly promptRe = signal(String.raw`^\*?\s?[\w.@~/:()\[\]-]+(\s[\w.()-]+)?\s?[#>$%]\s?`);

  constructor() {
    const given = this.ui.compareTexts();
    this.ui.compareTexts.set(null);
    if (given) {
      given.forEach((g, i) => { const v = 'given:' + i; this.given[v] = { label: g.label, lines: g.text.split(/\r?\n/) }; this.givenOpts.push({ v, label: g.label }); });
      void this.setSrc(0, 'given:0'); void this.setSrc(1, 'given:1');
      return;
    }
    const pre = this.ui.compareWith();
    this.ui.compareWith.set(null);
    if (pre) { void this.setSrc(0, pre[0]); void this.setSrc(1, pre[1]); return; }
    const tt = this.termTabs();
    if (tt.length >= 2) { this.setSrc(0, 'tab:' + tt[0].id); this.setSrc(1, 'tab:' + tt[1].id); }
  }

  private tabText(id: string): string[] {
    const b = this.tabs.handles.get(id)?.term.buffer.active;
    if (!b) return [];
    const out: string[] = [];
    for (let i = 0; i < b.length; i++) {
      const l = b.getLine(i);
      if (!l) continue;
      const s = l.translateToString(true);
      if (l.isWrapped && out.length) out[out.length - 1] += s; else out.push(s);
    }
    while (out.length && !out[out.length - 1].trim()) out.pop();
    return out;
  }

  pathName(v: string) {
    const parts = v.slice(5).split(/[\\/]/);
    const phase = parts.lastIndexOf('before') >= 0 ? 'Before: ' : parts.lastIndexOf('after') >= 0 ? 'After: ' : '';
    return phase + parts.slice(-2).join('/');
  }

  /** Texts handed over by another window (SFTP: a local and a remote file). */
  private given: Record<string, { label: string; lines: string[] }> = {};
  readonly givenOpts: { v: string; label: string }[] = [];

  async setSrc(side: number, v: string) {
    let lines: string[] | null = null, label = '';
    if (v.startsWith('given:')) {
      lines = this.given[v]?.lines ?? null;
      label = (this.given[v]?.label ?? '') + ' · ' + (lines?.length ?? 0) + ' lines';
    } else if (v.startsWith('tab:')) {
      const id = v.slice(4);
      lines = this.tabText(id);
      label = `${lines.length} lines of scrollback`;
    } else if (v.startsWith('path:')) {
      try { lines = (await this.backend.readTextFile(v.slice(5))).split(/\r?\n/); label = v.slice(5); } catch (e) { this.ui.error(e); return; }
    } else if (v === 'file') {
      const p = await this.backend.chooseOpenFile('Choose File to Compare').catch(() => '');
      if (!p) return;
      try { lines = (await this.backend.readTextFile(p)).split(/\r?\n/); label = p; } catch (e) { this.ui.error(e); return; }
    }
    this.srcs.update(s => { const n = [...s] as [string, string]; n[side] = v; return n; });
    this.labels.update(s => { const n = [...s] as [string, string]; n[side] = label; return n; });
    this.texts.update(s => { const n = [...s] as [string[] | null, string[] | null]; n[side] = lines; return n; });
  }

  readonly rows = computed(() => {
    const [a, b] = this.texts();
    if (!a || !b) return [];
    const ws = this.ignoreWs(), tm = this.ignoreTime();
    let pr: RegExp | null = null;
    if (this.ignorePrompt()) { try { pr = new RegExp(this.promptRe()); } catch { pr = null; } }
    const norm = (s: string) => {
      let x = pr ? s.replace(pr, '') : s;
      if (tm) x = x.replace(/\b\d{1,2}:\d{2}:\d{2}(\.\d+)?\b/g, 'T').replace(/\b\d+ days?,?/g, 'D').replace(/\b\d{4}-\d{2}-\d{2}\b/g, 'DATE')
        .replace(/\b(Mon|Tue|Wed|Thu|Fri|Sat|Sun)[a-z]*\b/g, 'DAY').replace(/\b(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*\.? +\d{1,2}\b/g, 'DATE');
      if (ws) x = x.replace(/\s+/g, ' ').trim();
      return x;
    };
    return diffLines(a, b, norm);
  });
  readonly shown = computed(() => (this.onlyDiff() ? this.rows().filter(r => r.kind !== 'same') : this.rows()));
  readonly stats = computed(() => {
    const r = this.rows();
    if (!r.length) return '';
    const c = (k: string) => r.filter(x => x.kind === k).length;
    return `${c('chg')} changed · ${c('del')} only left · ${c('add')} only right`;
  });
}
