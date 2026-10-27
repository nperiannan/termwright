import { Component, inject } from '@angular/core';
import { Button } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';

/** Button bar: user-defined buttons that send text/commands. */
@Component({
  selector: 'app-button-bar',
  template: `
    @for (b of ui.settings()?.buttons ?? []; track $index) {
      <button class="bb" [title]="(b.script ? 'Script: ' + b.script : b.action ? 'Action: ' + b.action : b.send) + (b.toAll ? '  (all tabs)' : '')" (click)="press(b)"
              (contextmenu)="menu($event, $index)">{{ b.label }}@if (b.toAll) {<sup>all</sup>}</button>
    }
    <button class="bb add" title="Add button" (click)="add()">+</button>
  `,
  styles: [`
    :host { display: flex; gap: 4px; padding: 3px 6px; overflow-x: auto; background: var(--panel); border-top: 1px solid var(--border); }
    .bb { white-space: nowrap; padding: 2px 10px; font-size: calc(12px * var(--ui-scale, 1)); height: 24px; }
    .bb sup { color: var(--accent); margin-left: 3px; font-size: calc(9px * var(--ui-scale, 1)); }
    .add { color: var(--muted); }
  `],
})
export class ButtonBar {
  protected ui = inject(Ui);
  private tabs = inject(Tabs);

  press(b: Button) {
    if (b.send) void this.tabs.send(b.send, b.toAll);
    if (b.action) this.ui.actionHandler(b.action);
    if (b.script) void this.tabs.runScriptPath(b.script);
    this.tabs.handles.get(this.tabs.activeId())?.term.focus();
  }

  async add() {
    const label = await this.ui.input('New Button', 'Label');
    if (!label) return;
    const send = await this.ui.input('New Button', 'Text to send (\\r = Enter, \\t = Tab, \\x03 = Ctrl+C)', label + '\\r');
    if (send === null) return;
    const s = this.ui.settings();
    if (s) this.ui.patchSettings({ buttons: [...s.buttons, { label, send, toAll: false, action: '', script: '' }] });
  }

  menu(ev: MouseEvent, i: number) {
    const s = this.ui.settings();
    if (!s) return;
    const b = s.buttons[i];
    const set = (buttons: Button[]) => this.ui.patchSettings({ buttons });
    this.ui.showMenu(ev, [
      { label: 'Edit...', action: async () => {
        const label = await this.ui.input('Edit Button', 'Label', b.label);
        if (!label) return;
        const send = await this.ui.input('Edit Button', 'Text to send', b.send);
        if (send === null) return;
        set(s.buttons.map((x, j) => (j === i ? { ...x, label, send } : x)));
      } },
      { label: b.toAll ? 'Send to Active Tab Only' : 'Send to All Tabs', action: () => set(s.buttons.map((x, j) => (j === i ? { ...x, toAll: !x.toAll } : x))) },
      { label: 'Move Left', disabled: i === 0, action: () => { const l = [...s.buttons]; [l[i - 1], l[i]] = [l[i], l[i - 1]]; set(l); } },
      { label: 'Move Right', disabled: i === s.buttons.length - 1, action: () => { const l = [...s.buttons]; [l[i + 1], l[i]] = [l[i], l[i + 1]]; set(l); } },
      { separator: true },
      { label: 'Delete', action: () => set(s.buttons.filter((_, j) => j !== i)) },
    ]);
  }
}
