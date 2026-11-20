import { Injectable, computed, inject, signal } from '@angular/core';
import type { IMarker, Terminal } from '@xterm/xterm';
import { Backend, errText } from './backend';
import { Ui } from './ui.service';
import { tr } from './i18n';
import { SECRET } from './history.service';
import { TestCase, TestExpect, TestStep } from './models';

/** A recorded step; for a terminal step the marker on its command line finds its output later. */
export interface RecStep { step: TestStep; tab?: string; term?: Terminal; marker?: IMarker; output?: string; status?: number; ms?: number; }

export type ExpectKind = 'contains' | 'field' | 'matches' | 'notContains' | 'save';

/** The test recorder: while on, commands typed in terminal tabs and requests sent in the API client
 *  become steps of a test; checks come from selected output ("Expect this") or the Steps panel. */
@Injectable({ providedIn: 'root' })
export class Recorder {
  private backend = inject(Backend);
  private ui = inject(Ui);
  readonly on = signal(false);
  readonly paused = signal(false);
  readonly name = signal('');
  readonly steps = signal<RecStep[]>([]);
  readonly checks = computed(() => this.steps().reduce((n, s) => n + (s.step.expect?.length ?? 0), 0));
  readonly recording = computed(() => this.on() && !this.paused());

  start(name = '') {
    if (this.on()) return;
    this.name.set(name || tr('New test'));
    this.steps.set([]);
    this.paused.set(false);
    this.on.set(true);
  }

  /** A command typed on a terminal tab (Enter on its line). */
  command(tab: string, session: string, cmd: string, term: Terminal) {
    if (!this.recording() || !session || !cmd.trim()) return;
    if (SECRET.test(cmd)) { this.ui.toast(tr('Not recorded: the command may hold a secret'), 'info'); return; }
    this.fillOutput(tab);
    const marker = term.registerMarker(0);
    this.push({ step: { run: { session, command: cmd.trim() } }, tab, term, marker });
  }

  /** A request sent in the API client; its status becomes the first check. */
  http(h: NonNullable<TestStep['http']>, status: number, ms: number, body: string) {
    if (!this.recording() || !h.url) return;
    const expect: TestExpect[] = status ? [{ status }] : [];
    this.push({ step: { http: h, expect }, output: body, status, ms });
  }

  private push(s: RecStep) { this.steps.update(l => [...l, s]); }

  /** The output of a tab's last step: the lines after its command up to the prompt line. */
  private fillOutput(tab: string) {
    const l = this.steps();
    for (let i = l.length - 1; i >= 0; i--) {
      if (l[i].tab !== tab) continue;
      this.output(l[i]);
      return;
    }
  }

  output(s: RecStep): string {
    if (s.term && s.marker && !s.marker.isDisposed && s.marker.line >= 0) {
      const b = s.term.buffer.active, end = b.baseY + b.cursorY, lines: string[] = [];
      for (let r = s.marker.line + 1; r < end; r++) {
        const ln = b.getLine(r);
        if (!ln) continue;
        const t = ln.translateToString(true);
        if (ln.isWrapped && lines.length) lines[lines.length - 1] += t; else lines.push(t);
      }
      s.output = lines.join('\n');
    }
    return s.output ?? '';
  }

  /** The step whose output holds the selection: the tab's last one. */
  lastOf(tab: string): number {
    const l = this.steps();
    for (let i = l.length - 1; i >= 0; i--) if (l[i].tab === tab) return i;
    return -1;
  }

  /** "Expect this" on text selected in a terminal tab. */
  expectSelection(tab: string, sel: string, kind: ExpectKind) {
    const i = this.lastOf(tab);
    if (i < 0) { this.ui.toast(tr('Type a command first: checks belong to the last recorded command of this tab'), 'info'); return; }
    sel = sel.replace(/\r?\n/g, ' ').trim();
    if (!sel) return;
    const s = this.steps()[i], out = this.output(s);
    const field = fieldPattern(out, sel);
    switch (kind) {
      case 'contains': this.addCheck(i, { contains: sel }); break;
      case 'notContains': this.addCheck(i, { notContains: sel }); break;
      case 'matches': this.addCheck(i, { matches: escapeRe(sel).replace(/\d+/g, '\\d+') }); break;
      case 'field': this.addCheck(i, field ? { field, equals: sel } : { contains: sel }); break;
      case 'save': void this.saveVar(i, field ?? `(${escapeRe(sel)})`, sel); break;
    }
  }

