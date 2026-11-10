import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { LabService } from '../core/lab.service';
import { LabGroup, LabModel } from '../core/models';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { Icon } from '../components/icon';

interface Row extends LabGroup { os: string; }
const CABLING = [
  { v: 'leaf-spine', label: 'Leaf-spine', hint: 'The first two switches (the first row) as the core pair with a link between them; every other switch to both cores.' },
  { v: 'chain', label: 'Chain', hint: 'Each switch to the next one.' },
  { v: 'ring', label: 'Ring', hint: 'A chain closed back to the first switch.' },
  { v: 'none', label: 'No cables', hint: 'Draw them yourself in the Lab window.' },
];

/** New Lab / Add Switches: where it runs, which switches, cabling, review. */
@Component({
  selector: 'app-lab-wizard',
  imports: [FormsModule, Icon],
  template: `
  <div class="modal-backdrop" (click)="ui.close()">
    <div class="modal labz" (click)="$event.stopPropagation()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="server" [size]="17" /></span>
        <span class="mh-t">{{ add ? tr('Add Switches') : tr('New Lab') }}<span class="mh-s">{{ tr('Step {0} of {1}', step() - first + 1, 4 - first) }}</span></span></div>
      <div class="wiz">
        <div class="steps">
          @for (s of stepNames; track $index) { @if ($index >= first) {
            <div [class.on]="$index === step()" [class.done]="$index < step()">{{ $index < step() ? '✓ ' : '' }}{{ $index - first + 1 }}  {{ tr(s) }}</div> } }
        </div>
        <div class="body">
          @switch (step()) {
            @case (0) {
              <b>{{ tr('Where should the switches run?') }}</b>
              <div class="choice">
                <div class="c on"><b>{{ tr('This computer - simulated') }}</b><span class="muted small">{{ tr('Starts in a second, a few MB per switch, from the device pack {0}.', lab.state()?.pack?.name ?? '') }}</span>
                  <ul><li>{{ tr('Up to 500 switches') }}</li><li>{{ tr('Not the real NOS, no protocols') }}</li><li>{{ tr('Works offline on macOS, Windows and Linux') }}</li></ul></div>
                <div class="c off"><b>{{ tr('A Linux host with KVM - real NOS VMs') }} <span class="soon">{{ tr('planned') }}</span></b><span class="muted small">{{ tr('Real network operating system images, deployed and wired by Termwright over SSH and shared with your team.') }}</span>
                  <ul><li>{{ tr('Real protocols and APIs') }}</li><li>{{ tr('About 1-4 GB of memory per switch') }}</li><li>{{ tr('Many hosts for large labs') }}</li></ul></div>
              </div>
            }
            @case (1) {
              <div class="tbl"><table>
                <thead><tr><th>OS</th><th>{{ tr('Model') }}</th><th>{{ tr('How many') }}</th><th>{{ tr('Names') }}</th><th>{{ tr('Ports') }}</th><th></th></tr></thead>
                <tbody>
                @for (r of rows(); track $index) {
                  <tr><td>@if ((modelOf(r.model)?.os?.length ?? 0) > 1) {
                        <select class="ossel" [ngModel]="r.os" (ngModelChange)="patch($index, { os: $event })" [name]="'o' + $index" [title]="tr('This model runs more than one operating system')">@for (o of modelOf(r.model)!.os; track o) { <option [value]="o">{{ lab.osShort(o) }}</option> }</select>
                      } @else { <span class="os">{{ lab.osShort(r.os) }}</span> }</td>
                    <td><select [ngModel]="r.model" (ngModelChange)="setModel($index, $event)" [name]="'m' + $index">
                      @for (f of families(); track f) { <optgroup [label]="f">@for (m of models(f); track m.sku) { <option [value]="m.sku">{{ m.sku }}</option> }</optgroup> }</select></td>
                    <td><input type="number" min="0" max="200" [ngModel]="r.count" (ngModelChange)="patch($index, { count: +$event })" [name]="'n' + $index" class="num" /></td>
                    <td><input class="mono" [ngModel]="r.names" (ngModelChange)="patch($index, { names: $event })" [name]="'p' + $index" spellcheck="false" /></td>
                    <td class="small" [title]="modelOf(r.model)?.layout ?? ''">{{ modelOf(r.model)?.ports }} <span class="muted">· {{ modelOf(r.model)?.layout }}</span></td>
                    <td><button type="button" class="sm" (click)="drop($index)" [disabled]="rows().length === 1" [title]="tr('Remove')">✕</button></td></tr>
                }
                </tbody></table></div>
              <div class="bar"><button type="button" class="sm" (click)="more()">+ {{ tr('Add a model') }}</button><span class="spacer"></span>
                @if (!add) { <span class="muted">{{ tr('Management network') }}</span><input class="mono net" [(ngModel)]="mgmtNet" name="net" spellcheck="false" /> }
                <span class="muted">{{ tr('Password') }}</span><input type="password" [(ngModel)]="password" name="pw" [placeholder]="tr('any')" class="pw" autocomplete="new-password" /></div>
              <p class="muted small">{{ tr('{n} in a name is the number. The switches log in as admin; without a password any password is taken. They listen on this computer only (127.0.0.1).') }}</p>
            }
            @case (2) {
              <div class="presets">
                @for (c of cabling; track c.v) {
                  <label class="c" [class.on]="cable === c.v"><input type="radio" name="cab" [value]="c.v" [(ngModel)]="cable" /> <b>{{ tr(c.label) }}</b><span class="muted small">{{ tr(c.hint) }}</span></label>
                }
              </div>
              <div class="prev"><svg [attr.viewBox]="'0 0 ' + preview().w + ' 200'">
                @for (l of preview().lines; track $index) { <line [attr.x1]="l[0]" [attr.y1]="l[1]" [attr.x2]="l[2]" [attr.y2]="l[3]" [class.isc]="l[4]" /> }
                @for (n of preview().nodes; track $index) { <g [class.old]="n.old"><rect [attr.x]="n.x" [attr.y]="n.y" width="120" height="30" rx="7" /><text [attr.x]="n.x + 60" [attr.y]="n.y + 19">{{ n.name }}</text></g> }
              </svg></div>
            }
            @case (3) {
              <div class="kv">
                <span>{{ tr('Runs on') }}</span><span>{{ tr('This computer - simulated') }}</span>
                <span>{{ tr('Switches') }}</span><span>{{ summary() }}</span>
                <span>{{ tr('Cabling') }}</span><span>{{ tr(cablingLabel()) }}</span>
                <span>{{ tr('Day-0 config') }}</span><span>{{ tr('Hostname, management address, login admin') }}{{ password ? '' : ' ' + tr('(any password)') }}</span>
                <span>{{ tr('Management') }}</span><span class="mono">{{ add ? (lab.state()?.mgmtNet || mgmtNet) : mgmtNet }}</span>
                <span>{{ tr('Sessions') }}</span><span>{{ tr('Folder') }}: {{ lab.state()?.folder || 'Simulated Lab' }}</span>
              </div>
              <label class="chk"><input type="checkbox" [(ngModel)]="start" name="st" /> {{ tr('Start the switches now') }}</label>
              <p class="muted small">{{ tr('Protocols need the real operating system - they come with KVM hosts.') }}</p>
            }
          }
        </div>
      </div>
      <div class="modal-foot"><span class="muted small">{{ summary() }}</span><span class="spacer"></span>
        <button type="button" (click)="ui.close()">{{ tr('Cancel') }}</button>
        @if (step() > first) { <button type="button" (click)="step.set(step() - 1)">{{ tr('Back') }}</button> }
        @if (step() < 3) { <button type="button" class="primary" (click)="step.set(step() + 1)" [disabled]="step() === 1 && !total()">{{ tr('Next') }}</button> }
        @else { <button type="button" class="primary" (click)="create()" [disabled]="!total() || lab.busy()">{{ add ? tr('Add {0} Switches', total()) : tr('Create Lab') }}</button> }
      </div>
    </div>
  </div>`,
  styles: [`
    .labz { width: min(980px, 96vw); }
    .wiz { display: grid; grid-template-columns: 190px 1fr; min-height: 430px; }
    .steps { border-right: 1px solid var(--line); padding: 14px 10px; background: var(--surface-2); }
    .steps div { padding: 8px 10px; border-radius: 8px; color: var(--muted); margin-bottom: 4px; font-size: 13px; }
    .steps div.on { background: var(--accent-soft); color: var(--text); font-weight: 600; } .steps div.done { color: var(--ok); }
    .body { padding: 14px 18px; min-width: 0; }
    .choice { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-top: 10px; }
    .choice .c { border: 1px solid var(--line); border-radius: 12px; padding: 14px; background: var(--surface); }
    .choice .c.on { border-color: var(--accent); box-shadow: 0 0 0 2px var(--accent-soft); } .choice .c.off { opacity: .6; }
    .choice b { display: block; font-size: 14px; margin-bottom: 4px; } .choice ul { margin: 8px 0 0; padding-left: 18px; font-size: 12.5px; color: var(--muted); }
    .soon { font-size: 10.5px; font-weight: 600; padding: 1px 7px; border-radius: 9px; background: var(--surface-2); color: var(--muted); margin-left: 4px; }
    .tbl { border: 1px solid var(--line); border-radius: 10px; overflow: auto; max-height: 260px; }
    table { width: 100%; border-collapse: collapse; } th { background: var(--surface-2); text-align: left; font-size: 12px; font-weight: 600; padding: 7px 10px; }
    td { padding: 5px 10px; border-top: 1px solid var(--line); }
    input.num { width: 70px; } input.net { width: 130px; } input.pw { width: 110px; }
    .bar { display: flex; gap: 8px; align-items: center; margin: 10px 0; flex-wrap: wrap; } .spacer { flex: 1; }
    button.sm { height: 26px; padding: 0 9px; font-size: 12px; }
    .mono { font-family: var(--mono); font-size: 12px; }
    .os { font-size: 10.5px; font-weight: 700; padding: 1px 6px; border-radius: 5px; background: color-mix(in srgb, #7c5cff 18%, transparent); color: #8f75ff; }
    .ossel { height: 26px; font-size: 12px; }
    
    .presets { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; }
    .presets .c { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; display: flex; flex-direction: column; gap: 4px; cursor: pointer; background: var(--surface); }
    .presets .c.on { border-color: var(--accent); box-shadow: 0 0 0 2px var(--accent-soft); } .presets input { display: none; }
    .prev { margin-top: 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--bg); overflow: auto; }
    .prev svg { display: block; width: 100%; min-width: 500px; height: 220px; }
    .prev rect { fill: var(--surface); stroke: var(--line); } .prev .old rect { stroke-dasharray: 4 3; }
    .prev text { fill: var(--text); font: 600 11px var(--ui-font); text-anchor: middle; }
    .prev line { stroke: var(--ok); stroke-width: 1.6; } .prev line.isc { stroke: var(--accent); stroke-width: 3; }
    .kv { display: grid; grid-template-columns: 140px 1fr; gap: 8px 12px; font-size: 13px; margin-bottom: 14px; } .kv > span:nth-child(odd) { color: var(--muted); }
    .chk { display: inline-flex; gap: 8px; align-items: center; }
  `],
})
export class LabWizard {
  protected ui = inject(Ui);
  protected lab = inject(LabService);
  protected tr = tr;
  protected add = this.ui.labWizard().add && !!this.lab.state()?.exists;
  protected first = this.add ? 1 : 0;
  protected step = signal(this.first);
  protected stepNames = ['Where it runs', 'Switches', 'Cabling', 'Review'];
  protected cabling = CABLING;
  protected cable = 'leaf-spine';
  protected mgmtNet = '10.99.0.0/24';
  protected password = '';
  protected start = true;
  protected rows = signal<Row[]>([]);
  constructor() { this.rows.set(this.firstRows()); }

