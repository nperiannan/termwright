import { Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { EmuTester, EmuTrafficTemplate, EmuTrafficView } from '../core/models';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';

const ROLES = ['A', 'B', 'C', 'D', 'E', 'F'];

/**
 * The traffic generator's library of ready tests: Termwright's own and yours. Choose one, the test port for each of
 * its roles, Apply: its streams and tests are added to the lab. Your streams and tests can be kept as a template.
 */
@Component({
  selector: 'app-traffic-library',
  imports: [FormsModule],
  template: `
    <div class="ov" (click)="closed.emit()">
      <div class="dl" role="dialog" [attr.aria-label]="tr('Test library')" (click)="$event.stopPropagation()">
        <div class="dh"><b>{{ tr('Test library') }}</b><span class="muted">{{ tr('Ready tests: choose one and the test ports it runs on') }}</span><span class="sp"></span><a class="x" (click)="closed.emit()" [title]="tr('Close')">✕</a></div>
        <div class="db">
          <nav class="ls">
            @for (g of groups(); track g.name) {
              <div class="gh">{{ tr(g.name) }}<span>{{ g.items.length }}</span></div>
              @for (t of g.items; track t.id) {
                <a [class.on]="sel()?.id === t.id" (click)="pick(t)"><b>{{ tr(t.name) }}</b><small>{{ count(t) }}</small></a>
              } @empty { <p class="muted small">{{ tr('None yet: save your streams and tests below.') }}</p> }
            }
          </nav>
          @if (sel(); as t) {
            <section class="dt">
              <h3>{{ tr(t.name) }}</h3>
              @if (t.desc) { <p class="desc">{{ tr(t.desc) }}</p> }
              <div class="roles">
                @for (r of roleList(); track r.k) {
                  <label><span class="rk">{{ r.k }}</span><span class="rn">{{ tr(r.what) }}</span>
                    <select [ngModel]="ports()[r.i] ?? ''" (ngModelChange)="setPort(r.i, $event)"><option value="">{{ tr('choose a test port') }}</option>@for (p of testers(); track p.id) { <option [value]="p.id">{{ p.name }}</option> }</select></label>
                }
              </div>
              @if ((t.ports ?? 0) > testers().length) { <p class="warn">⚠ {{ tr('It needs {0} test ports; the lab has {1}. Add test ports first.', t.ports, testers().length) }}</p> }
              <div class="what">
                @for (s of t.streams ?? []; track $index) { <div><i class="st">{{ tr('stream') }}</i><b>{{ s.name }}</b><span class="mono">{{ s.from }} → {{ s.to || tr('all') }} · {{ s.kind.toUpperCase() }} · {{ rateOf(s) }}{{ s.dscp ? ' · DSCP ' + s.dscp : '' }}{{ s.rtt ? ' · ' + tr('round trip') : '' }}</span></div> }
                @for (x of t.tests ?? []; track $index) { <div><i class="ts">{{ tr('test') }}</i><b>{{ x.name }}</b><span class="mono">{{ x.from }} → {{ x.to }}{{ x.monitor ? ' · ' + tr('monitor') + ' ' + x.monitor : '' }} · {{ x.kind }}</span></div> }
              </div>
              <div class="ac">@if (!t.builtin) { <button type="button" class="sm" (click)="remove(t)">{{ tr('Remove') }}</button> }<span class="sp"></span>
                <button type="button" class="primary" (click)="apply(t)" [disabled]="!ready() || busy()">{{ tr('Apply') }}</button></div>
            </section>
          }
        </div>
        <div class="df">
          <span class="muted">{{ tr('Keep this lab’s streams and tests as a template of yours:') }}</span>
          <input [(ngModel)]="newName" [placeholder]="tr('name')" />
          <input class="wide" [(ngModel)]="newDesc" [placeholder]="tr('what it checks (optional)')" />
          <button type="button" (click)="save()" [disabled]="!newName.trim() || busy()">{{ tr('Save as Template') }}</button>
        </div>
      </div>
    </div>`,
  styles: [`
    .ov { position: fixed; inset: 0; z-index: 100; background: rgba(0,0,0,.45); display: grid; place-items: center; }
    .dl { width: min(1000px, 94vw); height: min(640px, 88vh); background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 18px 50px rgba(0,0,0,.45); display: grid; grid-template-rows: auto minmax(0, 1fr) auto; overflow: hidden; font-size: 13px; }
    .dh { display: flex; align-items: baseline; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--line); b { font-size: 15px; } .x { cursor: pointer; color: var(--muted); } }
    .sp { flex: 1; } .muted { color: var(--muted); } .small { font-size: 11.5px; } .mono { font-family: var(--mono, monospace); font-size: 11.5px; }
    .db { display: grid; grid-template-columns: 280px minmax(0, 1fr); min-height: 0; }
    .ls { border-right: 1px solid var(--line); overflow: auto; padding: 8px; background: var(--surface-2);
      .gh { display: flex; justify-content: space-between; font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); padding: 8px 8px 4px; }
      a { display: flex; flex-direction: column; gap: 1px; padding: 7px 10px; border-radius: 8px; cursor: pointer; small { color: var(--muted); font-size: 11px; } &:hover { background: var(--surface); } &.on { background: var(--accent-soft); b { color: var(--accent); } } }
      p { padding: 4px 10px; } }
    .dt { padding: 14px 18px; overflow: auto; display: flex; flex-direction: column; gap: 12px; h3 { margin: 0; font-size: 16px; } .desc { margin: 0; color: var(--muted); line-height: 1.45; } }
    .roles { display: grid; gap: 6px; label { display: grid; grid-template-columns: 26px minmax(0, 1fr) 220px; align-items: center; gap: 8px; } .rk { width: 22px; height: 22px; border-radius: 6px; display: grid; place-items: center; font-weight: 700; font-size: 12px; background: color-mix(in srgb, var(--s1, #3987e5) 18%, transparent); color: var(--s1, #3987e5); } .rn { color: var(--muted); } }
    .what { border: 1px solid var(--line); border-radius: 10px; div { display: grid; grid-template-columns: 52px minmax(120px, auto) minmax(0, 1fr); gap: 10px; align-items: center; padding: 6px 10px; border-bottom: 1px solid var(--line); &:last-child { border-bottom: 0; } }
      i { font-style: normal; font-size: 10.5px; font-weight: 600; text-transform: uppercase; text-align: center; border-radius: 9px; padding: 1px 0; } .st { color: var(--s1, #3987e5); background: color-mix(in srgb, var(--s1, #3987e5) 14%, transparent); } .ts { color: var(--s3, #199e70); background: color-mix(in srgb, var(--s3, #199e70) 14%, transparent); } }
    .warn { margin: 0; color: var(--warning, #e39b17); font-size: 12px; }
    .ac { display: flex; gap: 8px; align-items: center; margin-top: auto; }
    .df { display: flex; align-items: center; gap: 8px; padding: 10px 16px; border-top: 1px solid var(--line); background: var(--surface-2); input { width: 180px; } input.wide { flex: 1; width: auto; } }`],
})
export class TrafficLibrary implements OnInit {
  readonly testers = input<EmuTester[]>([]);
  readonly closed = output<void>();
  readonly applied = output<EmuTrafficView>();
  private backend = inject(Backend);
  private ui = inject(Ui);
  protected tr = tr;
  protected all = signal<EmuTrafficTemplate[]>([]);
  protected sel = signal<EmuTrafficTemplate | null>(null);
  protected ports = signal<string[]>([]);
  protected busy = signal(false);
  protected newName = '';
  protected newDesc = '';

  protected groups = computed(() => [{ name: 'Termwright', items: this.all().filter(t => t.builtin || !t.saved) }, { name: 'Yours', items: this.all().filter(t => !t.builtin && !!t.saved) }]);
  protected roleList = computed(() => { const t = this.sel(); return t ? ROLES.slice(0, t.ports).map((k, i) => ({ k, i, what: t.roles?.[i] ?? '' })) : []; });
  protected ready = computed(() => { const t = this.sel(), p = this.ports(); return !!t && p.slice(0, t.ports).filter(Boolean).length === t.ports && new Set(p.slice(0, t.ports)).size === t.ports; });

  async ngOnInit() { await this.load(); }
  private async load() {
    try { const ts = (await this.backend.emuTrafficTemplates()) ?? []; for (const t of ts) if (!t.saved) t.builtin = true; this.all.set(ts); if (!this.sel() && ts.length) this.pick(ts[0]); } catch (e) { this.ui.error(e); }
  }
  protected pick(t: EmuTrafficTemplate) { this.sel.set(t); const ps = this.testers(); this.ports.set(ROLES.slice(0, t.ports).map((_, i) => ps[i]?.id ?? '')); }
  protected setPort(i: number, id: string) { const p = [...this.ports()]; p[i] = id; this.ports.set(p); }
  protected count(t: EmuTrafficTemplate) {
    const s = t.streams?.length ?? 0, x = t.tests?.length ?? 0;
    return [s ? tr(s === 1 ? '1 stream' : '{0} streams', s) : '', x ? tr(x === 1 ? '1 test' : '{0} tests', x) : '', tr('{0} test ports', t.ports)].filter(Boolean).join(' · ');
  }
  protected rateOf(s: NonNullable<EmuTrafficTemplate['streams']>[number]) { return s.rateUnit === 'pct' ? s.rate + ' % ' + tr('of line') : s.rateUnit === 'mbps' ? s.rate + ' Mbit/s' : s.pps + ' pps'; }
  protected async apply(t: EmuTrafficTemplate) {
    this.busy.set(true);
    try { const v = await this.backend.emuTrafficTemplateApply(t.id, this.ports().slice(0, t.ports)); this.ui.toast(tr('{0} added', tr(t.name)), 'success'); this.applied.emit(v); } catch (e) { this.ui.error(e); } finally { this.busy.set(false); }
  }
  protected async save() {
    this.busy.set(true);
    try { const t = await this.backend.emuTrafficTemplateSave(this.newName.trim(), this.newDesc.trim(), []); this.newName = this.newDesc = ''; await this.load(); const k = this.all().find(x => x.id === t.id); if (k) this.pick(k); this.ui.toast(tr('Saved'), 'success'); } catch (e) { this.ui.error(e); } finally { this.busy.set(false); }
  }
  protected async remove(t: EmuTrafficTemplate) {
    try { await this.backend.emuTrafficTemplateRemove(t.id); this.sel.set(null); await this.load(); } catch (e) { this.ui.error(e); }
  }
}
