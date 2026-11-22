import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Recorder, RecPhase, isRead } from '../core/recorder.service';
import { AIChat } from '../core/ai-chat.service';
import { TestExpect, TestTableExpect, TestStep } from '../core/models';
import { fieldName } from '../core/predict';

type AddKind = 'contains' | 'notContains' | 'matches' | 'field' | 'json' | 'oid' | 'status' | 'maxMs';

/** The Steps panel of the test recorder: the steps recorded so far with their checks. */
@Component({
  selector: 'app-recorder-panel',
  imports: [FormsModule],
  host: { class: 'recp', role: 'complementary', '[attr.aria-label]': "'Test Recorder'", '[style.width.px]': 'width()' },
  template: `
    <div class="grip" (mousedown)="drag($event)" title="Drag to change the width" role="separator" aria-orientation="vertical"></div>
    <div class="bar" [class.pz]="rec.paused()">
      <span class="dot" [class.pz]="rec.paused()"></span><b>{{ rec.paused() ? 'Paused' : 'Recording' }}</b>
      <span class="sp"></span>
      <button type="button" class="bb" (click)="rec.paused.set(!rec.paused())" [title]="rec.paused() ? 'Resume recording' : 'Pause: commands typed now are not recorded'">{{ rec.paused() ? '▶ Resume' : '⏸ Pause' }}</button>
      <button type="button" class="bb" (click)="rec.discard()">Discard</button>
      <button type="button" class="pri" (click)="rec.save()" [disabled]="!rec.steps().length">Save…</button>
    </div>
    <div class="pad">
      <input class="nm" [ngModel]="rec.name()" (ngModelChange)="rec.name.set($event)" placeholder="Test name" aria-label="Test name" />
      <div class="phases" role="tablist" aria-label="Record into">
        @for (p of phases; track p.k) { <button type="button" role="tab" [class.on]="rec.phase() === p.k" (click)="rec.phase.set(p.k)" [title]="p.t">{{ p.l }} <span class="n">{{ count(p.k) }}</span></button> }
      </div>
      <div class="hint">@switch (rec.phase()) {
        @case ('setup') { Setup puts the devices in a known state before the test. }
        @case ('cleanup') { Cleanup undoes what the test changed; it runs when the run is set to clean up. }
        @default { Commands, API requests and SNMP queries become steps. Select output and right-click → <b>Expect this</b>.@if (rec.autoChecks()) { Checks for what you configured are added by themselves. } }
      }</div>
    </div>
    <div class="list">
      @for (s of rec.steps(); track $index; let i = $index) {
        @if (i === 0 || rec.steps()[i - 1].phase !== s.phase) { <div class="ph">{{ phaseName(s.phase) }}</div> }
        <div class="st" [class.cl]="s.phase === 'cleanup'" [class.su]="s.phase === 'setup'" [class.read]="isReadStep(s.step)">
          <div class="top"><b>{{ numIn(i) }}</b>
            <span class="tg" [title]="target(s.step)">{{ short(target(s.step)) }}</span>
            <span class="sp"></span>
            @if (s.step.run && (s.term || s.output) && !s.table) { <button type="button" class="ic" (click)="rec.parseStep(i)" title="Show the output as a table (a TextFSM template from templates/) and check its values">⊞</button> }
            <button type="button" class="ic" (click)="rec.moveStep(i, -1)" [disabled]="i === 0" title="Move up">↑</button>
            <button type="button" class="ic" (click)="rec.removeStep(i)" title="Remove the step">✕</button>
          </div>
          <div class="cmd">{{ sent(s.step) }}</div>
          @if (s.table; as tb) {
            <div class="tbw"><table class="ptab"><tr>@for (h of tb.header; track $index) { <th>{{ h }}</th> }</tr>
              @for (r of tb.rows.slice(0, 40); track $index) { <tr>@for (c of r; track $index; let ci = $index) { <td (click)="rec.addCellCheck(i, r, ci)" title="Check this value">{{ c }}</td> }</tr> }</table></div>
            <div class="mut">{{ tb.rows.length }} rows · click a value to check it</div>
          }
          @if (s.step.expect?.length || s.step.save?.length) {
            <div class="chips">
              @for (e of s.step.expect ?? []; track $index; let j = $index) { <span class="chip">✓ {{ label(e) }}<button type="button" (click)="rec.removeCheck(i, j)" title="Remove the check">×</button></span> }
              @for (v of s.step.save ?? []; track $index; let j = $index) { <span class="chip var">{{ '{{' + v.var + '}}' }} ← {{ v.field || v.json || v.oid || v.column }}<button type="button" (click)="rec.removeSave(i, j)" title="Remove">×</button></span> }
            </div>
            @if (s.auto) { <div class="mut">✦ {{ s.auto }} {{ s.auto === 1 ? 'check' : 'checks' }} added from what was configured before it</div> }
          } @else if (isReadStep(s.step)) { <div class="mut warn">○ no checks yet - it passes whenever the device answers.@if (s.auto === 0) { No checks were added: {{ s.autoWhy }}. Select a value and right-click → Expect this. }</div> }
          @if (rec.suggest(i).length) {
            <div class="chips"><span class="mut">Suggested:</span>@for (x of rec.suggest(i); track $index) { <button type="button" class="sug" (click)="rec.addSuggested(i, x)" [title]="s.phase === 'cleanup' ? 'The cleanup removed it: check that it is gone' : 'A configuration command before this step set it: check that it is there'">+ {{ label(x) }}</button> }</div>
          }
          @if (s.undo && !s.undone) { <div class="undo"><span class="mut">↩ cleanup:</span> <code>{{ sent(s.undo) }}</code><button type="button" class="lnk" (click)="rec.addUndo(i)" title="Add this undo to the cleanup">+ add</button></div> }
          @if (adding() === i) {
            <div class="add">
              <select [(ngModel)]="kind">
                @for (k of kinds(s.step); track k.v) { <option [value]="k.v">{{ k.l }}</option> }
              </select>
              @if (kind === 'field' || kind === 'json' || kind === 'oid') { <input [(ngModel)]="subject" [placeholder]="kind === 'json' ? 'data.items[0].name' : kind === 'oid' ? 'ifOperStatus.5' : 'Uptime:\\s+(\\S+)'" /> }
              <input [(ngModel)]="value" [placeholder]="kind === 'status' ? '200' : kind === 'maxMs' ? '500' : kind === 'field' || kind === 'json' || kind === 'oid' ? 'equals (empty: is there)' : 'text'" (keydown.enter)="addCheck(i)" />
              <div class="row"><button type="button" (click)="adding.set(-1)">Cancel</button><button type="button" class="pri" (click)="addCheck(i)">Add</button></div>
            </div>
          } @else { <button type="button" class="lnk" (click)="openAdd(i, s.step)">+ Check</button> }
        </div>
      } @empty { <div class="mut empty">No steps yet.<br />Type a command in a terminal tab.</div> }
    </div>
    <div class="ft">
      @if (rec.undos()) { <div class="row"><span class="mut grow">↩ {{ rec.undos() }} {{ rec.undos() === 1 ? 'undo' : 'undos' }} suggested</span><button type="button" class="lnk" (click)="rec.addAllUndos()" title="Add every suggested undo to the cleanup, the last change first">Add all to Cleanup</button></div> }
      <div class="row"><span class="mut grow">{{ rec.steps().length }} steps · {{ rec.checks() }} checks</span>
        <button type="button" class="lnk" (click)="cleanupWithAI()" [disabled]="!rec.steps().length" title="Ask the AI assistant to write the cleanup for these steps">✦ Cleanup with AI</button>
        <button type="button" class="lnk" (click)="opts.set(!opts())">{{ opts() ? 'Hide options' : 'Options' }}</button></div>
      @if (opts()) {
        <div class="opts">
          <label title="Add the checks a show command (or an API request) most likely needs from what was configured before it"><input type="checkbox" [ngModel]="rec.autoChecks()" (ngModelChange)="rec.autoChecks.set($event)" /> Add checks for what was configured</label>
          <label title="Put the undo of each change (device type undo rules, the DELETE of a POST, the old value of an SNMP set) into the cleanup as it is recorded"><input type="checkbox" [ngModel]="rec.autoCleanup()" (ngModelChange)="rec.autoCleanup.set($event)" /> Add the undo of each change to the cleanup</label>
          <label title="Sessions are saved as {{ '{{' }}role{{ '}}' }} variables with these sessions as defaults, so a testbed can point the test at other devices"><input type="checkbox" [ngModel]="rec.roles()" (ngModelChange)="rec.roles.set($event)" /> Sessions as roles</label>
          <span class="mut">Afterwards put back:</span>
          <label title="Save the configuration before the test and restore it afterwards (the device type's save and restore commands)"><input type="checkbox" [ngModel]="rec.restore().includes('config')" (ngModelChange)="rec.toggleRestore('config', $event)" /> configuration</label>
          <label title="Take a checkpoint of each emulated lab switch before the test and go back to it afterwards"><input type="checkbox" [ngModel]="rec.restore().includes('lab')" (ngModelChange)="rec.toggleRestore('lab', $event)" /> lab snapshot</label>
        </div>
      }
    </div>`,
  styles: [`
    :host { position: relative; flex: none; width: 640px; min-width: 320px; max-width: 70vw; display: flex; flex-direction: column; min-height: 0; background: var(--panel); border-left: 1px solid var(--border); color: var(--text); font-size: calc(13px * var(--ui-scale, 1)); }
    .grip { position: absolute; left: -5px; top: 0; bottom: 0; width: 10px; cursor: col-resize; z-index: 20; display: flex; align-items: center; justify-content: center; }
    .grip::after { content: ''; width: 4px; height: 44px; border-radius: 3px; background: color-mix(in srgb, var(--text) 28%, transparent); }
    .grip:hover::after, .grip:active::after { background: var(--accent); height: 100%; border-radius: 0; width: 3px; }
    .top, .row { display: flex; align-items: center; gap: 6px; } .sp, .grow { flex: 1; }
    .bar { display: flex; align-items: center; gap: 6px; padding: 8px 10px; background: color-mix(in srgb, #ef5b5b 16%, var(--panel)); border-bottom: 1px solid color-mix(in srgb, #ef5b5b 40%, var(--border)); }
    .bar.pz { background: color-mix(in srgb, var(--text) 6%, var(--panel)); border-bottom-color: var(--border); } .bb { font-size: 12px; padding: 2px 9px; }
    .dot { width: 9px; height: 9px; border-radius: 50%; background: #ef5b5b; box-shadow: 0 0 0 4px rgba(239, 91, 91, .2); animation: blink 1.4s infinite; } .dot.pz { animation: none; background: var(--muted); box-shadow: none; }
    @keyframes blink { 50% { opacity: .35; } }
    .pad { display: flex; flex-direction: column; gap: 8px; padding: 10px; border-bottom: 1px solid var(--border); }
    .nm { height: 30px; } .hint, .mut { color: var(--muted); font-size: 12px; line-height: 1.45; } .warn { color: var(--warn, #e0a030); }
    .list { flex: 1; overflow: auto; display: flex; flex-direction: column; gap: 8px; min-height: 0; padding: 10px; }
    .st { border: 1px solid var(--border); border-radius: 9px; padding: 8px 10px; display: flex; flex-direction: column; gap: 5px; align-items: stretch; background: color-mix(in srgb, var(--text) 3%, transparent); }
    .st.read { }
    .top b { color: var(--muted); font-weight: 500; } .tg { color: var(--muted); white-space: nowrap; min-width: 0; overflow: hidden; text-overflow: ellipsis; font-size: 12px; } .cmd { font-family: var(--mono, monospace); white-space: pre-wrap; overflow-wrap: anywhere; }
    .ic { padding: 0 6px; height: 22px; flex: none; } .chips { display: flex; flex-wrap: wrap; gap: 4px; align-items: center; }
    .chip { font-size: 11.5px; padding: 1px 4px 1px 8px; border-radius: 10px; background: color-mix(in srgb, var(--ok, #4ec07a) 15%, transparent); color: var(--ok, #4ec07a); display: inline-flex; gap: 4px; align-items: center; max-width: 100%; overflow: hidden; }
    .chip.var { background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent); } .chip button { border: 0; background: none; color: inherit; padding: 0 3px; cursor: pointer; }
    .lnk { border: 0; background: none; color: var(--accent); padding: 0; cursor: pointer; font-size: 12px; align-self: flex-start; }
    .add { display: flex; flex-direction: column; gap: 5px; } .add .row { justify-content: flex-end; }
    .pri { background: var(--accent); color: #fff; border-color: transparent; } .empty { padding: 24px 0; text-align: center; }
    .phases { display: flex; gap: 2px; background: color-mix(in srgb, var(--text) 6%, transparent); border-radius: 7px; padding: 2px; } .phases button { flex: 1; border: 0; background: none; color: var(--text); padding: 4px 6px; border-radius: 5px; cursor: pointer; } .phases button.on { background: color-mix(in srgb, var(--accent) 28%, transparent); } .phases .n { color: var(--muted); font-size: 11px; }
    .ph { font-size: 11px; letter-spacing: .06em; color: var(--muted); text-transform: uppercase; margin-top: 2px; } .st.cl { border-color: color-mix(in srgb, var(--warn, #e0a030) 45%, var(--border)); } .st.su { border-style: dashed; }
    .sug { font-size: 11.5px; padding: 1px 7px; border: 1px dashed color-mix(in srgb, var(--accent) 50%, transparent); border-radius: 10px; background: none; color: var(--accent); cursor: pointer; }
    .tbw { max-height: 180px; overflow: auto; border: 1px solid var(--border); border-radius: 6px; } .ptab { border-collapse: collapse; font: 11.5px var(--mono, monospace); width: 100%; } .ptab th { position: sticky; top: 0; background: var(--panel); text-align: left; padding: 2px 6px; color: var(--muted); font-weight: 500; } .ptab td { padding: 2px 6px; cursor: pointer; border-top: 1px solid var(--border); white-space: nowrap; } .ptab td:hover { background: color-mix(in srgb, var(--accent) 20%, transparent); }
    .undo { font-size: 12px; display: flex; gap: 6px; align-items: center; } .undo code { font-family: var(--mono, monospace); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; flex: 1; }
    .ft { display: flex; flex-direction: column; gap: 6px; padding: 9px 10px; border-top: 1px solid var(--border); }
    .opts { display: flex; flex-direction: column; gap: 5px; font-size: 12px; } .opts label { display: flex; gap: 6px; align-items: center; }
  `],
})
export class RecorderPanel {
  readonly rec = inject(Recorder);
  private readonly ai = inject(AIChat);
  readonly phases: { k: RecPhase; l: string; t: string }[] = [
    { k: 'setup', l: 'Setup', t: 'Record steps that run before the test' },
    { k: 'steps', l: 'Steps', t: 'Record the test itself' },
    { k: 'cleanup', l: 'Cleanup', t: 'Record steps that undo the test - they always run' }];
  /** A step's number within its section (setup, steps, cleanup each start at 1). */
  numIn(i: number) { const l = this.rec.steps(), p = l[i].phase; return l.slice(0, i + 1).filter(s => s.phase === p).length; }
  count(p: RecPhase) { return this.rec.steps().filter(s => s.phase === p).length; }
  phaseName(p: RecPhase) { return p === 'setup' ? 'Setup' : p === 'cleanup' ? 'Cleanup - always runs' : 'Steps'; }
  /** The AI assistant writes the cleanup: the recorded steps with their sessions' device types. */
  cleanupWithAI() {
    const l = this.rec.steps().filter(s => s.phase !== 'cleanup').map((s, i) => `${i + 1}. [${s.phase}] ${this.target(s.step)}: ${stepText(s.step)}`).join('\n');
    this.ai.currentId.set('');
    this.ai.start({});
    this.ai.addFile('Recorded steps', l);
    void this.ai.send('Write the cleanup for these recorded Termwright test steps: the commands, REST requests or SNMP sets that undo every change they make, in the right order and in the right configuration mode for each device type (find the sessions to see their device types). Answer with a YAML cleanup: list I can paste into the test, and say which changes cannot be undone.');
  }
  readonly adding = signal(-1);
  readonly opts = signal(false);
  readonly width = signal((() => { try { return Number(localStorage.getItem('tw.rec.width')) || 640; } catch { return 640; } })());
  /** Drag the left edge to change the width (kept for next time). */
  drag(ev: MouseEvent) {
    ev.preventDefault();
    const x0 = ev.clientX, w0 = this.width();
    const move = (e: MouseEvent) => this.width.set(Math.max(320, Math.min(window.innerWidth * 0.7, w0 + x0 - e.clientX)));
    const up = () => { document.removeEventListener('mousemove', move); document.removeEventListener('mouseup', up); try { localStorage.setItem('tw.rec.width', String(this.width())); } catch { /* not kept */ } };
    document.addEventListener('mousemove', move); document.addEventListener('mouseup', up);
  }
  isReadStep = isRead;
  kind: AddKind = 'contains';
  subject = '';
  value = '';

