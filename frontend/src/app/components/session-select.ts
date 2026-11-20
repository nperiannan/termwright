import { Component, ElementRef, HostListener, computed, inject, input, output, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { SessionNode } from '../core/models';
import { Ui } from '../core/ui.service';
import { SessionIcon, SessionKind } from './session-icon';
import { tr } from '../core/i18n';

interface Row { path: string; name: string; hostname: string; protocol: string; }
interface Group { path: string; rows: Row[]; }

/**
 * Picks one saved session the way the lab's host picker does: a button with the chosen session, and a
 * popover with a search (name, address or folder; matches marked) over the sessions grouped by folder.
 */
@Component({
  selector: 'app-session-select',
  imports: [FormsModule, SessionIcon],
  template: `
    <button type="button" class="pick" (click)="toggle()" [class.on]="open()" [title]="value() || placeholder()" aria-haspopup="dialog">
      @if (chosen(); as c) {
        <app-session-icon [kind]="kindOf(c.protocol)" [size]="15" />
        <span class="nm" translate="no">{{ c.name }}</span><span class="fd" translate="no">{{ folderOf(c.path) }}</span>
      } @else { <span class="ph">{{ placeholder() }}</span> }
      <span class="cv">▾</span>
    </button>
    @if (open()) {
      <div class="pop" role="dialog" [attr.aria-label]="title()" (keydown.escape)="open.set(false)">
        <div class="hd"><b>{{ title() }}</b>@if (hint()) { <div class="muted small">{{ hint() }}</div> }</div>
        <div class="as"><span class="mg">⌕</span><input #q [(ngModel)]="query" (keydown.enter)="enter()" (keydown.arrowDown)="move(1); $event.preventDefault()" (keydown.arrowUp)="move(-1); $event.preventDefault()"
          [placeholder]="tr('Name, IP address or folder')" [attr.aria-label]="tr('Name, IP address or folder')" /><span class="k">{{ tr('{0} of {1}', shownN(), all().length) }}</span></div>
        <div class="tree">
          @if (allowNone()) { <div class="se none" [class.on]="!value()" (click)="pick('')"><span class="ic0"></span><span class="ell">{{ noneLabel() }}</span></div> }
          @for (g of groups(); track g.path) {
            <div class="fo" (click)="fold(g.path)"><span class="tw">{{ folded().has(g.path) && !query.trim() ? '▸' : '▾' }}</span><span class="fi"></span><span class="ell" translate="no">{{ g.path || tr('Top level') }}</span><span class="n">{{ g.rows.length }}</span></div>
            @if (!folded().has(g.path) || query.trim()) {
              @for (r of g.rows; track r.path) {
                <div class="se" [class.on]="r.path === value()" [class.cur]="r.path === cursor()" (click)="pick(r.path)" (mouseenter)="cursor.set(r.path)">
                  <app-session-icon [kind]="kindOf(r.protocol)" [size]="18" />
                  <span class="ell" translate="no">@for (p of hl(r.name); track $index) { @if (p.m) { <mark>{{ p.t }}</mark> } @else { {{ p.t }} } }</span>
                  <span class="mono ell" translate="no">@for (p of hl(r.hostname); track $index) { @if (p.m) { <mark>{{ p.t }}</mark> } @else { {{ p.t }} } }</span>
                  <span class="ck">{{ r.path === value() ? '✓' : '' }}</span>
                </div>
              }
            }
          } @empty { <p class="muted small pad">{{ all().length ? tr('No session matches.') : emptyText() }}</p> }
        </div>
      </div>
    }
  `,
  styles: [`
    :host { position: relative; display: inline-block; min-width: 0; }
    .pick { display: inline-flex; align-items: center; gap: 7px; height: 30px; box-sizing: border-box; padding: 0 9px; max-width: 100%; width: 100%; text-align: left; }
    .pick.on { border-color: var(--accent); }
    .nm { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-weight: 500; }
    .fd { color: var(--muted); font-size: 11.5px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; flex: 1; }
    .ph { color: var(--muted); flex: 1; } .cv { color: var(--muted); font-size: 10px; margin-left: auto; }
    .pop { position: absolute; left: 0; top: calc(100% + 4px); width: 520px; max-width: 86vw; height: min(500px, 70vh); background: var(--surface); border: 1px solid color-mix(in srgb, var(--muted) 45%, var(--line)); border-radius: 12px;
      box-shadow: 0 18px 50px rgba(0,0,0,.45); display: grid; grid-template-rows: auto auto minmax(0, 1fr); z-index: 30; }
    .hd { padding: 12px 14px 4px; b { font-size: 14px; } }
    .muted { color: var(--muted); } .small { font-size: 11.5px; } .pad { padding: 8px; }
    .as { margin: 6px 14px 8px; height: 34px; border: 1px solid var(--accent); border-radius: 8px; display: flex; align-items: center; padding: 0 10px; gap: 8px;
      .mg { color: var(--muted); } input { flex: 1; min-width: 0; border: 0; background: transparent; outline: none; height: 30px; padding: 0; font-size: 12.5px; }
      .k { font-size: 10.5px; color: var(--muted); border: 1px solid var(--line); border-radius: 5px; padding: 0 5px; white-space: nowrap; } }
    .tree { overflow: auto; padding: 0 8px 8px; }
    .fo { display: flex; align-items: center; gap: 7px; padding: 6px 8px; font-weight: 600; font-size: 12.5px; cursor: pointer; border-radius: 6px;
      &:hover { background: var(--surface-2); } .tw { color: var(--muted); width: 10px; } .fi { width: 16px; height: 12px; border-radius: 2px; background: #d6a33c; display: inline-block; flex: none; }
      .n { margin-left: auto; color: var(--muted); font-weight: 400; font-size: 11px; } }
    .se { display: grid; grid-template-columns: 22px minmax(0, 1fr) 150px 16px; align-items: center; gap: 8px; padding: 5px 8px 5px 26px; border-radius: 8px; font-size: 12.5px; cursor: pointer;
      &:hover, &.cur { background: var(--surface-2); } &.on { background: color-mix(in srgb, var(--accent) 16%, transparent); }
      .mono { font-family: var(--mono, monospace); font-size: 11.5px; color: var(--muted); } .ck { color: var(--accent); }
      mark { background: color-mix(in srgb, var(--warning, #e39b17) 35%, transparent); color: inherit; border-radius: 3px; padding: 0 1px; } }
    .se.none { padding-left: 8px; } .ic0 { width: 18px; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  `],
})
export class SessionSelect {
  readonly value = input('');
  /** Which sessions are offered. */
  readonly filter = input<(n: SessionNode) => boolean>(() => true);
  readonly placeholder = input('Choose a session…');
  readonly title = input('Choose a session');
  readonly hint = input('');
  readonly allowNone = input(false);
  readonly noneLabel = input('No session');
  readonly emptyText = input('No session yet.');
  readonly changed = output<string>();
  private readonly ui = inject(Ui);
  private readonly host = inject(ElementRef<HTMLElement>);
  private readonly qEl = viewChild<ElementRef<HTMLInputElement>>('q');
  readonly tr = tr;

  readonly open = signal(false);
  readonly folded = signal<Set<string>>(new Set());
  readonly cursor = signal('');
  query = '';

  readonly all = computed<Row[]>(() => {
    const out: Row[] = [], f = this.filter();
    const walk = (n: SessionNode | null | undefined) => n?.children?.forEach(c => {
      if (c.isFolder) walk(c);
      else if (f(c)) out.push({ path: c.path, name: c.name, hostname: c.hostname ?? '', protocol: c.protocol ?? '' });
    });
    walk(this.ui.tree());
    return out;
  });
  readonly chosen = computed(() => this.all().find(r => r.path === this.value()) ?? (this.value() ? { path: this.value(), name: this.value().split('/').pop() ?? '', hostname: '', protocol: '' } : null));

  groups(): Group[] {
    const q = this.query.trim().toLowerCase(), m = new Map<string, Group>();
    for (const r of this.all()) {
      if (q && !r.name.toLowerCase().includes(q) && !r.hostname.toLowerCase().includes(q) && !r.path.toLowerCase().includes(q)) continue;
      const f = this.folderOf(r.path);
      const g = m.get(f) ?? { path: f, rows: [] };
      g.rows.push(r); m.set(f, g);
    }
    return [...m.values()];
  }
  shownN() { return this.groups().reduce((a, g) => a + g.rows.length, 0); }
  private visible(): string[] { return this.groups().flatMap(g => (this.folded().has(g.path) && !this.query.trim()) ? [] : g.rows.map(r => r.path)); }

  toggle() {
    const on = !this.open();
    this.open.set(on);
    if (on) {
      this.query = '';
      // folders start folded except the chosen session's, when there are many
      const groups = this.groups();
      if (this.all().length > 40) this.folded.set(new Set(groups.map(g => g.path).filter(p => p !== this.folderOf(this.value()))));
      this.cursor.set(this.value());
      setTimeout(() => { this.qEl()?.nativeElement.focus(); this.host.nativeElement.querySelector('.se.on')?.scrollIntoView({ block: 'center' }); });
    }
  }
  fold(p: string) { const s = new Set(this.folded()); if (s.has(p)) s.delete(p); else s.add(p); this.folded.set(s); }
  pick(p: string) { this.open.set(false); if (p !== this.value()) this.changed.emit(p); }
  move(d: number) {
    const v = this.visible(); if (!v.length) return;
    const i = v.indexOf(this.cursor());
    this.cursor.set(v[Math.min(v.length - 1, Math.max(0, i + d))]);
    setTimeout(() => this.host.nativeElement.querySelector('.se.cur')?.scrollIntoView({ block: 'nearest' }));
  }
  enter() { const v = this.visible(); const c = v.includes(this.cursor()) ? this.cursor() : v[0]; if (c) this.pick(c); }
  folderOf(p: string) { const i = p.lastIndexOf('/'); return i < 0 ? '' : p.slice(0, i); }
  kindOf(p: string): SessionKind {
    return ({ Serial: 'serial', Console: 'console', 'Local Shell': 'shell', Telnet: 'telnet', Raw: 'raw', RDP: 'rdp', VNC: 'vnc', WOL: 'wol', API: 'api', SNMP: 'snmp' } as Record<string, SessionKind>)[p] ?? 'ssh';
  }
  hl(s: string) {
    const q = this.query.trim(); if (!q || !s) return [{ t: s, m: false }];
    const out: { t: string; m: boolean }[] = []; const low = s.toLowerCase(), ql = q.toLowerCase(); let i = 0;
    for (let j = low.indexOf(ql); j >= 0; j = low.indexOf(ql, i)) { if (j > i) out.push({ t: s.slice(i, j), m: false }); out.push({ t: s.slice(j, j + q.length), m: true }); i = j + q.length; }
    if (i < s.length) out.push({ t: s.slice(i), m: false });
    return out;
  }
  @HostListener('document:mousedown', ['$event'])
  outside(e: MouseEvent) { if (this.open() && !this.host.nativeElement.contains(e.target as Node)) this.open.set(false); }
}
