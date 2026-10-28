import { Component, computed, inject, model } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { SessionNode } from '../core/models';
import { Ui } from '../core/ui.service';
import { FolderIcon } from './folder-icon';

/** Folder field: type a folder (new folders and subfolders are made when
 *  saving, e.g. "Discovered/Lab 4"), or pick any existing folder or subfolder
 *  from the list. */
@Component({
  selector: 'app-folder-picker',
  imports: [FormsModule, FolderIcon],
  template: `
    <div class="fp">
      <app-folder-icon [name]="leaf()" [size]="18" />
      <input [ngModel]="value()" (ngModelChange)="value.set($event)" [placeholder]="placeholder" spellcheck="false" />
      <button type="button" class="pick" title="Choose an existing folder or subfolder" (click)="open($event)">
        <svg viewBox="0 0 24 24" width="14" height="14"><path d="M7 10l5 5 5-5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </button>
    </div>`,
  styles: [`
    :host { display: block; min-width: 0; }
    .fp { position: relative; display: flex; align-items: center; height: 32px; border: 1px solid var(--border); border-radius: var(--r-sm, 6px); background: var(--input-bg);
      &:focus-within { border-color: var(--accent); }
      app-folder-icon { margin-left: 9px; }
      input { flex: 1; min-width: 0; height: 100%; border: 0; background: transparent; padding: 0 8px; &:focus { border: 0; } }
      .pick { width: 30px; height: 100%; padding: 0; display: inline-flex; align-items: center; justify-content: center; border: 0; border-left: 1px solid var(--border);
        border-radius: 0 var(--r-sm, 6px) var(--r-sm, 6px) 0; background: transparent; color: var(--muted); &:hover { color: var(--text); background: var(--hover); } } }
  `],
})
export class FolderPicker {
  private ui = inject(Ui);
  readonly value = model<string>('');
  placeholder = 'Top level';
  readonly leaf = computed(() => (this.value() || '').split('/').filter(Boolean).pop() ?? '');

  private folders(): { path: string; depth: number; name: string }[] {
    const out: { path: string; depth: number; name: string }[] = [];
    const walk = (n: SessionNode, d: number) => n.children?.forEach(c => { if (c.isFolder) { out.push({ path: c.path, depth: d, name: c.name }); walk(c, d + 1); } });
    const r = this.ui.tree();
    if (r) walk(r, 0);
    return out;
  }

  open(ev: MouseEvent) {
    const box = ((ev.currentTarget as HTMLElement).parentElement as HTMLElement).getBoundingClientRect();
    const cur = this.value();
    const items = [
      { label: (cur ? '' : '✓ ') + '(Top level)', action: () => this.value.set('') },
      { separator: true },
      ...this.folders().map(f => ({ label: (f.path === cur ? '✓ ' : '') + ' '.repeat(f.depth) + f.name, hint: f.depth ? '' : '', action: () => this.value.set(f.path) })),
    ];
    if (items.length === 2) items.push({ label: 'No folders yet - type a name to make one', action: () => {} });
    const h = this.ui.menuHeight(items);
    this.ui.showMenuAt(box.left, box.bottom + 4 + h > window.innerHeight ? Math.max(8, box.top - 4 - h) : box.bottom + 4, items);
  }
}