  target(s: TestStep) { return s.run?.session ?? s.http?.session ?? s.snmp?.session ?? ''; }
  sent(s: TestStep) { return stepText(s); }
  /** The session's name (the last part of its path); the full path is the tooltip. */
  short(p: string) { return p.split('/').pop() ?? p; }
  kinds(s: TestStep): { v: AddKind; l: string }[] {
    const k: { v: AddKind; l: string }[] = [{ v: 'contains', l: 'Contains' }, { v: 'notContains', l: 'Does not contain' }, { v: 'matches', l: 'Matches (regular expression)' }, { v: 'field', l: 'Field (regular expression with a group)' }];
    if (s.snmp) return [{ v: 'oid', l: 'SNMP value (name or OID)' }, ...k];
    return s.http ? [{ v: 'status', l: 'Status' }, { v: 'json', l: 'JSON path' }, ...k, { v: 'maxMs', l: 'Takes at most (ms)' }] : k;
  }
  openAdd(i: number, s: TestStep) { this.kind = s.http ? 'json' : s.snmp ? 'oid' : 'contains'; this.subject = ''; this.value = ''; this.adding.set(i); }
  addCheck(i: number) {
    const v = this.value.trim(), sub = this.subject.trim();
    let e: TestExpect | null = null;
    switch (this.kind) {
      case 'status': e = +v ? { status: +v } : null; break;
      case 'maxMs': e = +v ? { maxMs: +v } : null; break;
      case 'json': case 'field': case 'oid': e = sub ? { [this.kind]: sub, ...(v ? { equals: v } : {}) } : null; break;
      default: e = v ? { [this.kind]: v } : null;
    }
    if (!e) return;
    this.rec.addCheck(i, e);
    this.adding.set(-1);
  }
  label(e: TestExpect) { return expectLabel(e); }

}

