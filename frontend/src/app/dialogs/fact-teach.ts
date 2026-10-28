import { Component, computed, inject, signal } from '@angular/core';
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
  <div class="modal-backdrop">
    <form class="modal ft" (ngSubmit)="save()">
      <div class="modal-head">Save as Device Fact</div>
      <div class="modal-body">
        @if (input(); as i) {
          <p class="hint">Termwright reads this value for every <b>{{ dtName() || 'device type' }}</b> session after it connects
            (in a background login - nothing is typed in your tab) and shows it on the tab, the status bar and the session.</p>
          <div class="grid">
            <label>Value</label><div class="val mono">{{ selected() }}</div>
            <label>Name</label>
            <div class="row">
              <select name="nm" [ngModel]="namePick()" (ngModelChange)="pickName($event)">
                @for (n of names; track n) { <option [value]="n">{{ n }}</option> }
                <option value="">Other…</option>
              </select>
              @if (!namePick()) { <input name="nmo" [(ngModel)]="customName" placeholder="e.g. Boot ROM, License, Location" /> }
            </div>
            <label>Device type</label>
            @if (i.deviceType) { <div><b>{{ i.deviceType }}</b></div> } @else {
              <select name="dt" [(ngModel)]="pickedType"><option value="">(choose)</option>
                @for (d of ui.settings()?.deviceTypes ?? []; track d.name) { <option [value]="d.name">{{ d.name }}</option> }</select>
            }
            <label>Command</label><input name="cmd" class="mono" [(ngModel)]="command" placeholder="e.g. show sys-info" />
            <label>Pattern</label>
            <div>
              <input name="re" class="mono re" [ngModel]="regex()" (ngModelChange)="regex.set($event); edited = true" spellcheck="false" />
              <div class="test" [class.ok]="found() === selected()" [class.bad]="found() !== selected()">
                @if (found() === selected()) { ✓ Finds "{{ found() }}" in the output }
                @else if (found() === null) { ✗ Does not match the output - edit the pattern }
                @else { ✗ Finds "{{ found() }}" instead of "{{ selected() }}" }
              </div>
              <div class="hint">Made from the line <span class="mono">{{ i.line.trim() }}</span>. The first ( ) group is the value.</div>
            </div>
            <label>When</label>
            <div class="checks">
              <label><input type="radio" name="ev" [value]="true" [(ngModel)]="every" /> At every connect (changes: version, uptime)</label>
              <label><input type="radio" name="ev" [value]="false" [(ngModel)]="every" /> Once per session (stays: model, serial, MAC)</label>
            </div>
          </div>
        }
      </div>
      <div class="modal-foot">
        <span class="spacer"></span>
        <button type="button" (click)="ui.close()">Cancel</button>
        <button type="submit" class="primary" [disabled]="!canSave()">Save Fact</button>
      </div>
    </form>
  </div>`,
  styles: [`
    .ft { width: 640px; }
    .val { padding: 4px 8px; border-radius: 6px; background: var(--accent-soft); display: inline-block; }
    .test { margin-top: 5px; font-size: calc(12px * var(--ui-scale, 1)); &.ok { color: var(--ok); } &.bad { color: var(--danger); } }
    .mono { font-family: var(--mono); }
    .re { width: 100%; }
  `],
})
export class FactTeach {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  readonly names = FACT_NAMES;
  readonly input = this.ui.factTeach;
  readonly selected = computed(() => { const i = this.input(); return i ? i.line.substr(i.start, i.length).trim() : ''; });
  readonly regex = signal('');
  readonly found = computed(() => testFactRegex(this.regex(), this.input()?.output ?? ''));
  readonly namePick = signal('Version');
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
    this.ui.close();
  }
}
