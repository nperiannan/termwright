import { Component, ElementRef, inject, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';

/** Command window: compose a command (or several lines) and send it to the
 *  active tab or broadcast it to every connected tab. */
@Component({
  selector: 'app-command-window',
  imports: [FormsModule],
  template: `
    <div class="cw-bar">
      <span class="title">Command Window</span>
      <label><input type="radio" name="cwTarget" value="active" [(ngModel)]="target" /> Active tab</label>
      <label><input type="radio" name="cwTarget" value="all" [(ngModel)]="target" /> All connected tabs ({{ tabs.connectedCount() }})</label>
      <label><input type="radio" name="cwTarget" value="selected" [(ngModel)]="target" /> Selected tabs ({{ tabs.targets('selected').length }})</label>
      <button class="pick" title="Choose the tabs to send to" (click)="pick($event)">Choose…</button>
      <label title="Send characters immediately as you type"><input type="checkbox" [(ngModel)]="charMode" /> Send characters immediately</label>
      <span class="spacer"></span>
      <button (click)="send()" [disabled]="!text().length && !charMode">Send ⏎</button>
      <button class="icon-btn" title="Close" (click)="ui.patchSettings({ showCommandWindow: false })">✕</button>
    </div>
    <textarea #box spellcheck="false" placeholder="Type a command. Enter sends, Shift+Enter adds a line, ↑/↓ recall history."
      [ngModel]="text()" (ngModelChange)="onChange($event)" (keydown)="onKey($event)"></textarea>
  `,
  styles: [`
    :host { display: flex; flex-direction: column; height: 100%; background: var(--panel); border-top: 1px solid var(--border); }
    .cw-bar { display: flex; align-items: center; gap: 14px; padding: 3px 8px; font-size: 12px; color: var(--muted);
      .title { font-weight: 600; text-transform: uppercase; letter-spacing: .04em; }
      label { display: flex; align-items: center; gap: 4px; cursor: pointer; }
      .pick { height: 20px; font-size: 11px; padding: 0 8px; }
      .spacer { flex: 1; } }
    textarea { flex: 1; resize: none; border: 0; border-top: 1px solid var(--border); border-radius: 0; padding: 6px 10px;
      font-family: var(--mono); font-size: 13px; background: var(--input-bg); color: var(--text); outline: none; }
  `],
})
export class CommandWindow {
  protected tabs = inject(Tabs);
  protected ui = inject(Ui);
  private box = viewChild.required<ElementRef<HTMLTextAreaElement>>('box');
  readonly text = signal('');
  target: 'active' | 'all' | 'selected' = 'active';
  charMode = false;
  private history: string[] = [];
  private hIdx = -1;

  onChange(v: string) {
    if (this.charMode) {
      const prev = this.text();
      if (v.startsWith(prev) && v.length > prev.length) void this.tabs.send(v.slice(prev.length).replace(/\n/g, '\r'), this.target, true);
    }
    this.text.set(v);
  }

  onKey(ev: KeyboardEvent) {
    if (ev.key === 'Enter' && !ev.shiftKey) {
      ev.preventDefault();
      if (this.charMode) { void this.tabs.send('\r', this.target, true); this.text.set(''); return; }
      this.send();
      return;
    }
    const single = !this.text().includes('\n');
    if ((ev.key === 'ArrowUp' || ev.key === 'ArrowDown') && single && this.history.length) {
      ev.preventDefault();
      this.hIdx = ev.key === 'ArrowUp' ? Math.min(this.hIdx + 1, this.history.length - 1) : Math.max(this.hIdx - 1, -1);
      this.text.set(this.hIdx < 0 ? '' : this.history[this.hIdx]);
    }
  }

  send() {
    const t = this.text();
    if (this.charMode) return;
    const lines = t.split('\n');
    void this.tabs.send(lines.map(l => l + '\r').join(''), this.target, true);
    if (t.trim()) this.history = [t, ...this.history.filter(h => h !== t)].slice(0, 100);
    this.hIdx = -1;
    this.text.set('');
    this.box().nativeElement.focus();
  }

  focus() { this.box().nativeElement.focus(); }

  /** Menu of connected tabs to tick for "Selected tabs". */
  pick(ev: MouseEvent) {
    const live = this.tabs.tabs().filter(t => t.kind === 'terminal' && t.state === 'connected');
    this.ui.showMenu(ev, live.length ? [
      ...live.map(t => ({ label: (this.tabs.selected().has(t.id) ? '☑ ' : '☐ ') + t.title, action: () => { this.tabs.toggleSelected(t.id); this.target = 'selected'; this.pick(ev); } })),
      { separator: true },
      { label: 'Select All', action: () => { this.tabs.selected.set(new Set(live.map(t => t.id))); this.target = 'selected'; } },
      { label: 'Clear', action: () => this.tabs.selected.set(new Set()) },
    ] : [{ label: 'No connected tabs', disabled: true }]);
  }
}