/** "ID=200 → STATE = active", "rows STATE=active: 2". */
export function tableLabel(t: TestTableExpect): string {
  const w = Object.entries(t.where ?? {}).map(([k, v]) => `${k}=${v}`).join(' ');
  const row = w ? `row ${w}` : 'rows';
  if (t.rows !== undefined) return `${row}: ${t.rows} rows`;
  if (!t.column) return `${row} ${t.exists === false ? 'is not there' : 'is there'}`;
  const cmp = t.matches ? `~ ${t.matches}` : t.equals !== undefined ? `= ${t.equals}` : t.notEquals !== undefined ? `≠ ${t.notEquals}` : t.greater !== undefined ? `> ${t.greater}` : t.less !== undefined ? `< ${t.less}` : 'is there';
  return `${row} → ${t.column} ${cmp}`;
}
/** Reads "ID=200, NAME=v2: STATE = active" (also "~ regex", "!= x", "> 5", "rows 2", "missing"). */
export function parseTableCheck(text: string): TestTableExpect | null {
  const i = text.indexOf(':');
  const wherePart = i >= 0 ? text.slice(0, i) : '', rest = (i >= 0 ? text.slice(i + 1) : text).trim();
  const where: Record<string, string> = {};
  for (const p of wherePart.split(',')) { const m = p.match(/^\s*([\w.-]+)\s*=\s*(.*?)\s*$/); if (m) where[m[1]] = m[2]; else if (p.trim()) return null; }
  const t: TestTableExpect = Object.keys(where).length ? { where } : {};
  let m: RegExpMatchArray | null;
  if ((m = rest.match(/^rows\s+(\d+)$/i))) return { ...t, rows: Number(m[1]) };
  if (/^(missing|not there)$/i.test(rest)) return { ...t, exists: false };
  if (/^(exists|there|)$/i.test(rest)) return t;
  if (!(m = rest.match(/^([\w.-]+)\s*(=|!=|≠|~|>|<)\s*(.*)$/))) return null;
  const [, col, op, v] = m;
  const c: TestTableExpect = { ...t, column: col };
  if (op === '=') c.equals = v; else if (op === '~') c.matches = v; else if (op === '>' || op === '<') { const n = Number(v); if (!Number.isFinite(n)) return null; if (op === '>') c.greater = n; else c.less = n; } else c.notEquals = v;
  return c;
}
export function expectLabel(e: TestExpect): string {
  if (e.table) return tableLabel(e.table);
  if (e.schema) return e.schema.trim().startsWith('{') ? 'fits the JSON schema' : `fits ${e.schema}`;
  if (e.agg) {
    const cmp = [e.equals !== undefined ? `= ${e.equals}` : '', e.greater !== undefined ? `> ${e.greater}` : '', e.less !== undefined ? `< ${e.less}` : ''].filter(Boolean).join(' ');
    const subj = e.field ? fieldName(e.field) : e.json ?? e.oid ?? '';
    return `${({ min: 'minimum', max: 'maximum', avg: 'average', delta: 'change', rate: 'rate/s' } as Record<string, string>)[e.agg] ?? e.agg} of ${subj} ${cmp}`;
  }
  if (e.field && e.equals !== undefined && fieldName(e.field) !== e.field) return `${fieldName(e.field)} = ${e.equals}`;
  const cmp = [e.equals !== undefined ? `= ${e.equals}` : '', e.notEquals !== undefined ? `≠ ${e.notEquals}` : '', e.greater !== undefined ? `> ${e.greater}` : '', e.less !== undefined ? `< ${e.less}` : '', e.exists === false ? 'missing' : ''].filter(Boolean).join(' ');
  if (e.contains) return `contains "${e.contains}"`;
  if (e.notContains) return `no "${e.notContains}"`;
  if (e.matches) return `matches ${e.matches}`;
  if (e.status) return `status ${e.status}`;
  if (e.json) return `${e.json} ${cmp || 'is there'}`;
  if (e.oid) return `${e.oid} ${cmp || 'is there'}`;
  if (e.field) return `${e.field} ${cmp || 'found'}`;
  if (e.maxMs) return `≤ ${e.maxMs} ms`;
  return '?';
}
/** What a step sends, for lists: the command, "GET /path" or "walk IF-MIB::ifTable". */
export function stepText(s: TestStep): string {
  if (s.call) return `${s.call.proc}(${Object.entries(s.call.with ?? {}).map(([k, v]) => `${k}=${v}`).join(', ')})`;
  if (s.tgen) return `${s.tgen.kind === 'ixnetwork' ? 'IxNetwork' : 'Spirent TestCenter'} traffic ${s.tgen.duration || 10} s${s.tgen.config ? ' · ' + s.tgen.config : ''}`;
  if (s.web) return `web ${s.web.url ?? ''}${s.web.actions?.length ? ' (' + s.web.actions.length + ' actions)' : ''}`.trim();
  if (s.netconf) return `netconf ${s.netconf.op || 'get'}${s.netconf.op === 'edit-config' ? ' ' + (s.netconf.target || 'running') : s.netconf.op === 'get-config' ? ' ' + (s.netconf.source || 'running') : ''}`;
  if (s.loop) return `for each {{${s.loop.var}}} in ${s.loop.values?.join(', ') ?? s.loop.range ?? s.loop.from ?? ''}: ${s.loop.steps.map(stepText).join('; ')}`;
  if (s.manual) return s.manual.instruction;
  if (s.traffic) return `traffic test ${s.traffic.test}`;
  if (s.upgrade) return `upgrade ${s.upgrade.session} to ${s.upgrade.image}`;
  if (!s.run && !s.http && !s.snmp) return s.wait ? `wait ${s.wait} s` : '';
  if (s.sample && s.sample.count > 1) return stepText({ ...s, sample: undefined }) + ` × ${s.sample.count}`;
  if (s.run) return s.run.command;
  if (s.snmp) return s.snmp.op === 'set' ? 'set ' + (s.snmp.values ?? []).map(v => `${v.oid} = ${v.value}`).join(', ') : `${s.snmp.op || 'get'} ${(s.snmp.oids ?? []).join(' ')}`;
  return `${s.http?.method || 'GET'} ${s.http?.url ?? ''}`;
}
