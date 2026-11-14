import { Component, ElementRef, computed, input, output, signal, viewChild, AfterViewInit } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { tr } from '../core/i18n';

/** A command of the palette: what it says, where it belongs, and what it does. */
export interface PaletteCmd { id: string; label: string; group: string; hint?: string; run: () => void; }

/**
 * The command palette (⌘K / Ctrl+K): type a few letters of a device, a page or an action, Enter runs the first;
 * ↑ ↓ choose, Esc closes.
 */
@Component({
  selector: 'app-cmd-palette',
  imports: [FormsModule],
  template: `
    <div class="ov" (click)="closed.emit()">
      <div class="pl" role="dialog" [attr.aria-label]="tr('Commands')" (click)="$event.stopPropagation()">
        <input #q class="q" [ngModel]="query()" (ngModelChange)="query.set($event); at.set(0)" [placeholder]="tr('A device, a page or an action…')" (keydown)="key($event)" spellcheck="false" autocomplete="off" />
        <div class="ls">
          @for (c of shown(); track c.id; let i = $index) {
            @if (i === 0 || shown()[i - 1].group !== c.group) { <div class="gh">{{ tr(c.group) }}</div> }
            <a [class.on]="i === at()" (click)="run(c)" (mouseenter)="at.set(i)"><span>{{ c.label }}</span>@if (c.hint) { <small>{{ c.hint }}</small> }</a>
          } @empty { <p class="muted">{{ tr('Nothing matches.') }}</p> }
        </div>
        <div class="ft"><span><kbd>↑</kbd><kbd>↓</kbd> {{ tr('choose') }}</span><span><kbd>Enter</kbd> {{ tr('run') }}</span><span><kbd>Esc</kbd> {{ tr('close') }}</span></div>
      </div>
    </div>`,
  styles: [`
    .ov { position: fixed; inset: 0; z-index: 100; background: rgba(0,0,0,.35); display: flex; justify-content: center; align-items: flex-start; padding-top: 12vh; }
    .pl { width: min(620px, 92vw); max-height: 70vh; background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 18px 50px rgba(0,0,0,.45); display: grid; grid-template-rows: auto minmax(0, 1fr) auto; overflow: hidden; }
    .q { border: 0 !important; border-bottom: 1px solid var(--line) !important; border-radius: 0 !important; padding: 14px 16px !important; font-size: 15px; background: transparent; outline: none; height: auto !important; }
    .ls { overflow: auto; padding: 6px; .gh { font-size: 10.5px; font-weight: 600; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); padding: 8px 10px 3px; }
      a { display: flex; align-items: baseline; gap: 10px; padding: 7px 10px; border-radius: 7px; cursor: pointer; span { flex: 1; } small { color: var(--muted); font-size: 11px; } &.on { background: var(--accent-soft); span { color: var(--accent); } } }
      p { padding: 10px; margin: 0; } }
    .muted { color: var(--muted); }
    .ft { display: flex; gap: 14px; padding: 7px 12px; border-top: 1px solid var(--line); font-size: 11px; color: var(--muted); background: var(--surface-2); kbd { font: 10.5px var(--mono, monospace); border: 1px solid var(--line); border-radius: 4px; padding: 0 4px; margin-right: 3px; } }`],
})
export class CmdPalette implements AfterViewInit {
  readonly cmds = input<PaletteCmd[]>([]);
  readonly closed = output<void>();
  protected tr = tr;
  protected query = signal('');
  protected at = signal(0);
  private q = viewChild<ElementRef<HTMLInputElement>>('q');

  /** Matches: every word of the query in the label or hint (any order), the ones starting with it first. */
  protected shown = computed(() => {
    const ws = this.query().toLowerCase().split(/\s+/).filter(Boolean);
    const all = this.cmds();
    if (!ws.length) return all.slice(0, 60);
    const hit = all.filter(c => { const t = (c.label + ' ' + (c.hint ?? '') + ' ' + c.group).toLowerCase(); return ws.every(w => t.includes(w)); });
    const starts = (c: PaletteCmd) => (c.label.toLowerCase().startsWith(ws[0]) ? 0 : 1);
    return hit.sort((a, b) => starts(a) - starts(b)).slice(0, 60);
  });

  ngAfterViewInit() { setTimeout(() => this.q()?.nativeElement.focus()); }
  protected key(e: KeyboardEvent) {
    const n = this.shown().length;
    if (e.key === 'ArrowDown') { this.at.set(Math.min(n - 1, this.at() + 1)); e.preventDefault(); }
    else if (e.key === 'ArrowUp') { this.at.set(Math.max(0, this.at() - 1)); e.preventDefault(); }
    else if (e.key === 'Enter') { const c = this.shown()[this.at()]; if (c) this.run(c); e.preventDefault(); }
    else if (e.key === 'Escape') { this.closed.emit(); e.preventDefault(); e.stopPropagation(); }
  }
  protected run(c: PaletteCmd) { this.closed.emit(); c.run(); }
}
