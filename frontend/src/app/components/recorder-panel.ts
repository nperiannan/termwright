import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Recorder, RecPhase } from '../core/recorder.service';
import { AIChat } from '../core/ai-chat.service';
import { TestExpect, TestStep } from '../core/models';

type AddKind = 'contains' | 'notContains' | 'matches' | 'field' | 'json' | 'oid' | 'status' | 'maxMs';

/** The Steps panel of the test recorder: the steps recorded so far with their checks. */
@Component({
  selector: 'app-recorder-panel',
  imports: [FormsModule],
  host: { class: 'recp', role: 'complementary', '[attr.aria-label]': "'Test Recorder'" },
  template: `
    <div class="hd">
      <span class="dot" [class.pz]="rec.paused()"></span><b>{{ rec.paused() ? 'Paused' : 'Recording' }}</b>
      <span class="sp"></span>
      <button type="button" (click)="rec.paused.set(!rec.paused())" [title]="rec.paused() ? 'Resume recording' : 'Pause: commands typed now are not recorded'">{{ rec.paused() ? '▶' : '⏸' }}</button>
    </div>
    <input class="nm" [ngModel]="rec.name()" (ngModelChange)="rec.name.set($event)" placeholder="Test name" aria-label="Test name" />
    <div class="phases" role="tablist" aria-label="Record into">
      @for (p of phases; track p.k) { <button type="button" role="tab" [class.on]="rec.phase() === p.k" (click)="rec.phase.set(p.k)" [title]="p.t">{{ p.l }} <span class="n">{{ count(p.k) }}</span></button> }
    </div>
    <div class="hint">@switch (rec.phase()) {
      @case ('setup') { <b>Setup</b>: steps that put the devices in a known state before the test. }
      @case ('cleanup') { <b>Cleanup</b>: steps that undo what the test changed - they always run, also after a failure, so the next test finds a clean device. }
      @default { Type commands in terminal tabs, send requests in the API Client or use the SNMP Browser. Select output and right-click → <b>Expect this</b> to add a check. }
    }</div>
    <div class="list">
      @for (s of rec.steps(); track $index; let i = $index) {
        @if (i === 0 || rec.steps()[i - 1].phase !== s.phase) { <div class="ph">{{ phaseName(s.phase) }}</div> }
        <div class="st" [class.cl]="s.phase === 'cleanup'" [class.su]="s.phase === 'setup'">
          <div class="top"><b>{{ numIn(i) }}</b>
            <span class="tg" [title]="target(s.step)">{{ short(target(s.step)) }}</span>
            <span class="sp"></span>
            <button type="button" class="ic" (click)="rec.moveStep(i, -1)" [disabled]="i === 0" title="Move up">↑</button>
            <button type="button" class="ic" (click)="rec.removeStep(i)" title="Remove the step">✕</button>
          </div>
          <div class="cmd">{{ sent(s.step) }}</div>
          @for (e of s.step.expect ?? []; track $index; let j = $index) {
            <span class="chip">✓ {{ label(e) }}<button type="button" (click)="rec.removeCheck(i, j)" title="Remove the check">×</button></span>
          }
          @for (v of s.step.save ?? []; track $index; let j = $index) {
            <span class="chip var">{{ '{{' + v.var + '}}' }} ← {{ v.field || v.json || v.oid }}<button type="button" (click)="rec.removeSave(i, j)" title="Remove">×</button></span>
          }
          @if (!(s.step.expect?.length)) { <div class="mut">no checks - passes when it runs without an error</div> }
          @for (x of rec.suggest(i); track $index) { <button type="button" class="lnk sug" (click)="rec.addSuggested(i, x)" [title]="s.phase === 'cleanup' ? 'The cleanup removed it: check that it is gone' : 'A configuration command before this step created it: check that it is there'">+ {{ label(x) }}</button> }
          @if (s.undo && !s.undone) { <div class="undo"><span class="mut">↩ undo:</span> <code>{{ sent(s.undo) }}</code><button type="button" class="lnk" (click)="rec.addUndo(i)" title="Add this undo to the cleanup">+ Cleanup</button></div> }
          @if (adding() === i) {
            <div class="add">
              <select [(ngModel)]="kind">
                @for (k of kinds(s.step); track k.v) { <option [value]="k.v">{{ k.l }}</option> }
              </select>
              @if (kind === 'field' || kind === 'json' || kind === 'oid') { <input [(ngModel)]="subject" [placeholder]="kind === 'json' ? 'data.items[0].name' : kind === 'oid' ? 'ifOperStatus.5' : 'Uptime:\\s+(\\S+)'" /> }
              <input [(ngModel)]="value" [placeholder]="kind === 'status' ? '200' : kind === 'maxMs' ? '500' : kind === 'field' || kind === 'json' || kind === 'oid' ? 'equals (empty: is there)' : 'text'" (keydown.enter)="addCheck(i)" />
              <div class="row"><button type="button" (click)="adding.set(-1)">Cancel</button><button type="button" class="pri" (click)="addCheck(i)">Add</button></div>
            </div>
          } @else {
            <button type="button" class="lnk" (click)="openAdd(i, s.step)">+ Check</button>
          }
        </div>
      } @empty { <div class="mut empty">No steps yet.</div> }
    </div>
    <div class="rst"><span class="mut">Afterwards put back:</span>
      <label title="Save the configuration before the test and restore it after the cleanup (the device type's save and restore commands)"><input type="checkbox" [ngModel]="rec.restore().includes('config')" (ngModelChange)="rec.toggleRestore('config', $event)" /> configuration</label>
      <label title="Take a checkpoint of each emulated lab switch before the test and go back to it after the cleanup"><input type="checkbox" [ngModel]="rec.restore().includes('lab')" (ngModelChange)="rec.toggleRestore('lab', $event)" /> lab snapshot</label></div>
    <div class="row"><button type="button" (click)="rec.addAllUndos()" [disabled]="!rec.undos()" title="Add every suggested undo to the cleanup, the last change first">↩ Undo All in Cleanup ({{ rec.undos() }})</button>
      <button type="button" (click)="cleanupWithAI()" [disabled]="!rec.steps().length" title="Ask the AI assistant to write the cleanup for these steps">✦ Cleanup with AI</button></div>
    <label class="roles" title="Sessions are saved as {{ '{{' }}role{{ '}}' }} variables with these sessions as defaults, so a testbed can point the test at other devices"><input type="checkbox" [ngModel]="rec.roles()" (ngModelChange)="rec.roles.set($event)" /> Sessions as roles</label>
    <div class="ft">
      <span class="mut">{{ rec.steps().length }} steps · {{ rec.checks() }} checks</span><span class="sp"></span>
      <button type="button" (click)="rec.discard()">Discard</button>
      <button type="button" class="pri" (click)="rec.save()" [disabled]="!rec.steps().length">Save Test…</button>
    </div>`,
  styles: [`
    :host { flex: none; width: 320px; display: flex; flex-direction: column; gap: 8px; min-height: 0; padding: 10px; background: var(--panel); border-left: 1px solid var(--border); color: var(--text); font-size: calc(13px * var(--ui-scale, 1)); }
    .hd, .top, .ft, .row { display: flex; align-items: center; gap: 6px; } .sp { flex: 1; }
    .dot { width: 9px; height: 9px; border-radius: 50%; background: #ef5b5b; box-shadow: 0 0 0 4px rgba(239, 91, 91, .2); animation: blink 1.4s infinite; } .dot.pz { animation: none; background: var(--muted); box-shadow: none; }
    @keyframes blink { 50% { opacity: .35; } }
    .nm { height: 30px; } .hint, .mut { color: var(--muted); font-size: 12px; line-height: 1.45; }
    .list { flex: 1; overflow: auto; display: flex; flex-direction: column; gap: 8px; min-height: 0; }
    .st { border: 1px solid var(--border); border-radius: 8px; padding: 7px 8px; display: flex; flex-direction: column; gap: 4px; align-items: flex-start; }
    .top { width: 100%; } .top .ic { flex: none; } .top b { margin-right: 2px; } .tg { color: var(--muted); white-space: nowrap; min-width: 0; overflow: hidden; text-overflow: ellipsis; } .cmd { font-family: var(--mono, monospace); white-space: pre-wrap; overflow-wrap: anywhere; width: 100%; }
    .ic { padding: 0 6px; height: 22px; } .chip { font-size: 11.5px; padding: 1px 4px 1px 8px; border-radius: 10px; background: color-mix(in srgb, var(--ok, #4ec07a) 15%, transparent); color: var(--ok, #4ec07a); display: inline-flex; gap: 4px; align-items: center; max-width: 100%; overflow: hidden; }
    .chip.var { background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent); } .chip button { border: 0; background: none; color: inherit; padding: 0 3px; cursor: pointer; }
    .lnk { border: 0; background: none; color: var(--accent); padding: 0; cursor: pointer; font-size: 12px; }
    .add { display: flex; flex-direction: column; gap: 5px; width: 100%; } .add .row { justify-content: flex-end; }
    .pri { background: var(--accent); color: #fff; border-color: transparent; } .empty { padding: 16px 0; text-align: center; }
    .phases { display: flex; gap: 2px; background: color-mix(in srgb, var(--text) 6%, transparent); border-radius: 7px; padding: 2px; } .phases button { flex: 1; border: 0; background: none; color: var(--text); padding: 4px 6px; border-radius: 5px; cursor: pointer; } .phases button.on { background: color-mix(in srgb, var(--accent) 28%, transparent); } .phases .n { color: var(--muted); font-size: 11px; }
    .ph { font-size: 11px; letter-spacing: .06em; color: var(--muted); text-transform: uppercase; margin-top: 2px; } .st.cl { border-color: color-mix(in srgb, var(--warn, #e0a030) 45%, var(--border)); } .st.su { border-style: dashed; }
    .sug { font-size: 12px; padding: 1px 4px; margin: 3px 4px 0 0; border: 1px dashed color-mix(in srgb, var(--accent) 50%, transparent); border-radius: 10px; }
    .undo { font-size: 12px; display: flex; gap: 6px; align-items: center; width: 100%; } .undo code { font-family: var(--mono, monospace); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; flex: 1; }
    .rst { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; font-size: 12px; } .rst label { display: flex; gap: 4px; align-items: center; } .row { display: flex; gap: 6px; } .row button { flex: 1; font-size: 12px; } .roles { display: flex; gap: 6px; align-items: center; font-size: 12px; color: var(--muted); }
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

export function expectLabel(e: TestExpect): string {
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
  if (s.run) return s.run.command;
  if (s.snmp) return s.snmp.op === 'set' ? 'set ' + (s.snmp.values ?? []).map(v => `${v.oid} = ${v.value}`).join(', ') : `${s.snmp.op || 'get'} ${(s.snmp.oids ?? []).join(' ')}`;
  return `${s.http?.method || 'GET'} ${s.http?.url ?? ''}`;
}
