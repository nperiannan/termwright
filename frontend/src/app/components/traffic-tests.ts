import { Component, OnDestroy, OnInit, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { EmuTest, EmuTestRun, EmuTester, LabDevice } from '../core/models';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';

const KINDS: { k: EmuTest['kind']; name: string; what: string }[] = [
  { k: 'throughput', name: 'Throughput', what: 'RFC 2544: the highest rate without loss, per frame size' },
  { k: 'latency', name: 'Latency', what: 'RFC 2544: latency and jitter at the throughput rate (or a rate you give)' },
  { k: 'frame-loss', name: 'Frame loss', what: 'RFC 2544: loss at 100 %, 90 % … of the top rate, until two steps lose nothing' },
  { k: 'back-to-back', name: 'Back-to-back', what: 'RFC 2544: the longest burst at the top rate without loss' },
  { k: 'learning', name: 'MAC learning', what: 'RFC 2889: how many source MACs the switches learn before they flood (a third test port watches)' },
  { k: 'convergence', name: 'Convergence', what: 'Traffic while a cable is pulled or a switch stops or restarts: frames lost and the outage in ms' },
];

/** The Traffic tab's tests: saved RFC 2544, learning and convergence tests, their runs and results. */
@Component({
  selector: 'app-traffic-tests',
  imports: [FormsModule],
  template: `
  <div class="tw" [class.ed]="!!ed()">
    <div class="tl">
      <table>
        <tr><th>{{ tr('Test') }}</th><th>{{ tr('Kind') }}</th><th>{{ tr('Ports') }}</th><th>{{ tr('Last run') }}</th><th></th></tr>
        @for (t of tests(); track t.id) { @let r = lastOf(t.id);
          <tr [class.on]="ed()?.id === t.id || shown()?.test === t.id" (click)="show(t)"><td><b>{{ t.name }}</b></td><td>{{ tr(kindName(t.kind)) }}</td>
            <td class="mono">{{ pn(t.from) }} {{ t.both ? '⇄' : '→' }} {{ pn(t.to) }}@if (t.monitor) { · {{ pn(t.monitor) }} }</td>
            <td>@if (r) { <span class="pill" [class]="'pill ' + r.status">{{ tr(r.status) }}</span> <span class="muted small">{{ time(r.started) }}</span> }</td>
            <td class="ra"><a (click)="$event.stopPropagation(); run(t)" [class.off]="!!running()" [title]="tr('Run')">▶</a><a (click)="$event.stopPropagation(); edit(t)" [title]="tr('Edit')">✎</a><a (click)="$event.stopPropagation(); remove(t)" [title]="tr('Delete')">✕</a></td></tr>
        } @empty { <tr><td colspan="5" class="muted">{{ testers().length < 2 ? tr('Add two test ports first (＋ Test Port).') : tr('No test yet: ＋ Test.') }}</td></tr> }
      </table>
      @if (shown(); as r) {
        <div class="run">
          <div class="rh"><b>{{ r.name }}</b><span class="pill" [class]="'pill ' + r.status">{{ tr(r.status) }}</span><span class="muted small">{{ r.step || (r.ended ? tr('ended {0}', time(r.ended)) : '') }}</span>@if (r.error) { <span class="r small">{{ r.error }}</span> }<span class="sp"></span>
            @if (r.status === 'running') { <button type="button" class="sm danger" (click)="stop()">◼ {{ tr('Stop') }}</button> } @else { <button type="button" class="sm" (click)="save(r)">{{ tr('Save Report…') }}</button> }</div>
          <table class="rr">
            <tr><th>{{ tr('Direction') }}</th><th>{{ tr('Frame') }}</th>@for (c of cols(r); track c.k) { <th>{{ tr(c.name) }}</th> }<th></th></tr>
            @for (x of r.rows; track $index) {
              <tr><td class="mono">{{ x.dir }}</td><td class="mono">{{ x.size ? x.size + ' B' : '' }}</td>
                @for (c of cols(r); track c.k) { <td class="mono" [class.bad]="c.k === 'loss' && x.loss > 0">{{ val(x, c.k) }}</td> }
                <td class="muted small">{{ x.note }}</td></tr>
            } @empty { <tr><td [attr.colspan]="cols(r).length + 3" class="muted">{{ r.status === 'running' ? tr('Results show here as each step ends.') : tr('No results.') }}</td></tr> }
          </table>
          @if (r.log?.length) { <details class="lg"><summary class="muted small">{{ tr('Trials ({0})', r.log!.length) }}</summary><pre>{{ r.log!.slice(-200).join('\\n') }}</pre></details> }
        </div>
      }
    </div>
    @if (ed(); as e) {
      <div class="te"><div class="teh"><b>{{ e.id ? e.name : tr('New test') }}</b><span class="sp"></span><a (click)="ed.set(null)">✕</a></div>
        <label><span>{{ tr('Kind') }}</span><select [(ngModel)]="e.kind">@for (k of kinds; track k.k) { <option [value]="k.k">{{ tr(k.name) }}</option> }</select></label>
        <p class="muted small">{{ tr(kindWhat(e.kind)) }}</p>
        <label><span>{{ tr('Name') }}</span><input [(ngModel)]="e.name" [placeholder]="e.kind + '-1'" /></label>
        <label><span>{{ tr('From') }}</span><select [(ngModel)]="e.from">@for (t of testers(); track t.id) { <option [value]="t.id">{{ t.name }}</option> }</select></label>
        <label><span>{{ tr('To') }}</span><select [(ngModel)]="e.to">@for (t of testers(); track t.id) { @if (t.id !== e.from) { <option [value]="t.id">{{ t.name }}</option> } }</select></label>
        @if (e.kind === 'learning') { <label><span>{{ tr('Monitor') }}</span><select [(ngModel)]="e.monitor" [title]="tr('A third test port: frames flooded to it mean an address was not learned')"><option value="">-</option>@for (t of testers(); track t.id) { @if (t.id !== e.from && t.id !== e.to) { <option [value]="t.id">{{ t.name }}</option> } }</select></label> }
        @if (e.kind !== 'back-to-back' && e.kind !== 'learning') { <label><span>{{ tr('Both ways') }}</span><input type="checkbox" class="cbx" [(ngModel)]="e.both" /></label> }
        @if (e.kind !== 'convergence' && e.kind !== 'learning') { <label><span>{{ tr('Frame sizes') }}</span><input [ngModel]="sizesText()" (ngModelChange)="setSizes($event)" placeholder="64, 128, 256, 512, 1024, 1280, 1518" /></label> }
        @else { <label><span>{{ tr('Frame size') }}</span><input type="number" min="60" max="9000" [ngModel]="e.sizes?.[0] ?? 128" (ngModelChange)="e.sizes = [+$event]" /></label> }
        @if (e.kind === 'convergence' || e.kind === 'latency' || e.kind === 'frame-loss') { <label><span>{{ tr('Rate (pps)') }}</span><input type="number" min="0" [(ngModel)]="e.rate" [placeholder]="e.kind === 'latency' ? tr('the throughput found') : e.kind === 'frame-loss' ? tr('the top rate') : '1000'" /></label> }
        @if (e.kind !== 'convergence') {
          <label><span>{{ e.kind === 'back-to-back' ? tr('Max burst') : e.kind === 'learning' ? tr('Max addresses') : tr('Top rate (pps)') }}</span><input type="number" min="1" [ngModel]="e.kind === 'back-to-back' || e.kind === 'learning' ? e.max : e.maxPps" (ngModelChange)="e.kind === 'back-to-back' || e.kind === 'learning' ? (e.max = +$event) : (e.maxPps = +$event)" [placeholder]="e.kind === 'back-to-back' ? '10000' : e.kind === 'learning' ? '4096' : '10000'" /></label>
          @if (e.kind === 'back-to-back') { <label><span>{{ tr('Top rate (pps)') }}</span><input type="number" min="1" [(ngModel)]="e.maxPps" placeholder="10000" /></label> }
          <label><span>{{ tr('Trial') }}</span><div class="two"><input type="number" min="2" [(ngModel)]="e.trial" [placeholder]="tr('5 s')" [title]="tr('Seconds per trial')" /><input type="number" min="0" step="0.01" [(ngModel)]="e.loss" [placeholder]="tr('loss 0 %')" [title]="tr('Loss % that still counts as none')" /></div></label>
        } @else {
          <label><span>{{ tr('During it') }}</span><select [(ngModel)]="e.action"><option value="pull">{{ tr('pull a cable') }}</option><option value="stop">{{ tr('stop a switch') }}</option><option value="restart">{{ tr('restart a switch') }}</option></select></label>
          <label><span>{{ tr('Switch') }}</span><select [(ngModel)]="e.switch">@for (d of devs(); track d.id) { <option [value]="d.id">{{ d.hostname }}</option> }</select></label>
          @if (e.action === 'pull') { <label><span>{{ tr('Port') }}</span><input [(ngModel)]="e.port" placeholder="1" [title]="tr('The port whose cable is pulled (a LAG member for a LAG failover)')" /></label> }
          <label><span>{{ tr('Timing') }}</span><div class="two"><input type="number" min="1" [(ngModel)]="e.at" [placeholder]="tr('at 5 s')" [title]="tr('Seconds after the traffic starts')" /><input type="number" min="0" [(ngModel)]="e.hold" [placeholder]="tr('back after')" [title]="tr('Seconds until the cable is plugged in or the switch started again (0: not)')" /><input type="number" min="5" [(ngModel)]="e.seconds" [placeholder]="tr('30 s')" [title]="tr('How long the traffic runs')" /></div></label>
        }
        <div class="teb"><button type="button" class="primary" (click)="saveTest(e)">{{ tr('Save') }}</button><button type="button" (click)="saveTest(e, true)" [disabled]="!!running()">▶ {{ tr('Save and Run') }}</button></div>
      </div>
    }
  </div>`,
  styles: [`
    :host { display: block; height: 100%; min-height: 0; }
    .tw { display: grid; grid-template-columns: minmax(0, 1fr); height: 100%; min-height: 0; &.ed { grid-template-columns: minmax(0, 1fr) 340px; } }
    .tl { overflow: auto; min-height: 0; }
    table { width: 100%; border-collapse: collapse; font-size: 12px; } th { position: sticky; top: 0; background: var(--surface-2); text-align: left; font-weight: 500; padding: 4px 8px; color: var(--muted); }
    td { padding: 3px 8px; border-bottom: 1px solid var(--line); white-space: nowrap; } tr.on td { background: var(--accent-soft); } tr { cursor: default; }
    .ra a { cursor: pointer; margin-left: 8px; color: var(--muted); } .ra a:hover { color: var(--text); } .ra a.off { pointer-events: none; opacity: .4; }
    .mono { font-family: var(--mono, monospace); } .muted { color: var(--muted); } .small { font-size: 11.5px; } .r { color: var(--danger); } .bad { color: var(--danger); } .sp { flex: 1; }
    .pill { font-size: 11px; padding: 1px 7px; border-radius: 9px; border: 1px solid var(--line); } .pill.running { color: var(--accent); border-color: var(--accent); } .pill.done { color: var(--success, #1fa45a); border-color: var(--success, #1fa45a); } .pill.failed { color: var(--danger); border-color: var(--danger); }
    .run { border-top: 1px solid var(--line); margin-top: 4px; } .rh { display: flex; gap: 8px; align-items: center; padding: 6px 8px; }
    button.danger { background: var(--danger); border-color: var(--danger); color: #fff; }
    .lg { padding: 4px 8px; pre { max-height: 160px; overflow: auto; font-size: 11px; margin: 4px 0; } }
    .te { border-left: 1px solid var(--line); padding: 8px 12px; overflow: auto; display: grid; gap: 7px; align-content: start; background: var(--surface-2); font-size: 12px;
      .teh { display: flex; align-items: center; a { cursor: pointer; color: var(--muted); } } p { margin: 0; }
      label { display: grid; grid-template-columns: 96px minmax(0, 1fr); align-items: center; gap: 8px; > span { color: var(--muted); } } input, select { height: 26px; min-width: 0; }
      .two { display: grid; grid-auto-flow: column; grid-auto-columns: minmax(0, 1fr); gap: 6px; } .cbx { height: auto; justify-self: start; } .teb { display: flex; gap: 8px; } }
  `],
})
export class TrafficTests implements OnInit, OnDestroy {
  readonly testers = input<EmuTester[]>([]);
  readonly devs = input<LabDevice[]>([]);
  private backend = inject(Backend);
  private ui = inject(Ui);
  protected tr = tr;
  protected kinds = KINDS;
  protected tests = signal<EmuTest[]>([]);
  protected runs = signal<EmuTestRun[]>([]);
  protected ed = signal<EmuTest | null>(null);
  protected sel = signal('');
  private timer: ReturnType<typeof setInterval> | null = null;
  protected running = computed(() => this.runs().find(r => r.status === 'running') ?? null);
  /** The run shown under the list: the running one, else the last of the selected test, else the last. */
  protected shown = computed(() => this.running() ?? (this.sel() ? this.runs().find(r => r.test === this.sel()) : this.runs()[0]) ?? null);
  async ngOnInit() { await this.load(); this.timer = setInterval(() => { if (this.running()) void this.loadRuns(); }, 2000); }
  ngOnDestroy() { if (this.timer) clearInterval(this.timer); }
  async load() { try { this.tests.set((await this.backend.emuTests()) ?? []); } catch { /* none */ } await this.loadRuns(); }
  async loadRuns() { try { this.runs.set((await this.backend.emuTestRuns()) ?? []); } catch { /* none */ } }
  lastOf(id: string) { return this.runs().find(r => r.test === id) ?? null; }
  kindName(k: string) { return KINDS.find(x => x.k === k)?.name ?? k; }
  kindWhat(k: string) { return KINDS.find(x => x.k === k)?.what ?? ''; }
  pn(id?: string) { return this.testers().find(t => t.id === id)?.name ?? id ?? ''; }
  time(s?: string) { return s ? new Date(s).toLocaleTimeString() : ''; }
  show(t: EmuTest) { this.sel.set(t.id); }
  newTest() {
    const ts = this.testers(); if (ts.length < 2) { this.ui.toast(tr('Add two test ports first (＋ Test Port).')); return; }
    this.ed.set({ id: '', name: '', kind: 'throughput', from: ts[0].id, to: ts[1].id, both: true, monitor: ts[2]?.id ?? '', sizes: [64, 128, 256, 512, 1024, 1280, 1518], action: 'pull', switch: this.devs()[0]?.id ?? '' });
  }
  edit(t: EmuTest) { this.ed.set({ ...t, sizes: [...(t.sizes ?? [])] }); }
  sizesText() { return (this.ed()?.sizes ?? []).join(', '); }
  setSizes(v: string) { const e = this.ed(); if (e) e.sizes = v.split(/[\s,]+/).map(x => +x).filter(x => x > 0); }
  async saveTest(e: EmuTest, run = false) {
    const num = (v: unknown) => +(v as number) || 0;
    const t: EmuTest = { ...e, name: (e.name || '').trim(), trial: num(e.trial), maxPps: num(e.maxPps), rate: num(e.rate), loss: num(e.loss), max: num(e.max), at: num(e.at), hold: num(e.hold), seconds: num(e.seconds), vlan: num(e.vlan),
      monitor: e.kind === 'learning' ? e.monitor : '', action: e.kind === 'convergence' ? e.action : '', switch: e.kind === 'convergence' ? e.switch : '', port: e.kind === 'convergence' && e.action === 'pull' ? e.port : '' };
    try {
      const list = (await this.backend.emuTestSave(t)) ?? [];
      this.tests.set(list); this.ed.set(null);
      const saved = list.find(x => x.id === e.id) ?? list[list.length - 1];
      if (run && saved) await this.run(saved);
    } catch (err) { this.ui.error(err); }
  }
  async remove(t: EmuTest) {
    if (!(await this.ui.confirm(tr('Delete {0}?', t.name), tr('Its past runs stay until Termwright restarts.'), tr('Delete')))) return;
    try { this.tests.set((await this.backend.emuTestRemove(t.id)) ?? []); } catch (e) { this.ui.error(e); }
  }
  async run(t: EmuTest) { try { await this.backend.emuTestRun(t.id); this.sel.set(t.id); await this.loadRuns(); } catch (e) { this.ui.error(e); } }
  async stop() { try { await this.backend.emuTestStop(); setTimeout(() => void this.loadRuns(), 1500); } catch (e) { this.ui.error(e); } }
  async save(r: EmuTestRun) {
    try { const p = await this.backend.chooseSaveFile(tr('Save the Test Report'), r.name + '.html', [{ displayName: 'HTML', pattern: '*.html' }, { displayName: 'CSV', pattern: '*.csv' }]); if (!p) return; await this.backend.emuTestReportSave(r.id, p); this.ui.toast(tr('Saved'), 'success'); } catch (e) { this.ui.error(e); }
  }
  /** The result columns a test kind has. */
  cols(r: EmuTestRun): { k: string; name: string }[] {
    switch (r.kind) {
      case 'throughput': return [{ k: 'pps', name: 'Rate (pps)' }, { k: 'mbps', name: 'Mbit/s' }, { k: 'trials', name: 'Trials' }];
      case 'latency': return [{ k: 'pps', name: 'Rate (pps)' }, { k: 'latMin', name: 'Min ms' }, { k: 'latAvg', name: 'Avg ms' }, { k: 'latMax', name: 'Max ms' }, { k: 'jitter', name: 'Jitter ms' }, { k: 'loss', name: 'Loss %' }];
      case 'frame-loss': return [{ k: 'load', name: 'Load %' }, { k: 'pps', name: 'Rate (pps)' }, { k: 'loss', name: 'Loss %' }];
      case 'back-to-back': return [{ k: 'burst', name: 'Burst (frames)' }, { k: 'trials', name: 'Trials' }];
      case 'learning': return [{ k: 'addresses', name: 'Addresses' }, { k: 'trials', name: 'Trials' }];
      default: return [{ k: 'pps', name: 'Rate (pps)' }, { k: 'lost', name: 'Lost' }, { k: 'outageMs', name: 'Outage ms' }, { k: 'loss', name: 'Loss %' }];
    }
  }
  val(x: Record<string, unknown> | object, k: string) { const v = (x as Record<string, unknown>)[k]; return typeof v === 'number' ? (Number.isInteger(v) ? String(v) : v.toFixed(2)) : v ?? ''; }
}
