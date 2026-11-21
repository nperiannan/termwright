import { Injectable, computed, inject, signal } from '@angular/core';
import type { IMarker, Terminal } from '@xterm/xterm';
import { Backend, errText } from './backend';
import { Ui } from './ui.service';
import { tr } from './i18n';
import { SECRET } from './history.service';
import { TestCase, TestExpect, TestStep } from './models';

/** A recorded step; for a terminal step the marker on its command line finds its output later. */
export type RecPhase = 'setup' | 'steps' | 'cleanup';
/** A recorded step; for a terminal step the marker on its command line finds its output later. undo: the
 *  suggested cleanup step (from the device type's undo rules, a POST's DELETE or an SNMP set's old value). */
export interface RecStep { step: TestStep; phase: RecPhase; undo?: TestStep; undone?: boolean; tab?: string; term?: Terminal; marker?: IMarker; output?: string; status?: number; ms?: number; }

export type ExpectKind = 'contains' | 'field' | 'matches' | 'notContains' | 'save';

/** The test recorder: while on, commands typed in terminal tabs and requests sent in the API client
 *  become steps of a test; checks come from selected output ("Expect this") or the Steps panel. */
@Injectable({ providedIn: 'root' })
export class Recorder {
  private backend = inject(Backend);
  private ui = inject(Ui);
  readonly on = signal(false);
  readonly paused = signal(false);
  /** Saved sessions become {{roles}} (their paths kept as the test's defaults), so a testbed can point the test at other devices. */
  readonly roles = signal(true);
  /** Where new steps go: setup, the test's own steps or the cleanup. */
  readonly phase = signal<RecPhase>('steps');
  /** What Termwright saves before the test and puts back after it. */
  readonly restore = signal<('config' | 'lab')[]>([]);
  readonly undos = computed(() => this.steps().filter(s => s.undo && !s.undone).length);
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
  command(tab: string, session: string, cmd: string, term: Terminal, deviceType = '') {
    if (!this.recording() || !session || !cmd.trim()) return;
    if (SECRET.test(cmd)) { this.ui.toast(tr('Not recorded: the command may hold a secret'), 'info'); return; }
    this.fillOutput(tab);
    const marker = term.registerMarker(0);
    const u = this.phase() === 'steps' ? undoOf(this.undoRules(deviceType), cmd) : '';
    this.push({ step: { run: { session, command: cmd.trim() } }, phase: this.phase(), undo: u ? { run: { session, command: u } } : undefined, tab, term, marker });
  }

  /** The undo rules of a device type: its own, else the built-in ones for its name. */
  private undoRules(deviceType: string) {
    const d = this.ui.settings()?.deviceTypes?.find(x => x.name.toLowerCase() === deviceType.toLowerCase());
    return d?.undoRules?.trim() ? d.undoRules : builtinUndo(deviceType);
  }

  /** A request sent in the API client; its status becomes the first check. */
  http(h: NonNullable<TestStep['http']>, status: number, ms: number, body: string) {
    if (!this.recording() || !h.url) return;
    const expect: TestExpect[] = status ? [{ status }] : [];
    const step: TestStep = { http: h, expect };
    let undo: TestStep | undefined;
    // a POST that created something: its DELETE undoes it (the id comes from the answer)
    if (this.phase() === 'steps' && (h.method || 'GET').toUpperCase() === 'POST' && status >= 200 && status < 300) {
      try {
        const j = JSON.parse(body), id = ['id', 'data.id', 'uuid', 'name'].find(k => k.split('.').reduce((o: any, p) => o?.[p], j) != null);
        if (id) {
          const v = 'created_' + (this.steps().length + 1);
          step.save = [{ var: v, json: id }];
          undo = { http: { session: h.session, environment: h.environment, collection: h.collection, method: 'DELETE', url: h.url.replace(/\/+$/, '') + '/{{' + v + '}}' }, expect: [{ status: 200 }] };
          undo.expect = [];
        }
      } catch { /* not JSON */ }
    }
    this.push({ step, phase: this.phase(), undo, output: body, status, ms });
  }

