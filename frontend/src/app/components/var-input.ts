import { Component, ElementRef, input, output, viewChild } from '@angular/core';

/** A one-line box whose {{variables}} are coloured: green when they have a value, red when not. */
@Component({
  selector: 'app-var-input',
  template: `
    <div class="vi">
      <div class="hl" #hl aria-hidden="true">@for (p of parts(); track $index) { @if (p.v) { <span [class]="p.ok ? 'ok' : 'no'">{{ p.t }}</span> } @else { <span>{{ p.t }}</span> } }&nbsp;</div>
      <input #box [value]="value()" (input)="changed($any($event.target).value)" (scroll)="sync()" (keyup)="sync()" (click)="sync()" (keydown.enter)="enter.emit()"
        [placeholder]="placeholder()" spellcheck="false" autocomplete="off" translate="no" [attr.aria-label]="label() || placeholder()" [title]="tip()" />
    </div>`,
  styles: [`
    :host { display: block; min-width: 0; }
    .vi { position: relative; height: 100%; }
    .hl, input { font: 12.5px var(--mono); padding: 0 9px; box-sizing: border-box; height: 100%; letter-spacing: 0; }
    .hl { position: absolute; inset: 0; white-space: pre; overflow: hidden; pointer-events: none; display: flex; align-items: center; border: 1px solid transparent; color: var(--text); }
    input { position: relative; width: 100%; color: transparent; caret-color: var(--text); background: transparent; }
    .vi { background: var(--input-bg); border-radius: 5px; }
    input::placeholder { color: var(--faint); }
    input::selection { background: color-mix(in srgb, var(--accent) 35%, transparent); color: transparent; }
    .ok { color: var(--ok); background: color-mix(in srgb, var(--ok) 13%, transparent); border-radius: 3px; }
    .no { color: var(--danger); background: color-mix(in srgb, var(--danger) 14%, transparent); border-radius: 3px; }
  `],
})
export class VarInput {
  readonly value = input('');
  readonly placeholder = input('');
  readonly label = input('');
  /** Names that have a value. */
  readonly known = input<Set<string>>(new Set());
  readonly valueChange = output<string>();
  readonly enter = output<void>();
  private readonly box = viewChild<ElementRef<HTMLInputElement>>('box');
  private readonly hl = viewChild<ElementRef<HTMLElement>>('hl');
  parts() {
    const s = this.value() ?? '', out: { t: string; v: boolean; ok: boolean }[] = [], re = /\{\{\s*([^{}]+?)\s*\}\}/g;
    let i = 0, m: RegExpExecArray | null;
    while ((m = re.exec(s))) {
      if (m.index > i) out.push({ t: s.slice(i, m.index), v: false, ok: false });
      out.push({ t: m[0], v: true, ok: m[1].startsWith('$') || this.known().has(m[1]) });
      i = m.index + m[0].length;
    }
    if (i < s.length) out.push({ t: s.slice(i), v: false, ok: false });
    return out;
  }
  tip() { const bad = this.parts().filter(p => p.v && !p.ok).map(p => p.t); return bad.length ? 'No value for ' + bad.join(', ') : ''; }
  changed(v: string) { this.valueChange.emit(v); queueMicrotask(() => this.sync()); }
  sync() { const b = this.box()?.nativeElement, h = this.hl()?.nativeElement; if (b && h) h.scrollLeft = b.scrollLeft; }
}