  protected total = computed(() => this.rows().reduce((n, r) => n + (r.count > 0 ? r.count : 0), 0));
  protected summary = computed(() => {
    const parts = this.rows().filter(r => r.count > 0).map(r => `${r.count} × ${r.model}`);
    return parts.length ? tr('{0} switches', this.total()) + ' (' + parts.join(', ') + ')' : tr('No switches chosen');
  });
  protected cablingLabel = () => CABLING.find(c => c.v === this.cable)?.label ?? '';

  families(): string[] { return [...new Set((this.lab.state()?.models ?? []).map(m => m.family))]; }
  models(family: string): LabModel[] { return (this.lab.state()?.models ?? []).filter(m => m.family === family); }
  modelOf(name: string) { return (this.lab.state()?.models ?? []).find(m => m.sku === name); }
  setModel(i: number, model: string) {
    const m = this.modelOf(model);
    const os = m?.os.includes(this.rows()[i].os) ? this.rows()[i].os : (m?.os[0] ?? '');
    this.patch(i, { model, os, names: this.nameFor(model) });
  }
  private nameFor(model: string) { return 'sw-' + model.split('-').find(p => /\d/.test(p))!.toLowerCase().replace(/[^a-z0-9]+/g, '') + '-{n}'; }
  /** First rows: a model of each family (two each); adding: one row. */
  private firstRows(): Row[] {
    const ms = this.lab.state()?.models ?? [];
    const pick = this.add ? ms.slice(0, 1) : this.families().slice(0, 3).map(f => this.models(f)[0]);
    return pick.filter(Boolean).map(m => ({ os: m.os[0], model: m.sku, count: 2, names: this.nameFor(m.sku) }));
  }
  patch(i: number, p: Partial<Row>) { this.rows.set(this.rows().map((r, j) => (j === i ? { ...r, ...p } : r))); }
  more() { const m = (this.lab.state()?.models ?? [])[0]; if (m) this.rows.set([...this.rows(), { os: m.os[0], model: m.sku, count: 1, names: this.nameFor(m.sku) }]); }
  drop(i: number) { this.rows.set(this.rows().filter((_, j) => j !== i)); }

