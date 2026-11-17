import { Component, HostListener, computed, inject, signal } from '@angular/core';
import { Ui } from '../core/ui.service';
import { tr } from '../core/i18n';
import { Icon } from '../components/icon';

/** The browser-storage key behind "Do not show this when the Emulated Lab opens". */
export const LAB_WORKFLOW_HIDE = 'tw.lab.workflowHide';
export function labWorkflowHidden(): boolean {
  try { return localStorage.getItem(LAB_WORKFLOW_HIDE) === '1'; } catch { return false; }
}

/** A one-page picture of the emulated lab workflow, from an empty KVM host to a traffic report.
 *  Shown when the Emulated Lab opens unless the person turned it off; Help → Emulated Lab
 *  Workflow… shows it again, and the checkbox there turns the automatic showing back on. */
@Component({
  selector: 'app-lab-workflow',
  imports: [Icon],
  template: `
  <div class="modal-backdrop" (click)="close()">
    <div class="modal lwf" role="dialog" [attr.aria-label]="tr('Emulated Lab workflow')" (click)="$event.stopPropagation()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="topology" [size]="17" /></span>
        <span class="mh-t">{{ tr('Emulated Lab workflow') }}<span class="mh-s">{{ tr('From an empty KVM host to a traffic report') }}</span></span>
        <span class="spacer"></span>
        <button type="button" class="x" (click)="close()" [attr.aria-label]="tr('Close')" [title]="tr('Close')"><app-icon name="close" [size]="14" /></button></div>
      <div class="lwf-body"><img [src]="src()" [alt]="tr('The nine steps of the Emulated Lab workflow')" draggable="false" /></div>
      <div class="lwf-foot">
        <label class="chk"><input type="checkbox" [checked]="hide()" (change)="setHide($any($event.target).checked)" name="hide" /> {{ tr('Do not show this when the Emulated Lab opens') }}</label>
        <span class="spacer"></span>
        <button type="button" class="primary" (click)="close()">{{ tr('Close') }}</button>
      </div>
    </div>
  </div>`,
  styles: [`
    .lwf { width: min(1500px, calc(100vw - 32px)); }
    .spacer { flex: 1 1 auto; }
    .lwf-body { overflow: auto; padding: 0; min-height: 0; flex: 1; background: var(--bg); }
    .lwf-body img { display: block; width: 100%; height: auto; user-select: none; }
    .lwf-foot { display: flex; align-items: center; gap: 12px; padding: 10px 16px; border-top: 1px solid var(--border); background: var(--chrome); }
    .chk { display: inline-flex; align-items: center; gap: 8px; cursor: pointer; }
    .x { background: transparent; border: 0; color: var(--muted); cursor: pointer; padding: 4px; border-radius: 6px; }
    .x:hover { background: var(--hover); color: var(--text); }
  `],
})
export class LabWorkflow {
  protected ui = inject(Ui);
  protected tr = tr;
  readonly hide = signal(labWorkflowHidden());
  /** The picture that matches the app's theme. */
  readonly src = computed(() => this.ui.lightUi() ? 'emulation-workflow-light.png' : 'emulation-workflow-dark.png');
  setHide(on: boolean) {
    this.hide.set(on);
    try { if (on) localStorage.setItem(LAB_WORKFLOW_HIDE, '1'); else localStorage.removeItem(LAB_WORKFLOW_HIDE); } catch { /* storage unavailable: it just shows again next time */ }
  }
  close() { this.ui.close(); }
  @HostListener('document:keydown.escape') onEsc() { this.close(); }
}