  /** The field pattern of the selection in out, e.g. "Admin State:\s+(\S+)". */
  fieldOf(tab: string, sel: string): string | null {
    const i = this.lastOf(tab);
    return i < 0 ? null : fieldPattern(this.output(this.steps()[i]), sel.trim());
  }

  private async saveVar(i: number, field: string, sel: string) {
    const v = await this.ui.input(tr('Save as Variable'), tr('Name (used later as {{name}})'), slugVar(sel));
    if (!v) return;
    this.patch(i, s => ({ ...s, save: [...(s.save ?? []), { var: slugVar(v), field }] }));
  }

  addCheck(i: number, e: TestExpect) {
    this.patch(i, s => (s.expect ?? []).some(x => JSON.stringify(x) === JSON.stringify(e)) ? s : { ...s, expect: [...(s.expect ?? []), e] });
  }
  removeCheck(i: number, j: number) { this.patch(i, s => ({ ...s, expect: (s.expect ?? []).filter((_, k) => k !== j) })); }
  removeSave(i: number, j: number) { this.patch(i, s => ({ ...s, save: (s.save ?? []).filter((_, k) => k !== j) })); }
  removeStep(i: number) { this.steps.update(l => l.filter((_, k) => k !== i)); }
  moveStep(i: number, d: -1 | 1) {
    this.steps.update(l => { const j = i + d; if (j < 0 || j >= l.length) return l; const c = [...l]; [c[i], c[j]] = [c[j], c[i]]; return c; });
  }
  patch(i: number, f: (s: TestStep) => TestStep) {
    this.steps.update(l => l.map((s, k) => k === i ? { ...s, step: f(s.step) } : s));
  }

  /** The test as it would be saved. */
  testCase(): TestCase {
    const l = this.steps();
    l.forEach(s => this.output(s));
    return { name: this.name().trim() || tr('New test'), steps: l.map(s => s.step) };
  }

  async save(): Promise<boolean> {
    if (!this.steps().length) { this.ui.toast(tr('Nothing recorded yet'), 'info'); return false; }
    const file = await this.ui.input(tr('Save Test'), tr('File in the tests folder (cases/…)'), 'cases/' + slugFile(this.name()) + '.yaml');
    if (!file) return false;
    try {
      const rel = await this.backend.testSave(file, this.testCase());
      this.ui.toast(tr('Saved {0}', rel), 'success');
      this.stop();
      return true;
    } catch (e) { this.ui.toast(errText(e), 'error'); return false; }
  }

  async discard() {
    if (this.steps().length && !(await this.ui.confirm(tr('Discard the recording?'), tr('{0} steps are not saved.', this.steps().length), tr('Discard'), true))) return;
    this.stop();
  }

  private stop() {
    this.steps().forEach(s => s.marker?.dispose());
    this.steps.set([]);
    this.on.set(false);
    this.paused.set(false);
  }
}

export function escapeRe(s: string) { return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); }

/** A regular expression whose group is the selected value: the label before it on its line
 *  ("Admin State:   Enabled" → "Admin State:\s+(\S+)"); null when the value has no label. */
export function fieldPattern(out: string, sel: string): string | null {
  if (!sel || sel.includes('\n')) return null;
  const tok = new RegExp('(^|\\s)' + escapeRe(sel) + '(?=$|[\\s.,;)])');
  for (const line of out.split('\n')) {
    const m = tok.exec(line);
    if (!m) continue;
    const at = m.index + m[1].length;
    const before = line.slice(0, at).replace(/\s+$/, ''), label = before.split(/\s{2,}/).pop()?.trim() ?? '';
    if (!label || label.length > 60) return null;
    const value = /^\d+(\.\d+)?$/.test(sel) ? '(\\d+(?:\\.\\d+)?)' : /\s/.test(sel) ? '(\\S+(?: \\S+)*)' : '(\\S+)';
    return escapeRe(label).replace(/\s+/g, '\\s+') + '\\s*' + value;
  }
  return null;
}

function slugVar(s: string) { return s.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '').slice(0, 30) || 'value'; }
function slugFile(s: string) { return s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 50) || 'test'; }