  /** A sketch of the cabling (the real port choice happens in Termwright). */
  protected preview = computed(() => {
    const names: { name: string; os: string; old?: boolean }[] = [];
    if (this.add) for (const d of this.lab.devices()) names.push({ name: d.hostname, os: d.os, old: true });
    for (const r of this.rows()) for (let i = 1; i <= Math.min(r.count, 12); i++) names.push({ name: r.names.replace('{n}', String(i)), os: r.os });
    const shown = names.slice(0, 16);
    let cores: number[] = [];
    if (this.cable === 'leaf-spine') {
      cores = shown.length > 1 ? [0, 1] : [0];
    }
    const top = shown.map((_, i) => i).filter(i => cores.includes(i)), bottom = shown.map((_, i) => i).filter(i => !cores.includes(i));
    const w = Math.max(560, Math.max(top.length, bottom.length) * 135 + 20);
    const at = new Map<number, { x: number; y: number }>();
    top.forEach((i, k) => at.set(i, { x: (w - top.length * 135) / 2 + k * 135, y: 18 }));
    bottom.forEach((i, k) => at.set(i, { x: (w - bottom.length * 135) / 2 + k * 135, y: cores.length ? 150 : 85 }));
    const lines: [number, number, number, number, boolean][] = [];
    const mid = (i: number) => ({ x: at.get(i)!.x + 60, y: at.get(i)!.y + 15 });
    const join = (a: number, b: number, isc = false) => { const p = mid(a), q = mid(b); lines.push([p.x, p.y, q.x, q.y, isc]); };
    if (this.cable === 'leaf-spine') {
      if (top.length === 2) join(top[0], top[1], true);
      for (const l of bottom) if (!shown[l].old) for (const c of top) join(c, l);
    } else if (this.cable === 'chain' || this.cable === 'ring') {
      const fresh = shown.map((_, i) => i).filter(i => !shown[i].old);
      for (let k = 0; k + 1 < fresh.length; k++) join(fresh[k], fresh[k + 1]);
      if (this.cable === 'ring' && fresh.length > 2) join(fresh[fresh.length - 1], fresh[0]);
    }
    return { w, lines, nodes: shown.map((n, i) => ({ ...n, ...at.get(i)! })) };
  });

  async create() {
    const ok = await this.lab.add({
      groups: this.rows().filter(r => r.count > 0).map(r => ({ os: r.os, model: r.model, count: r.count, names: r.names })),
      mgmtNet: this.add ? '' : this.mgmtNet, cabling: this.cable, password: this.password, start: this.start,
    });
    if (ok) this.ui.open('lab');
  }
}