  /** An SNMP get, walk or set in the SNMP Browser; a get's values become checks (the readable ones). */
  snmp(s: NonNullable<TestStep['snmp']>, rows: { name: string; display: string }[], old?: { value: string; type?: string }) {
    if (!this.recording() || !s.session) return;
    const op = s.op === 'table' ? 'walk' : s.op;
    const expect: TestExpect[] = op === 'get' ? rows.slice(0, 5).map(r => ({ oid: r.name.replace(/^[\w-]+::/, ''), equals: r.display })) : [];
    // a set: setting the old value back undoes it
    const v = s.values?.[0], undo: TestStep | undefined = this.phase() === 'steps' && op === 'set' && v && old ? { snmp: { session: s.session, op: 'set', values: [{ oid: v.oid, type: old.type || v.type, value: old.value }] } } : undefined;
    this.push({ step: { snmp: { ...s, op }, expect }, phase: this.phase(), undo, output: rows.map(r => r.name + ' = ' + r.display).join('\n') });
  }

  /** Adds a step's suggested undo to the cleanup (the cleanup runs in reverse order of what it undoes). */
  addUndo(i: number) {
    const s = this.steps()[i];
    if (!s?.undo || s.undone) return;
    this.steps.update(l => {
      const c = l.map((x, k) => k === i ? { ...x, undone: true } : x);
      const firstClean = c.findIndex(x => x.phase === 'cleanup');
      const item: RecStep = { step: structuredClone(s.undo!), phase: 'cleanup' };
      if (firstClean < 0) c.push(item); else c.splice(firstClean, 0, item); // newest change undone first
      return c;
    });
  }
  /** Adds every suggested undo, the last change first. */
  addAllUndos() {
    const idx = this.steps().map((s, i) => s.undo && !s.undone ? i : -1).filter(i => i >= 0);
    for (const i of idx) this.addUndo(i);
  }
  toggleRestore(k: 'config' | 'lab', on: boolean) { this.restore.update(l => on ? [...new Set([...l, k])] : l.filter(x => x !== k)); }

