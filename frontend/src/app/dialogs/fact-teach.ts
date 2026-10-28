import { Component, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { FACT_EVERY, FACT_NAMES, FactRule } from '../core/models';
import { factRegex, testFactRegex } from '../core/fact-regex';
import { Ui } from '../core/ui.service';

/** Save Selection as Device Fact: the user highlights a value (the version
 *  in "show version") and names it; the command and a regular expression
 *  are made from the output and saved with the device type, so the value
 *  is read for every session of that type. */
@Component({
  selector: 'app-fact-teach',
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop" [class.nested]="embedded()">
    <form class="ft" (ngSubmit)="save()" (keydown.escape)="close()">
      @if (input(); as i) {
        <div class="hd">
          <div class="ic"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 6h10M4 12h7M4 18h5"/><circle cx="17" cy="15" r="3.5"/><path d="M19.6 17.6 22 20"/></svg></div>
          <div class="tt"><div class="t">Save as Device Fact</div>
            <div class="s">Read for every <b>{{ dtName() || 'device type' }}</b> session after it connects - in the background, nothing is typed in your tab</div></div>
          <button type="button" class="x" title="Close (Esc)" (click)="close()">✕</button>
        </div>
        <div class="bd">
          <div>
            <div class="lbl">From the output</div>
            <div class="out">@if (i.command) {<div class="cmd">{{ i.command }}</div>}@for (l of context(); track $index) {<div [class.cur]="l.cur">@if (l.cur) {{{ l.pre }}<mark>{{ l.val }}</mark>{{ l.post }}} @else {{{ l.text }}}</div>}</div>
          </div>
          <div>
            <div class="lbl">This is the</div>
            <div class="chips">
              @for (n of names; track n) { <button type="button" class="chip" [class.on]="namePick() === n" (click)="pickName(n)">@if (namePick() === n) {✓ }{{ n }}</button> }
              @if (namePick()) { <button type="button" class="chip add" (click)="pickName('')">+ Other…</button> }
              @else { <input class="chip on other" name="nmo" [(ngModel)]="customName" placeholder="Name, e.g. Boot ROM" autofocus /> }
            </div>
          </div>
          @if (!i.deviceType) {
            <div><div class="lbl">Device type</div>
              <select name="dt" [(ngModel)]="pickedType"><option value="">(choose the device type)</option>
                @for (d of ui.settings()?.deviceTypes ?? []; track d.name) { <option [value]="d.name">{{ d.name }}</option> }</select></div>
          }
          <div class="two">
            <div><div class="lbl">Command</div><label class="field"><span class="k">Run</span><input name="cmd" [(ngModel)]="command" placeholder="e.g. show sys-info" spellcheck="false" /></label></div>
            <div><div class="lbl">Read it</div>
              <div class="seg"><button type="button" [class.on]="every" (click)="every = true">Every connect</button><button type="button" [class.on]="!every" (click)="every = false">Once</button></div></div>
          </div>
          <div class="res">
            @if (found() === selected()) { <span class="ok">✓ Finds {{ found() }}</span> }
            @else if (found() === null) { <span class="bad">✗ Not found in the output</span> }
            @else { <span class="bad">✗ Finds "{{ found() }}"</span> }
            <span class="s">in the output of {{ command || 'the command' }}</span>
            <button type="button" class="pt" (click)="showPattern.set(!showPattern())">Pattern {{ showPattern() ? '▾' : '▸' }}</button>
          </div>
          @if (showPattern() || found() !== selected()) {
            <div>
              <textarea name="re" class="re" rows="2" [ngModel]="regex()" (ngModelChange)="regex.set($event); edited = true" spellcheck="false"></textarea>
              <div class="hint">Made from the highlighted line. The first ( ) group is the value - change it only if needed.</div>
            </div>
          }
        </div>
        <div class="foot">
          <span class="spacer"></span>
          <button type="button" (click)="close()">Cancel</button>
          <button type="submit" class="primary" [disabled]="!canSave()">Save Fact</button>
        </div>
      }
    </form>
  </div>`,
  styles: [`
    .ft { width: 580px; max-width: calc(100vw - 32px); max-height: calc(100vh - 40px); overflow: auto; background: var(--surface, var(--panel));
      border: 1px solid var(--line, var(--border)); border-radius: var(--r-lg, 9px); box-shadow: var(--shadow); }
    .hd { display: flex; gap: 12px; align-items: center; padding: 16px 18px 12px; }
    .ic { width: 34px; height: 34px; flex: none; border-radius: 8px; background: var(--accent-soft); color: var(--accent); display: grid; place-items: center; svg { width: 18px; height: 18px; } }
    .tt { flex: 1; min-width: 0; } .t { font-weight: 650; font-size: calc(15px * var(--ui-scale, 1)); }
    .s { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); }
    .x { align-self: flex-start; border: 0; background: transparent !important; color: var(--muted); height: 24px; padding: 0 4px; &:hover { color: var(--text); } }
    .bd { padding: 2px 18px 16px; display: flex; flex-direction: column; gap: 14px; }
    .lbl { font-size: calc(11px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .06em; text-transform: uppercase; color: var(--faint); margin-bottom: 6px; }
    .out { background: var(--term-bg, var(--bg)); border: 1px solid var(--line, var(--border)); border-radius: var(--r, 6px); padding: 9px 12px;
      font: calc(12px * var(--ui-scale, 1))/1.65 var(--mono); color: var(--muted); white-space: pre; overflow-x: auto;
      .cmd { color: var(--faint); } .cur { color: var(--text); }
      mark { background: var(--accent); color: #fff; border-radius: 3px; padding: 0 2px; } }
    .chips { display: flex; gap: 6px; flex-wrap: wrap; }
    .chip { height: 28px; padding: 0 12px; border-radius: var(--r, 6px); border: 1px solid var(--line, var(--border)); background: var(--surface-2, var(--tab)); font-weight: 500;
      &.on { border-color: var(--accent); background: var(--accent-soft); color: var(--accent); }
      &.add { color: var(--muted); border-style: dashed; }
      &.other { width: 170px; color: var(--text); } }
    select { width: 100%; height: 32px; }
    .two { display: grid; grid-template-columns: 1fr auto; gap: 14px; align-items: end; }
    .field { height: 32px; border: 1px solid var(--border); border-radius: var(--r, 6px); background: var(--input-bg); display: flex; align-items: center; gap: 8px; padding: 0 0 0 10px;
      &:focus-within { border-color: var(--accent); }
      .k { font-size: calc(11px * var(--ui-scale, 1)); color: var(--faint); text-transform: uppercase; letter-spacing: .05em; }
      input { flex: 1; min-width: 0; border: 0; background: transparent; height: 100%; padding: 0 8px 0 0; font-family: var(--mono); &:focus { border: 0; } } }
    .seg { display: inline-flex; padding: 2px; border: 1px solid var(--line, var(--border)); border-radius: var(--r, 6px); background: var(--surface-2, var(--tab));
      button { height: 26px; padding: 0 12px; border: 0; border-radius: var(--r-sm, 5px); background: transparent !important; color: var(--muted); font-weight: 500; white-space: nowrap; flex: none; }
      button.on { background: var(--surface, var(--panel)) !important; color: var(--text); box-shadow: 0 1px 2px rgba(0, 0, 0, .18), 0 0 0 1px var(--line, var(--border)); } }
    .res { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
    .ok, .bad { display: inline-flex; align-items: center; height: 26px; padding: 0 10px; border-radius: var(--r, 6px); font-weight: 600; }
    .ok { background: rgba(63, 191, 111, .14); color: var(--ok); } .bad { background: rgba(229, 72, 77, .14); color: var(--danger); }
    .pt { margin-left: auto; border: 0; background: transparent !important; color: var(--accent); font-weight: 500; height: auto; padding: 0; }
    .re { width: 100%; resize: vertical; font: calc(12px * var(--ui-scale, 1))/1.5 var(--mono); word-break: break-all; }
    .hint { margin-top: 6px; }
    .foot { display: flex; gap: 8px; padding: 12px 18px; border-top: 1px solid var(--line, var(--border)); .spacer { flex: 1; } }
  `],
})
export class FactTeach {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  /** Opened from Session Options (over it) instead of on its own. */
  readonly embedded = input(false);
  readonly done = output<boolean>();
  close() { if (this.embedded()) this.done.emit(false); else this.ui.close(); }
  readonly names = FACT_NAMES;
  readonly input = this.ui.factTeach;
  readonly selected = computed(() => { const i = this.input(); return i ? i.line.substr(i.start, i.length).trim() : ''; });
  readonly regex = signal('');
  readonly found = computed(() => testFactRegex(this.regex(), this.input()?.output ?? ''));
  readonly namePick = signal('Version');
  readonly showPattern = signal(false);
  /** The selected line with two lines above and below it (from the command's output). */
  readonly context = computed(() => {
    const i = this.input();
    if (!i) return [];
    const lines = i.output.split('\n');
    let at = lines.findIndex(l => l === i.line);
    if (at < 0) at = lines.findIndex(l => l.trim() === i.line.trim());
    const pick = at < 0 ? [] : lines.slice(Math.max(0, at - 2), at + 3).map((t, k) => ({ text: t.replace(/\s+$/, ''), cur: Math.max(0, at - 2) + k === at }));
    if (!pick.length) pick.push({ text: i.line, cur: true });
    return pick.map(p => p.cur ? { ...p, pre: i.line.slice(0, i.start), val: i.line.substr(i.start, i.length), post: i.line.slice(i.start + i.length).replace(/\s+$/, '') } : { ...p, pre: '', val: '', post: '' });
  });
  customName = '';
  pickedType = '';
  command = '';
  every = true;

  constructor() {
    const i = this.input();
    if (!i) return;
    this.command = i.command;
    const v = this.selected();
    // a first guess of what the value is
    const guess = /^[0-9a-f]{2}([:.-]?[0-9a-f]{2}){5}$/i.test(v) ? 'MAC'
      : /\b(day|hour|min|week|year)s?\b|\bup\b|\d+:\d\d:\d\d/i.test(v) ? 'Uptime'
      : /^\d+(\.\d+){1,4}[a-z0-9.-]*$/i.test(v) ? 'Version' : 'Model';
    this.pickName(guess);
  }

  /** The pattern was changed by hand (then a new name keeps it). */
  edited = false;

  pickName(n: string) {
    this.namePick.set(n);
    if (n) this.every = FACT_EVERY.includes(n);
    const i = this.input();
    if (i && !this.edited) this.regex.set(factRegex(i.line, i.start, i.length, n));
  }

  dtName() { return this.input()?.deviceType || this.pickedType; }
  name() { return (this.namePick() || this.customName).trim(); }
  canSave() { return !!this.name() && !!this.command.trim() && !!this.regex() && this.found() !== null && !!this.dtName(); }

  async save() {
    const i = this.input();
    if (!i || !this.canSave()) return;
    const types = structuredClone(this.ui.settings()?.deviceTypes ?? []);
    const d = types.find(t => t.name.toLowerCase() === this.dtName().toLowerCase());
    if (!d) { this.ui.toast('Device type not found', 'error'); return; }
    const rule: FactRule = { name: this.name(), command: this.command.trim(), regex: this.regex(), every: this.every };
    d.facts = [...(d.facts ?? []).filter(f => f.name.toLowerCase() !== rule.name.toLowerCase()), rule];
    const cur = this.ui.settings();
    if (cur) await this.ui.saveSettings({ ...cur, deviceTypes: types });
    const f = await this.backend.setSessionFacts(i.path, { [rule.name]: this.found() ?? '', _at: new Date().toISOString() }).catch(() => null);
    if (f) this.ui.setFactsLocal(i.path, f);
    this.ui.toast(`${rule.name} is now read for ${d.name} sessions`, 'success');
    if (this.embedded()) this.done.emit(true); else this.ui.close();
  }
}
