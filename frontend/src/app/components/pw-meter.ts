import { Component, computed, input } from '@angular/core';
import { pwStrength } from '../core/pw-strength';

/** Four bars and a word (Weak, Fair, Good, Strong) for a password being typed. */
@Component({
  selector: 'app-pw-meter',
  template: `
    <div class="m" [class.empty]="!pw()">
      <span class="bars">@for (i of [1, 2, 3, 4]; track i) { <i [style.background]="i <= s().score ? s().color : null"></i> }</span>
      <span class="l" [style.color]="s().color">{{ s().label || 'At least 4 characters - a longer phrase is stronger' }}</span>
    </div>`,
  styles: [`
    .m { display: flex; align-items: center; gap: 10px; margin-top: 6px; font-size: calc(12px * var(--ui-scale, 1)); }
    .bars { display: inline-flex; gap: 4px; i { width: 34px; height: 4px; border-radius: 2px; background: var(--line); } }
    .empty .l { color: var(--muted) !important; }
  `],
})
export class PwMeter {
  readonly pw = input('');
  readonly s = computed(() => pwStrength(this.pw()));
}
