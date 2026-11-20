import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Recorder } from '../core/recorder.service';
import { TestExpect, TestStep } from '../core/models';

type AddKind = 'contains' | 'notContains' | 'matches' | 'field' | 'json' | 'status' | 'maxMs';

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
    <div class="hint">Type commands in terminal tabs or send requests in the API Client. Select output and right-click → <b>Expect this</b> to add a check.</div>
    <div class="list">
      @for (s of rec.steps(); track $index; let i = $index) {
        <div class="st">
          <div class="top"><b>{{ i + 1 }}</b>
            <span class="tg">{{ target(s.step) }} ›</span><span class="cmd">{{ sent(s.step) }}</span>
            <span class="sp"></span>
            <button type="button" class="ic" (click)="rec.moveStep(i, -1)" [disabled]="i === 0" title="Move up">↑</button>
            <button type="button" class="ic" (click)="rec.removeStep(i)" title="Remove the step">✕</button>
          </div>
          @for (e of s.step.expect ?? []; track $index; let j = $index) {
            <span class="chip">✓ {{ label(e) }}<button type="button" (click)="rec.removeCheck(i, j)" title="Remove the check">×</button></span>
          }
          @for (v of s.step.save ?? []; track $index; let j = $index) {
            <span class="chip var">{{ '{{' + v.var + '}}' }} ← {{ v.field || v.json }}<button type="button" (click)="rec.removeSave(i, j)" title="Remove">×</button></span>
          }
          @if (!(s.step.expect?.length)) { <div class="mut">no checks - passes when it runs without an error</div> }
          @if (adding() === i) {
            <div class="add">
              <select [(ngModel)]="kind">
                @for (k of kinds(s.step); track k.v) { <option [value]="k.v">{{ k.l }}</option> }
              </select>
              @if (kind === 'field' || kind === 'json') { <input [(ngModel)]="subject" [placeholder]="kind === 'json' ? 'data.items[0].name' : 'Uptime:\\s+(\\S+)'" /> }
              <input [(ngModel)]="value" [placeholder]="kind === 'status' ? '200' : kind === 'maxMs' ? '500' : kind === 'field' || kind === 'json' ? 'equals (empty: is there)' : 'text'" (keydown.enter)="addCheck(i)" />
              <div class="row"><button type="button" (click)="adding.set(-1)">Cancel</button><button type="button" class="pri" (click)="addCheck(i)">Add</button></div>
            </div>
          } @else {
            <button type="button" class="lnk" (click)="openAdd(i, s.step)">+ Check</button>
          }
        </div>
      } @empty { <div class="mut empty">No steps yet.</div> }
    </div>
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
    .top { width: 100%; } .top b { margin-right: 2px; } .tg { color: var(--muted); white-space: nowrap; } .cmd { font-family: var(--mono, monospace); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
    .ic { padding: 0 6px; height: 22px; } .chip { font-size: 11.5px; padding: 1px 4px 1px 8px; border-radius: 10px; background: color-mix(in srgb, var(--ok, #4ec07a) 15%, transparent); color: var(--ok, #4ec07a); display: inline-flex; gap: 4px; align-items: center; max-width: 100%; overflow: hidden; }
    .chip.var { background: color-mix(in srgb, var(--accent) 15%, transparent); color: var(--accent); } .chip button { border: 0; background: none; color: inherit; padding: 0 3px; cursor: pointer; }
    .lnk { border: 0; background: none; color: var(--accent); padding: 0; cursor: pointer; font-size: 12px; }
    .add { display: flex; flex-direction: column; gap: 5px; width: 100%; } .add .row { justify-content: flex-end; }
    .pri { background: var(--accent); color: #fff; border-color: transparent; } .empty { padding: 16px 0; text-align: center; }
  `],
})
export class RecorderPanel {
  readonly rec = inject(Recorder);
  readonly adding = signal(-1);
  kind: AddKind = 'contains';
  subject = '';
  value = '';

  target(s: TestStep) { return s.run?.session ?? s.http?.session ?? ''; }
  sent(s: TestStep) { return s.run?.command ?? `${s.http?.method || 'GET'} ${s.http?.url ?? ''}`; }
  kinds(s: TestStep): { v: AddKind; l: string }[] {
    const k: { v: AddKind; l: string }[] = [{ v: 'contains', l: 'Contains' }, { v: 'notContains', l: 'Does not contain' }, { v: 'matches', l: 'Matches (regular expression)' }, { v: 'field', l: 'Field (regular expression with a group)' }];
    return s.http ? [{ v: 'status', l: 'Status' }, { v: 'json', l: 'JSON path' }, ...k, { v: 'maxMs', l: 'Takes at most (ms)' }] : k;
  }
  openAdd(i: number, s: TestStep) { this.kind = s.http ? 'json' : 'contains'; this.subject = ''; this.value = ''; this.adding.set(i); }
  addCheck(i: number) {
    const v = this.value.trim(), sub = this.subject.trim();
    let e: TestExpect | null = null;
    switch (this.kind) {
      case 'status': e = +v ? { status: +v } : null; break;
      case 'maxMs': e = +v ? { maxMs: +v } : null; break;
      case 'json': case 'field': e = sub ? { [this.kind]: sub, ...(v ? { equals: v } : {}) } : null; break;
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
  if (e.field) return `${e.field} ${cmp || 'found'}`;
  if (e.maxMs) return `≤ ${e.maxMs} ms`;
  return '?';
}