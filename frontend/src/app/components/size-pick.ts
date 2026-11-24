import { Component, computed, input, model, signal } from '@angular/core';

/** Browser page sizes: the usual screen resolutions, or a custom WIDTHxHEIGHT. */
export const PAGE_SIZES: { label: string; size: string }[] = [
  { label: 'qHD · 960×540', size: '960x540' },
  { label: 'HD · 1280×720', size: '1280x720' },
  { label: 'Full HD · 1920×1080', size: '1920x1080' },
  { label: '2K · 2560×1440', size: '2560x1440' },
  { label: '3K · 2880×1620', size: '2880x1620' },
  { label: '4K · 3840×2160', size: '3840x2160' },
];

/** A page size picker: a preset, the default, or Custom with its own WIDTHxHEIGHT. */
@Component({
  selector: 'app-size-pick',
  standalone: true,
  template: `<span class="sp">
    <select (change)="pick($any($event.target).value)" [attr.aria-label]="'Page size'">
      <option value="" [selected]="choice() === ''">{{ empty() }}</option>
      @for (p of sizes; track p.size) { <option [value]="p.size" [selected]="choice() === p.size">{{ p.label }}</option> }
      <option value="custom" [selected]="choice() === 'custom'">Custom…</option>
    </select>
    @if (choice() === 'custom') {
      <input class="cust" [value]="value() ?? ''" (input)="custom($any($event.target).value)" placeholder="1600x900" spellcheck="false" [class.bad]="!!value() && !valid()"
        title="Width x height in pixels, e.g. 1600x900 or 390x844 for a phone" />
    }
  </span>`,
  styles: [`.sp { display: inline-flex; gap: 6px; align-items: center; min-width: 0; } select { min-width: 0; } .cust { width: 110px; } .bad { border-color: var(--danger, #ef6161) !important; }`],
})
export class SizePick {
  readonly value = model<string | undefined>();
  /** What no size means here ("Browser default", "As the app"). */
  readonly empty = input('Browser default');
  readonly sizes = PAGE_SIZES;
  private readonly customOn = signal(false);
  readonly choice = computed(() => {
    const v = (this.value() ?? '').replace(/\s+/g, '').replace('×', 'x');
    if (!v) return this.customOn() ? 'custom' : '';
    return PAGE_SIZES.some(p => p.size === v) ? v : 'custom';
  });
  readonly valid = computed(() => /^\d{2,5}\s*[x×]\s*\d{2,5}$/.test(this.value() ?? ''));

  pick(v: string) {
    this.customOn.set(v === 'custom');
    if (v === 'custom') { if (!this.value() || PAGE_SIZES.some(p => p.size === this.value())) this.value.set(''); return; }
    this.value.set(v || undefined);
  }
  custom(v: string) { this.customOn.set(true); this.value.set(v.trim().replace('×', 'x')); }
}
