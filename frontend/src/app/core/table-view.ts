import { Component, ElementRef, HostListener, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { tr } from './i18n';

/** How a column's values compare when sorted. */
export type ColKind = 'text' | 'num' | 'ver';

export interface TvCol<T> {
  key: string;
  /** The column's title (translated by the bar); the key when none. */
  label?: string;
  /** The cell's value: what is searched, filtered and sorted. */
  get: (r: T) => string | number | null | undefined;
  kind?: ColKind;
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

/**
 * Search, column filters and sorting of a list of rows, as signals: a table renders view.rows().
 * Sort and filters stay while the component lives; the search is cleared by the user.
 */
export class TableView<T> {
  readonly q = signal('');
  readonly sort = signal<{ key: string; dir: 1 | -1 }[]>([]);
  /** Ticked values per column (a column with none ticked is not filtered). */
  readonly vals = signal<Record<string, string[]>>({});
  /** Free text per column. */
  readonly txt = signal<Record<string, string>>({});

  constructor(private src: () => T[], readonly cols: TvCol<T>[], private fixed?: (r: T) => boolean) {}

  private col(k: string) { return this.cols.find(c => c.key === k); }
  private str(c: TvCol<T>, r: T) { const v = c.get(r); return v === null || v === undefined ? '' : String(v); }

  /** What passes the search and the column filters, in the chosen order. */
  readonly rows = computed(() => {
    const words = this.q().toLowerCase().split(/\s+/).filter(Boolean), vals = this.vals(), txt = this.txt();
    let out = this.src().filter(r => {
      if (this.fixed && this.fixed(r)) return true;
      for (const c of this.cols) {
        const v = vals[c.key];
        if (v?.length && !v.includes(this.str(c, r))) return false;
        const t = txt[c.key]?.trim().toLowerCase();
        if (t && !this.str(c, r).toLowerCase().includes(t)) return false;
      }
      if (!words.length) return true;
      const hay = this.cols.map(c => this.str(c, r)).join('\u0001').toLowerCase();
      return words.every(w => hay.includes(w));
    });
    const keys = this.sort();
    if (keys.length) {
      out = out.map((r, i) => ({ r, i })).sort((a, b) => {
        for (const k of keys) {
          const c = this.col(k.key); if (!c) continue;
          const x = c.get(a.r), y = c.get(b.r), nx = x === null || x === undefined || x === '', ny = y === null || y === undefined || y === '';
          if (nx || ny) { if (nx && ny) continue; return nx ? 1 : -1; }
          const d = c.kind === 'num' ? Number(x) - Number(y) : collator.compare(String(x), String(y));
          if (d) return d * k.dir;
        }
        return a.i - b.i;
      }).map(o => o.r);
    }
    return out;
  });
  readonly total = computed(() => this.src().length);
  readonly shown = computed(() => this.rows().length);
  readonly active = computed(() => {
    const out: { key: string; text: string }[] = [], vals = this.vals(), txt = this.txt();
    for (const c of this.cols) {
      if (vals[c.key]?.length) out.push({ key: c.key, text: vals[c.key].join(', ') });
      if (txt[c.key]?.trim()) out.push({ key: c.key, text: '“' + txt[c.key].trim() + '”' });
    }
    return out;
  });

  /** The distinct values of a column, for its filter list. */
  values(key: string): string[] {
    const c = this.col(key); if (!c) return [];
    return [...new Set(this.src().map(r => this.str(c, r)).filter(Boolean))].sort((a, b) => collator.compare(a, b));
  }
  dirOf(key: string): 0 | 1 | -1 { return this.sort().find(k => k.key === key)?.dir ?? 0; }
  /** Click: ascending, descending, off. With multi (Shift) the column is added as a further key. */
  toggleSort(key: string, multi = false) {
    const cur = this.dirOf(key), next = cur === 0 ? 1 : cur === 1 ? -1 : 0;
    const rest = multi ? this.sort().filter(k => k.key !== key) : [];
    this.sort.set(next === 0 ? rest : [...rest, { key, dir: next as 1 | -1 }]);
  }
  has(key: string, v: string) { return (this.vals()[key] ?? []).includes(v); }
  toggleVal(key: string, v: string) {
    const cur = this.vals()[key] ?? [];
    this.vals.update(m => ({ ...m, [key]: cur.includes(v) ? cur.filter(x => x !== v) : [...cur, v] }));
  }
  setTxt(key: string, t: string) { this.txt.update(m => ({ ...m, [key]: t })); }
  filtered(key: string) { return !!(this.vals()[key]?.length || this.txt()[key]?.trim()); }
  clearCol(key: string) { this.vals.update(m => ({ ...m, [key]: [] })); this.txt.update(m => ({ ...m, [key]: '' })); }
  clearFilters() { this.vals.set({}); this.txt.set({}); }
  clearAll() { this.q.set(''); this.clearFilters(); }
}

/** The bar above a table: search, the active column filters as chips, and how many rows show. */
@Component({
  selector: 'app-tv-bar',
  imports: [FormsModule],
  template: `
    <div class="tvb">
      <label class="srch"><span aria-hidden="true">⌕</span><input type="search" [ngModel]="tv().q()" (ngModelChange)="tv().q.set($event)" [name]="'tvq'" [placeholder]="tr('Search')" [attr.aria-label]="tr('Search')" spellcheck="false" /></label>
      @for (c of tv().active(); track $index) { <span class="chip">{{ label(c.key) }}: {{ c.text }}<a (click)="tv().clearCol(c.key)" [title]="tr('Remove this filter')">✕</a></span> }
      @if (tv().active().length > 1) { <a class="clr" (click)="tv().clearFilters()">{{ tr('Clear filters') }}</a> }
      <span class="sp"></span>
      @if (tv().shown() !== tv().total()) { <span class="cnt">{{ tr('{0} of {1}', tv().shown(), tv().total()) }}</span> }
      <ng-content />
    </div>`,
  styles: [`
    .tvb { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding: 4px 0 6px; font-size: 12px; }
    .srch { display: inline-flex; align-items: center; gap: 6px; border: 1px solid var(--line); border-radius: 7px; padding: 2px 9px; min-width: 200px; background: var(--surface); color: var(--muted); }
    .srch:focus-within { border-color: var(--accent); }
    .srch input { all: unset; flex: 1; color: var(--text); min-width: 0; }
    .chip { display: inline-flex; align-items: center; gap: 6px; background: var(--accent-soft); color: var(--accent); border-radius: 12px; padding: 1px 9px; }
    .chip a { cursor: pointer; opacity: .8; } .clr { cursor: pointer; color: var(--muted); text-decoration: underline; }
    .sp { flex: 1; } .cnt { color: var(--muted); }`],
})
export class TvBar {
  readonly tv = input.required<TableView<any>>();
  protected tr = tr;
  protected label(key: string) { const c = this.tv().cols.find(c => c.key === key); return tr(c?.label ?? key); }
}

/**
 * A column title that sorts on click (Shift-click adds a second key) and, with a filter kind, opens its
 * value list ("values") or a text box ("text"). Use it on a th or any header element: <th tvh [tv]="v" key="state">.
 */
@Component({
  selector: '[tvh]',
  imports: [FormsModule],
  template: `<span class="tt" (click)="tv().toggleSort(key(), $event.shiftKey)" [title]="tr('Sort')" role="button" tabindex="0" (keydown.enter)="tv().toggleSort(key())"><ng-content /></span>
    @if (tv().dirOf(key()); as d) { <span class="so">{{ d === 1 ? '▲' : '▼' }}</span> }
    @if (filter() !== 'none') { <button type="button" class="fl" [class.on]="tv().filtered(key())" (click)="toggle($event)" [attr.aria-label]="tr('Filter')" [title]="tr('Filter')">⏷</button> }
    @if (open()) {
      <div class="pop" [style.top.px]="at().y" [style.left.px]="at().x" (click)="$event.stopPropagation()">
        @if (filter() === 'values') {
          <div class="ph">{{ tr('Show only') }}</div>
          <div class="pl">@for (v of vlist(); track v) { <label><input type="checkbox" [checked]="tv().has(key(), v)" (change)="tv().toggleVal(key(), v)" /> {{ v }}</label> } @empty { <span class="mu">{{ tr('Nothing yet') }}</span> }</div>
        } @else {
          <div class="ph">{{ tr('Contains') }}</div>
          <input class="tx" type="text" [ngModel]="tv().txt()[key()] ?? ''" (ngModelChange)="tv().setTxt(key(), $event)" [name]="'tvt' + key()" spellcheck="false" autofocus />
        }
        @if (tv().filtered(key())) { <a class="cl" (click)="tv().clearCol(key())">{{ tr('Clear') }}</a> }
      </div>
    }`,
  styles: [`
    :host { white-space: nowrap; }
    .tt { cursor: pointer; user-select: none; } .tt:hover { color: var(--text); }
    .so { color: var(--accent); margin-left: 4px; font-size: 9px; }
    .fl { all: unset; cursor: pointer; margin-left: 5px; font-size: 10px; opacity: .45; } .fl:hover, .fl.on { opacity: 1; color: var(--accent); }
    .pop { position: fixed; z-index: 90; min-width: 180px; max-width: 320px; max-height: 320px; overflow: auto; background: var(--surface); color: var(--text); border: 1px solid var(--line); border-radius: 9px; box-shadow: 0 10px 30px rgba(0,0,0,.3); padding: 8px 10px; font: 12px/1.5 var(--font, system-ui, sans-serif); text-transform: none; letter-spacing: 0; font-weight: 400; }
    .ph { color: var(--muted); font-size: 11px; margin-bottom: 4px; } .pl label { display: block; cursor: pointer; white-space: nowrap; } .mu { color: var(--muted); }
    .tx { width: 100%; box-sizing: border-box; border: 1px solid var(--line); border-radius: 6px; padding: 3px 7px; background: var(--bg, var(--surface)); color: var(--text); }
    .cl { display: inline-block; margin-top: 6px; cursor: pointer; color: var(--accent); }`],
})
export class TvHead {
  readonly tv = input.required<TableView<any>>();
  readonly key = input.required<string>();
  /** values: a list of the column's values to tick; text: a box; none: sorting only. */
  readonly filter = input<'values' | 'text' | 'none'>('values');
  protected tr = tr;
  protected open = signal(false);
  protected at = signal({ x: 0, y: 0 });
  private el = inject<ElementRef<HTMLElement>>(ElementRef);
  protected vlist = computed(() => (this.open() ? this.tv().values(this.key()) : []));
  protected toggle(ev: MouseEvent) {
    ev.stopPropagation();
    if (this.open()) { this.open.set(false); return; }
    const r = (ev.currentTarget as HTMLElement).getBoundingClientRect();
    this.at.set({ x: Math.max(8, Math.min(r.left, window.innerWidth - 330)), y: Math.min(r.bottom + 4, window.innerHeight - 340) });
    this.open.set(true);
  }
  @HostListener('document:click', ['$event']) away(ev: MouseEvent) { if (this.open() && !this.el.nativeElement.contains(ev.target as Node)) this.open.set(false); }
  @HostListener('document:keydown.escape') esc() { this.open.set(false); }
}
