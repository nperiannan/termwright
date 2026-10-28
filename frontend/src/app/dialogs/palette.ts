import { Component, ElementRef, computed, inject, signal, viewChild, AfterViewInit } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ACTIONS, SessionNode, snippetTypes } from '../core/models';
import { Tabs } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { Backend } from '../core/backend';
import { connectString, looksLikeConnect } from '../core/connect-string';

interface Item { kind: 'connect' | 'session' | 'tab' | 'action' | 'command'; label: string; detail: string; run: (alt: boolean) => void; boost?: number; }

/** Quick Open (Cmd+G / Ctrl+Shift+G, or the search box in the title bar):
 *  sessions, open tabs, menu actions and saved commands; an address or a
 *  connect line ("ssh admin@10.1.1.1", "voss 10.1.1.1") connects. */
@Component({
  selector: 'app-palette',
  imports: [FormsModule],
  template: `
  <div class="modal-backdrop pal-back" (mousedown)="ui.close()">
    <div class="pal" (mousedown)="$event.stopPropagation()">
      <input #q class="pal-q" [ngModel]="query()" (ngModelChange)="query.set($event); sel.set(0)" (keydown)="key($event)"
        placeholder="Search sessions, tabs, commands - or type an address to connect" spellcheck="false" />
      <div class="pal-list">
        @for (it of results(); track $index) {
          <div class="pal-row" [class.on]="$index === sel()" (mouseenter)="sel.set($index)" (click)="choose(it, $event.altKey)">
            <span class="kind {{ it.kind }}">{{ it.kind }}</span>
            <span class="lbl">{{ it.label }}</span>
            <span class="det">{{ it.detail }}</span>
          </div>
        } @empty { <div class="pal-empty">Nothing matches</div> }
      </div>
      <div class="pal-foot"><span><kbd>↵</kbd> open</span><span><kbd>Alt</kbd>+<kbd>↵</kbd> SFTP</span><span><kbd>Esc</kbd> close</span>
        <span class="grow"></span><span>ssh user&#64;host · telnet host 23 · serial COM3 115200</span></div>
    </div>
  </div>`,
  styles: [`
    .pal-back { align-items: flex-start; padding-top: 12vh; }
    .pal { width: 640px; max-width: calc(100vw - 32px); background: var(--panel); border: 1px solid var(--border); border-radius: 8px;
      box-shadow: 0 12px 40px rgba(0,0,0,.45); overflow: hidden; }
    .pal-q { width: 100%; border: 0; border-bottom: 1px solid var(--border); border-radius: 0; padding: 12px 14px; font-size: calc(15px * var(--ui-scale, 1)); outline: none; }
    .pal-list { max-height: 50vh; overflow: auto; padding: 4px 0; }
    .pal-row { display: flex; align-items: center; gap: 10px; padding: 6px 14px; cursor: pointer;
      &.on { background: var(--selected); }
      .kind { font-size: calc(10px * var(--ui-scale, 1)); text-transform: uppercase; color: var(--muted); width: 58px; flex: none; }
      .lbl { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
      .det { margin-left: auto; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 45%; } }
    .pal-empty { padding: 12px 14px; color: var(--muted); }
    .kind.connect { color: var(--accent); font-weight: 600; }
    .pal-foot { display: flex; gap: 14px; align-items: center; padding: 7px 14px; border-top: 1px solid var(--border); color: var(--muted);
      font-size: calc(11px * var(--ui-scale, 1)); white-space: nowrap; overflow: hidden; .grow { flex: 1; }
      kbd { font: inherit; padding: 0 4px; border: 1px solid var(--border); border-radius: 4px; } }
  `],
})
export class Palette implements AfterViewInit {
  protected ui = inject(Ui);
  private tabs = inject(Tabs);
  private backend = inject(Backend);
  private q = viewChild.required<ElementRef<HTMLInputElement>>('q');
  readonly query = signal('');
  readonly sel = signal(0);

  private readonly all = computed<Item[]>(() => {
    const out: Item[] = [];
    for (const t of this.tabs.tabs()) {
      out.push({ kind: 'tab', label: t.title, detail: t.info || t.state, run: () => this.tabs.activeId.set(t.id) });
    }
    const walk = (n: SessionNode) => (n.children ?? []).forEach(c => {
      if (c.isFolder) { walk(c); return; }
      const folder = c.path.includes('/') ? c.path.slice(0, c.path.lastIndexOf('/')) : '';
      out.push({ kind: 'session', label: c.name, detail: [c.hostname, folder].filter(Boolean).join(' · '),
        run: alt => void this.tabs.openSession(c.path, alt ? 'sftp' : 'auto') });
    });
    const root = this.ui.tree();
    if (root) walk(root);
    const dt = this.tabs.activeDeviceType().toLowerCase();
    const known = (this.ui.settings()?.deviceTypes ?? []).map(d => d.name);
    for (const c of this.ui.settings()?.snippets ?? []) {
      const types = snippetTypes(c, known);
      const mine = !!dt && types.some(t => t.toLowerCase() === dt);
      out.push({ kind: 'command', label: c.name, detail: (mine ? 'for ' + this.tabs.activeDeviceType() + ' · ' : '') + (c.folder || c.text.split('\n')[0]),
        run: () => this.ui.runSnippet?.(c), boost: mine ? -0.6 : types.length && dt ? 0.4 : 0 });
    }
    for (const [a, label] of Object.entries(ACTIONS)) {
      out.push({ kind: 'action', label: label.replace(/\.\.\.$/, '…'), detail: '', run: () => this.ui.actionHandler(a) });
    }
    return out;
  });

  readonly results = computed(() => {
    const q = this.query().trim().toLowerCase();
    if (!q) return this.all().slice(0, 60);
    const words = q.split(/\s+/);
    const scored: { it: Item; score: number }[] = [];
    for (const it of this.all()) {
      const hay = (it.label + ' ' + it.detail).toLowerCase();
      if (!words.every(w => hay.includes(w))) continue;
      const l = it.label.toLowerCase();
      const score = (l.startsWith(q) ? 0 : l.includes(q) ? 1 : 2) + (it.kind === 'action' ? 0.5 : 0) + (it.boost ?? 0);
      scored.push({ it, score });
    }
    const list = scored.sort((a, b) => a.score - b.score).slice(0, 60).map(s => s.it);
    const text = this.query().trim();
    if (looksLikeConnect(text, this.ui)) {
      const conn: Item = { kind: 'connect', label: 'Connect to ' + text, detail: 'new connection', run: () => void connectString(text, this.ui, this.tabs, this.backend) };
      // a session with this very name opens first; otherwise the connection does
      const exact = list[0]?.kind === 'session' && list[0].label.toLowerCase() === q;
      list.splice(exact ? 1 : 0, 0, conn);
    }
    return list;
  });

  ngAfterViewInit() { setTimeout(() => this.q().nativeElement.focus(), 0); }

  key(ev: KeyboardEvent) {
    const n = this.results().length;
    if (ev.key === 'ArrowDown') { ev.preventDefault(); this.sel.set(n ? (this.sel() + 1) % n : 0); }
    else if (ev.key === 'ArrowUp') { ev.preventDefault(); this.sel.set(n ? (this.sel() - 1 + n) % n : 0); }
    else if (ev.key === 'Escape') { this.ui.close(); }
    else if (ev.key === 'Enter') {
      ev.preventDefault();
      const it = this.results()[this.sel()];
      if (it) this.choose(it, ev.altKey);
    }
  }

  choose(it: Item, alt: boolean) {
    this.ui.close();
    it.run(alt);
  }
}