  /** New steps go to the end of their phase: setup first, then the steps, then the cleanup. */
  private push(s: RecStep) {
    const order: RecPhase[] = ['setup', 'steps', 'cleanup'];
    this.steps.update(l => { const at = l.findIndex(x => order.indexOf(x.phase) > order.indexOf(s.phase)); const c = [...l]; if (at < 0) c.push(s); else c.splice(at, 0, s); return c; });
  }

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
    const pick = (p: RecPhase) => l.filter(s => s.phase === p).map(s => structuredClone(s.step));
    const setup = pick('setup'), steps = pick('steps'), cleanup = pick('cleanup');
    const extra = { ...(setup.length ? { setup } : {}), ...(cleanup.length ? { cleanup } : {}), ...(this.restore().length ? { restore: this.restore() } : {}) };
    if (!this.roles()) return { name: this.name().trim() || tr('New test'), ...extra, steps };
    const vars: Record<string, string> = {}, role = (p: string) => {
      if (!p || p.includes('{{')) return p;
      let r = roleName(p), i = 2;
      while (vars[r] && vars[r] !== p) r = roleName(p) + '_' + i++;
      vars[r] = p;
      return '{{' + r + '}}';
    };
    for (const s of [...setup, ...steps, ...cleanup]) {
      if (s.run) s.run.session = role(s.run.session);
      if (s.snmp) s.snmp.session = role(s.snmp.session);
      if (s.http?.session) s.http.session = role(s.http.session);
    }
    return { name: this.name().trim() || tr('New test'), vars, ...extra, steps };
  }

  /** Checks worth adding to a read step: the names and numbers of the configuration commands recorded before it
   *  on the same device - in the steps, what they created must be in the output (contains); in the cleanup, what was
   *  removed must be gone (does not contain). */
  suggest(i: number): TestExpect[] {
    const l = this.steps(), s = l[i], cmd = s?.step.run?.command ?? '';
    if (!s?.step.run || !isRead(s.step) || s.output == null) return [];
    const out = s.output, have = new Set((s.step.expect ?? []).map(e => e.contains ?? e.notContains));
    const toks = new Set<string>();
    for (const x of l.slice(0, i)) {
      if (!x.step.run || x.step.run.session !== s.step.run.session || isRead(x.step) || x.phase !== 'steps') continue;
      for (const t of x.step.run.command.split(/\s+/)) if (t.length >= 2 && /\d/.test(t) && /^[\w.:/-]+$/.test(t) && !(' ' + cmd + ' ').includes(' ' + t + ' ')) toks.add(t);
    }
    const res: TestExpect[] = [];
    for (const t of toks) {
      if (have.has(t)) continue;
      const there = new RegExp('(^|[^\\w])' + t.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&') + '($|[^\\w])', 'm').test(out);
      if (s.phase === 'cleanup' && !there) res.push({ notContains: t });
      else if (s.phase !== 'cleanup' && there) res.push({ contains: t });
      if (res.length >= 3) break;
    }
    return res;
  }
  addSuggested(i: number, e: TestExpect) { this.steps.update(l => l.map((x, j) => j === i ? { ...x, step: { ...x.step, expect: [...(x.step.expect ?? []), e] } } : x)); }

  async save(): Promise<boolean> {
    if (!this.steps().length) { this.ui.toast(tr('Nothing recorded yet'), 'info'); return false; }
    const bare = this.steps().filter(s => isRead(s.step) && !s.step.expect?.length).length;
    if (bare && !(await this.ui.confirm(tr('Save Without Checks?'), tr('{0} steps read the device but check nothing, so they pass whenever they run. Add a check: select the value and choose Expect this, or use a suggested check.', bare), tr('Save Anyway')))) return false;
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
    this.phase.set('steps');
    this.restore.set([]);
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

/** A variable name for a session path, as Termwright's testbeds name roles: Lab/core-sw1 → core_sw1. */
export function roleName(path: string) {
  const n = (path.split('/').pop() ?? '').toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '');
  return !n || /^\d/.test(n) ? 'dev_' + n : n;
}

/** The built-in undo rules by device type name (as Termwright's device types have them). */
const CISCO_UNDO = `^(show|ping|traceroute|exit|end|copy|write|save|terminal|enable|configure|conf t|do) =>
^interface .* =>
^router .* =>
^no (.+) => $1
^(.+) => no $1`;
const BUILTIN_UNDO: Record<string, string> = {
  exos: `^(show|ping|traceroute|save|top|clear) =>
^create vlan (\\S+).* => delete vlan $1
^configure vlan (\\S+) add ports (\\S+).* => configure vlan $1 delete ports $2
^configure vlan (\\S+) ipaddress .* => unconfigure vlan $1 ipaddress
^configure vlan (\\S+) tag .* =>
^create (\\S+) (\\S+).* => delete $1 $2
^enable (.+) => disable $1
^disable (.+) => enable $1`,
  voss: CISCO_UNDO, 'cisco ios': CISCO_UNDO, 'cisco nx-os': CISCO_UNDO, 'eltex mes': CISCO_UNDO, 'ruckus icx': CISCO_UNDO,
  'huawei vrp': `^(display|ping|tracert|save|quit|return|system-view|screen-length) =>
^interface .* =>
^undo (.+) => $1
^(.+) => undo $1`,
};
export function builtinUndo(deviceType: string) { return BUILTIN_UNDO[deviceType.toLowerCase()] ?? ''; }

/** The undo of a command by rules ("regex => undo" per line, first match wins; "" = none needed). */
export function undoOf(rules: string, cmd: string): string {
  cmd = cmd.trim();
  for (const line of rules.split('\n')) {
    const at = line.indexOf('=>');
    if (at < 0) continue;
    const pat = line.slice(0, at).trim(), undo = line.slice(at + 2).trim();
    if (!pat) continue;
    let re: RegExp;
    try { re = new RegExp(pat); } catch { continue; }
    const m = re.exec(cmd);
    if (!m) continue;
    return undo.replace(/\$(\d)/g, (_, d) => m[+d] ?? '');
  }
  return '';
}

/** A step that only reads: show/display commands, REST GET, SNMP get/walk. */
export function isRead(s: TestStep): boolean {
  if (s.run) return /^\s*(show|sh|display|dis|get|ping|traceroute)\b/i.test(s.run.command);
  if (s.http) return (s.http.method || 'GET').toUpperCase() === 'GET';
  if (s.snmp) return (s.snmp.op || 'get') !== 'set';
  return false;
}
