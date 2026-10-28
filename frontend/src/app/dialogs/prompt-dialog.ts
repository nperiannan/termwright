import { Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Autofocus } from '../core/autofocus';
import { Backend } from '../core/backend';
import { Prompt } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Icon } from '../components/icon';

/** Shows backend prompts (passwords, host keys, keyboard-interactive) one at a time. */
@Component({
  selector: 'app-prompt-dialog',
  imports: [FormsModule, Autofocus, Icon],
  template: `
  @if (current(); as p) {
    <div class="modal-backdrop">
      <form class="modal" (ngSubmit)="answer(true, p.choices?.[0] ?? '')" autocomplete="off">
        <div class="modal-head" [class.danger]="p.danger"><span class="mh-ic" [class.dz]="p.danger"><app-icon [name]="promptIcon(p)" [size]="17" /></span>
          <span class="mh-t">{{ p.title }}@if (tabTitle(); as t) {<span class="mh-s">{{ t }}</span>}</span></div>
        <div class="modal-body">
          @if (tabTitle(); as t) { <div class="hint sess">Session: {{ t }}</div> }
          @if (p.message) { <pre class="msg">{{ p.message }}</pre> }
          @for (f of p.fields ?? []; track $index) {
            <label class="field">{{ f.label }}
              <input [name]="'f' + $index" [type]="f.secret ? 'password' : 'text'" [(ngModel)]="values[$index]"
                     [appAutofocus]="$index === 0" />
            </label>
          }
          @if (p.allowRemember) {
            <label class="inline-check"><input type="checkbox" name="rem" [(ngModel)]="remember" /> {{ p.rememberLabel || 'Save ' + (p.title === 'Key Passphrase' ? 'passphrase' : 'password') + ' (OS keychain)' }}</label>
          }
        </div>
        <div class="modal-foot">
          <span class="spacer"></span>
          <button type="button" (click)="answer(false, '')">Cancel</button>
          @if (p.choices?.length) {
            @for (c of p.choices!.slice(1); track c) { <button type="button" (click)="answer(true, c)">{{ c }}</button> }
            <button type="submit" class="primary" [class.danger]="p.danger">{{ p.choices![0] }}</button>
          } @else {
            <button type="submit" class="primary">OK</button>
          }
        </div>
      </form>
    </div>
  }`,
  styles: [`
    .msg { white-space: pre-wrap; font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); background: var(--input-bg); padding: 8px; border-radius: 4px; margin: 0 0 10px; user-select: text; }
    .field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
    .modal-head.danger { background: var(--danger); color: #fff; }
    :host-context(.modern) .modal-head.danger { background: var(--surface); color: var(--text); }
    .mh-ic.dz { background: rgba(229, 72, 77, .14) !important; color: var(--danger) !important; }
    :host-context(.modern) .sess { display: none; }
    :host-context(.modern) .field { font-size: calc(12px * var(--ui-scale, 1)); font-weight: 500; color: var(--muted); input { font-size: calc(13px * var(--ui-scale, 1)); color: var(--text); min-height: 32px; } }
    :host-context(.modern) .msg { background: var(--surface-2); border: 1px solid var(--line); border-radius: var(--r); padding: 9px 11px; }
  `],
})
export class PromptDialog {
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  readonly queue = signal<Prompt[]>([]);
  readonly current = computed(() => this.queue()[0] ?? null);
  readonly tabTitle = computed(() => {
    const p = this.current();
    return p ? this.tabs.get(p.tabId)?.title ?? '' : '';
  });
  values: string[] = [];
  promptIcon(p: Prompt) { return p.danger ? 'alert' : (p.fields ?? []).some(f => f.secret) ? 'key' : 'lock'; }
  remember = false;

  constructor() {
    this.backend.on('prompt', (p: Prompt) => {
      if (!this.queue().length) this.reset(p);
      this.queue.update(q => [...q, p]);
      if (p.tabId) this.tabs.activeId.set(p.tabId);
    });
    this.backend.on('prompt:done', (id: string) => this.drop(id));
  }

  private reset(p: Prompt) {
    this.values = (p.fields ?? []).map(f => f.value ?? '');
    this.remember = false;
  }

  private drop(id: string) {
    const was = this.current()?.id;
    this.queue.update(q => q.filter(x => x.id !== id));
    const next = this.current();
    if (next && next.id !== was) this.reset(next);
  }

  answer(ok: boolean, choice: string) {
    const p = this.current();
    if (!p) return;
    void this.backend.answerPrompt(p.id, { ok, values: [...this.values], choice, remember: this.remember });
    this.drop(p.id);
    if (!this.current()) this.tabs.handles.get(p.tabId)?.term.focus();
  }
}
