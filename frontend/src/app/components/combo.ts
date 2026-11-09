import { Component, ElementRef, HostListener, computed, effect, input, output, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Icon } from './icon';
import { tr } from '../core/i18n';

export interface ComboOption { value: string; label?: string; hint?: string; }

/**
 * A text box with a drop-down list: pick one of the options, or type your own.
 * The list filters as you type; ↑/↓ choose, Enter takes, Esc closes.
 */
@Component({
  selector: 'app-combo',
  imports: [FormsModule, Icon],
  host: { class: 'combo' },
  template: `
    <input #box [name]="name()" [class]="inputClass()" [ngModel]="value()" (ngModelChange)="typed($event)" (keydown)="key($event)"
      [placeholder]="placeholder()" spellcheck="false" autocomplete="off" role="combobox" [attr.aria-expanded]="open()" aria-autocomplete="list" [attr.aria-label]="label() || placeholder()" />
    <button type="button" class="chev" tabindex="-1" [attr.aria-label]="showList" (mousedown)="$event.preventDefault()" (click)="toggle()"><app-icon name="chevron-down" [size]="13" /></button>
    @if (open()) {
      <div #list class="list" role="listbox" [style.left.px]="pos().x" [style.top.px]="pos().y" [style.width.px]="pos().w" (mousedown)="$event.preventDefault()">
        @for (o of shown(); track o.value; let i = $index) {
          <div class="opt" role="option" [class.on]="i === hi()" [attr.aria-selected]="o.value === value()" (mouseenter)="hi.set(i)" (click)="pick(o.value)">
            <span class="ck">@if (o.value === value()) { <app-icon name="check" [size]="12" /> }</span>
            <span class="lb">{{ o.label || o.value }}</span>@if (o.hint) { <small>{{ o.hint }}</small> }
          </div>
        } @empty { <div class="none">{{ loading() ? loadingText() : emptyText() }}</div> }
        @if (loading() && shown().length) { <div class="none">{{ loadingText() }}</div> }
      </div>
    }`,
  styles: [`
    :host { position: relative; display: flex; align-items: center; min-width: 0; }
    input { flex: 1; min-width: 0; padding-right: 28px !important; }
    .chev { position: absolute; right: 3px; top: 50%; transform: translateY(-50%); width: 24px; height: 24px; padding: 0; border: 0; background: none; color: var(--muted); display: inline-flex; align-items: center; justify-content: center; border-radius: 5px; }
    .chev:hover { color: var(--text); background: var(--hover); }
    .list { position: fixed; z-index: 1000; max-height: 260px; overflow: auto; background: var(--panel); border: 1px solid var(--border); border-radius: 9px; box-shadow: var(--shadow); padding: 4px; }
    .opt { display: flex; align-items: center; gap: 7px; padding: 6px 8px; border-radius: 6px; cursor: pointer; font-size: calc(12.5px * var(--ui-scale, 1)); }
    .opt.on { background: color-mix(in srgb, var(--accent) 16%, transparent); }
    .ck { width: 14px; flex: none; display: inline-flex; color: var(--accent); }
    .lb { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    small { margin-left: auto; color: var(--muted); font-family: var(--mono); font-size: calc(11px * var(--ui-scale, 1)); white-space: nowrap; }
    .none { padding: 7px 10px; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); }
  `],
})
export class Combo {
  readonly value = input<string>('');
  readonly options = input<ComboOption[]>([]);
  readonly placeholder = input('');
  readonly label = input('');
  readonly name = input('combo');
  readonly inputClass = input('');
  readonly loading = input(false);
  readonly loadingText = input('Loading…');
  readonly emptyText = input('Nothing to choose - type a name');
  readonly valueChange = output<string>();
  protected readonly showList = tr('Show the list');
  /** The list opened: load the options now if they are not there yet. */
  readonly opened = output<void>();

  private box = viewChild.required<ElementRef<HTMLInputElement>>('box');
  readonly open = signal(false);
  readonly hi = signal(-1);
  readonly filter = signal('');
  readonly pos = signal({ x: 0, y: 0, w: 200 });
  /** Typed text filters the list; a value picked from it shows the whole list again. */
  readonly shown = computed(() => {
    const f = this.filter().trim().toLowerCase();
    const l = this.options();
    return f ? l.filter(o => (o.value + ' ' + (o.label ?? '')).toLowerCase().includes(f)) : l;
  });

  private list = viewChild<ElementRef<HTMLElement>>('list');

  constructor(private host: ElementRef<HTMLElement>) {
    // on the page itself: a dialog that moves or clips (transform, overflow) would carry it off or cut it
    effect(() => { const l = this.list()?.nativeElement; if (l && l.parentElement !== document.body) document.body.appendChild(l); });
  }

  /** The list sits on the window, so it follows the box while open (scrolling, the page changing). */
  private follow = () => {
    if (!this.open()) return;
    const r = this.host.nativeElement.getBoundingClientRect();
    if (r.bottom < 0 || r.top > innerHeight || !r.width) { this.open.set(false); return; }
    const below = innerHeight - r.bottom > 200;
    const p = { x: r.left, y: below ? r.bottom + 4 : Math.max(8, r.top - 264), w: r.width }, o = this.pos();
    if (p.x !== o.x || p.y !== o.y || p.w !== o.w) this.pos.set(p);
    requestAnimationFrame(this.follow);
  };
  typed(v: string) { this.filter.set(v); this.valueChange.emit(v); if (!this.open()) this.show(); this.hi.set(-1); }
  toggle() { if (this.open()) this.open.set(false); else { this.filter.set(''); this.show(); } this.box().nativeElement.focus(); }
  private show() {
    this.open.set(true);
    this.follow();
    this.hi.set(Math.max(0, this.shown().findIndex(o => o.value === this.value())));
    this.opened.emit();
  }
  pick(v: string) { this.valueChange.emit(v); this.filter.set(''); this.open.set(false); }
  key(e: KeyboardEvent) {
    const n = this.shown().length;
    if (e.key === 'ArrowDown') { e.preventDefault(); if (!this.open()) { this.filter.set(''); this.show(); } else this.hi.set(n ? (this.hi() + 1) % n : -1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); if (this.open()) this.hi.set(n ? (this.hi() - 1 + n) % n : -1); }
    else if (e.key === 'Enter' && this.open()) { e.preventDefault(); const o = this.shown()[this.hi()]; if (o) this.pick(o.value); else this.open.set(false); }
    else if (e.key === 'Escape' && this.open()) { e.preventDefault(); e.stopPropagation(); this.open.set(false); }
    else if (e.key === 'Tab') this.open.set(false);
  }
  @HostListener('document:mousedown', ['$event']) out(e: MouseEvent) {
    const t = e.target as Node;
    if (this.open() && !this.host.nativeElement.contains(t) && !this.list()?.nativeElement.contains(t)) this.open.set(false);
  }
  @HostListener('window:resize') resized() { this.open.set(false); }
}
