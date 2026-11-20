import { Component, ElementRef, computed, input, output, viewChild } from '@angular/core';

/** A text area for code with line numbers (Tab inserts two spaces). */
@Component({
  selector: 'app-code-area',
  template: `
    <div class="ca" [style.min-height.px]="minHeight()">
      <pre class="gut" #gut aria-hidden="true">{{ numbers() }}</pre>
      <textarea #ta [value]="value()" (input)="changed($any($event.target).value)" (scroll)="sync()" (keydown)="key($event)" [placeholder]="placeholder()"
        spellcheck="false" autocomplete="off" translate="no" [attr.aria-label]="label() || placeholder()" [readOnly]="readonly()"></textarea>
    </div>`,
  styles: [`
    :host { display: block; min-height: 0; }
    .ca { display: grid; grid-template-columns: auto 1fr; height: 100%; border: 1px solid var(--border); border-radius: var(--r, 6px); background: var(--input-bg); overflow: hidden; box-sizing: border-box; }
    .gut { margin: 0; padding: 6px 8px 6px 10px; font: 12px/18px var(--mono); color: var(--faint); text-align: right; user-select: none; overflow: hidden; background: var(--surface-2); border-right: 1px solid var(--line); min-width: 22px; }
    textarea { border: 0; border-radius: 0; margin: 0; padding: 6px 10px; font: 12px/18px var(--mono); resize: none; background: transparent; outline: none; white-space: pre; overflow: auto; min-height: 0; height: 100%; box-sizing: border-box; tab-size: 2; }
  `],
})
export class CodeArea {
  readonly value = input('');
  readonly placeholder = input('');
  readonly label = input('');
  readonly minHeight = input(120);
  readonly readonly = input(false);
  readonly valueChange = output<string>();
  private readonly ta = viewChild<ElementRef<HTMLTextAreaElement>>('ta');
  private readonly gut = viewChild<ElementRef<HTMLElement>>('gut');
  readonly numbers = computed(() => { const n = Math.max(1, (this.value() ?? '').split('\n').length); return Array.from({ length: n }, (_, i) => i + 1).join('\n'); });
  changed(v: string) { this.valueChange.emit(v); queueMicrotask(() => this.sync()); }
  sync() { const t = this.ta()?.nativeElement, g = this.gut()?.nativeElement; if (t && g) g.scrollTop = t.scrollTop; }
  key(e: KeyboardEvent) {
    if (e.key !== 'Tab' || this.readonly()) return;
    e.preventDefault();
    const t = e.target as HTMLTextAreaElement, a = t.selectionStart, b = t.selectionEnd;
    t.setRangeText('  ', a, b, 'end');
    this.changed(t.value);
  }
}
