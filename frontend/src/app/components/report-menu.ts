import { Component, input, output, signal } from '@angular/core';
import { tr } from '../core/i18n';

export type ReportKind = 'html' | 'pdf' | 'xlsx' | 'csv';

const KINDS: { k: ReportKind; name: string; what: string }[] = [
  { k: 'html', name: 'HTML Report…', what: 'One page to open in any browser or attach' },
  { k: 'pdf', name: 'PDF Report…', what: 'To print or keep: printed by Chrome, Edge or Brave' },
  { k: 'xlsx', name: 'Excel Workbook…', what: 'Every number in sheets, with Excel\'s own charts' },
  { k: 'csv', name: 'CSV…', what: 'The streams table only' },
];

/** Report ▾: the formats a traffic report is saved in (the Traffic page and the bottom pane's Traffic tab). */
@Component({
  selector: 'app-report-menu',
  host: { '(document:click)': 'open.set(false)', class: 'rpm' },
  template: `
    <button type="button" [class.sm]="small()" (click)="open.set(!open()); $event.stopPropagation()" [disabled]="disabled()" [attr.aria-expanded]="open()" aria-haspopup="menu"
      [title]="tr('The run\\'s report to share and keep: graphs, topology, frames sent and what arrived')">{{ tr('Report') }} ▾</button>
    @if (open()) {
      <div class="rpmm" role="menu" [class.up]="up()" (click)="$event.stopPropagation()">
        @for (f of shown(); track f.k) { <button type="button" role="menuitem" (click)="open.set(false); pick.emit(f.k)"><b>{{ tr(f.name) }}</b><span>{{ tr(f.what) }}</span></button> }
      </div>
    }`,
  styles: [`
    :host { position: relative; display: inline-block; }
    :host > button { white-space: nowrap; }
    .rpmm { position: absolute; right: 0; top: calc(100% + 4px); z-index: 40; background: var(--surface); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 8px 24px rgba(0,0,0,.18); padding: 4px; display: flex; flex-direction: column; min-width: 340px; }
    .rpmm.up { top: auto; bottom: calc(100% + 4px); }
    .rpmm button { display: flex; flex-direction: column; align-items: flex-start; gap: 2px; height: auto; min-height: 0; line-height: 1.3; border: 0; background: none; text-align: left; padding: 7px 10px; border-radius: 6px; white-space: normal; }
    .rpmm button:hover { background: var(--hover, rgba(127,127,127,.12)); }
    .rpmm span { color: var(--muted); font-size: 11.5px; }
  `],
})
export class ReportMenu {
  readonly disabled = input(false);
  readonly small = input(false);
  /** Open upwards (a button at the bottom of the window). */
  readonly up = input(false);
  /** The formats offered (all four by default). */
  readonly kinds = input<ReportKind[]>(['html', 'pdf', 'xlsx', 'csv']);
  readonly pick = output<ReportKind>();
  protected open = signal(false);
  /** What the CSV holds. */
  readonly csvWhat = input('The streams table only');
  protected shown() { return KINDS.filter(k => this.kinds().includes(k.k)).map(k => k.k === 'csv' ? { ...k, what: this.csvWhat() } : k); }
  protected tr = tr;
}
