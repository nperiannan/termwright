import { AfterViewInit, Component, ElementRef, OnDestroy, effect, inject, input, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Terminal } from '@xterm/xterm';
import { SearchAddon } from '@xterm/addon-search';
import { Unicode11Addon } from '@xterm/addon-unicode11';
import { Backend, errText } from '../core/backend';
import { RecordingData } from '../core/models';
import { TabView } from '../core/tabs.service';
import { terminalTheme } from '../core/themes';
import { Ui } from '../core/ui.service';

interface Ev { t: number; v: number; code: string; data: string; }

/** Longest pause kept when "Skip idle time" is on (seconds). */
const IDLE = 1.5;

/** Plays a session recording (asciinema v2 .cast) with its timing. */
@Component({
  selector: 'app-player-view',
  imports: [FormsModule],
  template: `
    <div class="bar">
      <button type="button" class="play" (click)="toggle()" [disabled]="!events.length" [title]="playing() ? 'Pause (Space)' : 'Play (Space)'">{{ playing() ? '❚❚' : '▶' }}</button>
      <span class="time">{{ fmt(pos()) }} / {{ fmt(total()) }}</span>
      <input type="range" class="seek" min="0" [max]="total()" step="0.1" [ngModel]="pos()" (ngModelChange)="seek(+$event)" [disabled]="!events.length" />
      <select [ngModel]="speed()" (ngModelChange)="speed.set(+$event)" title="Speed">
        @for (s of speeds; track s) { <option [value]="s">{{ s }}×</option> }
      </select>
      <label class="chk" title="Shorten pauses longer than {{ idle }} s"><input type="checkbox" [ngModel]="skipIdle()" (ngModelChange)="setSkip($event)" /> Skip idle</label>
      @if (markers().length) {
        <select class="mk" [ngModel]="''" (ngModelChange)="$event !== '' && jumpMarker(+$event)" title="Markers">
          <option value="">Markers ({{ markers().length }})</option>
          @for (m of markers(); track $index) { <option [value]="$index">{{ fmt(m.v) }} {{ m.data }}</option> }
        </select>
      }
      <input class="q" [(ngModel)]="query" placeholder="Find text…" (keydown.enter)="find()" spellcheck="false" />
      <button type="button" (click)="find()" [disabled]="!query.trim()">Find</button>
      <span class="spacer"></span>
      <button type="button" (click)="exportText()" [disabled]="!events.length" title="Save everything the device showed as plain text">Export Text…</button>
      <button type="button" (click)="reveal()" title="Show the .cast file in its folder">Show</button>
    </div>
    @if (info()) { <div class="info">{{ info() }}@if (flash()) { <span class="flash">◆ {{ flash() }}</span> }</div> }
    @if (err()) { <div class="err">{{ err() }}</div> }
    <div class="stage" #stage tabindex="0" (keydown)="key($event)">
      <div class="scaler" [style.transform]="'scale(' + scale() + ')'"><div #host class="host"></div></div>
    </div>
  `,
  styles: [`
    :host { display: flex; flex-direction: column; position: absolute; inset: 0; background: var(--bg); }
    .bar { display: flex; align-items: center; gap: 8px; padding: 5px 8px; border-bottom: 1px solid var(--border); font-size: 12px; flex-wrap: wrap;
      button, select { height: 24px; font-size: 12px; } .play { width: 34px; } .seek { flex: 1; min-width: 100px; accent-color: var(--accent); }
      .time { font-family: var(--mono); color: var(--muted); white-space: nowrap; } .q { width: 110px; height: 24px; } .mk { max-width: 190px; }
      .chk { display: inline-flex; gap: 4px; align-items: center; color: var(--muted); } .spacer { flex: 1; } }
    .info { padding: 3px 10px; font-size: 11.5px; color: var(--muted); border-bottom: 1px solid var(--border); .flash { margin-left: 12px; color: #f59e0b; } }
    .err { padding: 6px 10px; color: var(--danger); }
    .stage { flex: 1; overflow: hidden; position: relative; outline: none; padding: 8px; }
    .scaler { transform-origin: 0 0; display: inline-block; }
  `],
})
export class PlayerView implements AfterViewInit, OnDestroy {
  readonly tab = input.required<TabView>();
  readonly visible = input(true);
  private backend = inject(Backend);
  private ui = inject(Ui);
  private host = viewChild.required<ElementRef<HTMLDivElement>>('host');
  private stage = viewChild.required<ElementRef<HTMLDivElement>>('stage');
  readonly speeds = [0.5, 1, 2, 4, 8];
  readonly idle = IDLE;
  readonly pos = signal(0);
  readonly total = signal(0);
  readonly playing = signal(false);
  readonly speed = signal(1);
  readonly skipIdle = signal(true);
  readonly scale = signal(1);
  readonly info = signal('');
  readonly err = signal('');
  readonly flash = signal('');
  readonly markers = signal<Ev[]>([]);
  query = '';
  events: Ev[] = [];
  private rec?: RecordingData;
  private term?: Terminal;
  private search?: SearchAddon;
  private idx = 0; // next event to apply
  private timer?: ReturnType<typeof setInterval>;
  private last = 0;
  private ro?: ResizeObserver;
  private flashTimer?: ReturnType<typeof setTimeout>;

  constructor() {
    effect(() => { if (this.visible()) setTimeout(() => this.fitScale()); });
  }

  async ngAfterViewInit() {
    const s = this.ui.settings();
    this.term = new Terminal({
      fontFamily: s?.fontFamily || 'monospace', fontSize: s?.fontSize || 14, scrollback: 5000, allowProposedApi: true,
      theme: terminalTheme('', s, this.ui.lightUi()), cursorBlink: false, disableStdin: true, convertEol: false,
    });
    const uni = new Unicode11Addon();
    this.term.loadAddon(uni);
    this.term.unicode.activeVersion = '11';
    this.search = new SearchAddon();
    this.term.loadAddon(this.search);
    this.term.open(this.host().nativeElement);
    this.ro = new ResizeObserver(() => this.fitScale());
    this.ro.observe(this.stage().nativeElement);
    try {
      this.rec = await this.backend.loadRecording(this.tab().playFile!);
      const h = this.rec.header;
      this.term.resize(Math.max(h.width, 10), Math.max(h.height, 4));
      this.build();
      const when = h.timestamp ? new Date(h.timestamp * 1000).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }) : '';
      const who = [h.termwright_user, h.termwright_host].filter(Boolean).join('@');
      this.info.set([h.termwright_session || h.title, who, when, `${h.width}×${h.height}`].filter(Boolean).join(' · '));
      this.fitScale();
      this.toggle();
    } catch (e) { this.err.set('Cannot play this recording: ' + errText(e)); }
  }

  ngOnDestroy() {
    clearInterval(this.timer);
    clearTimeout(this.flashTimer);
    this.ro?.disconnect();
    this.term?.dispose();
  }

  /** Event times on the playback clock (pauses shortened when skipping idle time). */
  private build() {
    const evs = this.rec?.events ?? [];
    let prev = 0, v = 0;
    this.events = evs.map(([t, code, data]) => {
      const gap = Math.max(0, t - prev);
      prev = t;
      v += this.skipIdle() ? Math.min(gap, IDLE) : gap;
      return { t, v, code, data };
    });
    this.total.set(this.events.length ? this.events[this.events.length - 1].v : 0);
    this.markers.set(this.events.filter(e => e.code === 'm'));
  }

  setSkip(on: boolean) {
    const real = this.events[Math.max(this.idx - 1, 0)]?.t ?? 0;
    this.skipIdle.set(on);
    this.build();
    // stay at the same point of the recording
    const i = this.events.findIndex(e => e.t >= real);
    this.pos.set(i >= 0 ? this.events[i].v : this.total());
  }

  private fitScale() {
    const host = this.host()?.nativeElement, stage = this.stage()?.nativeElement;
    const screen = host?.querySelector('.xterm-screen') as HTMLElement | null;
    if (!screen || !stage) return;
    const w = stage.clientWidth - 16, h = stage.clientHeight - 16;
    this.scale.set(Math.min(1, w / screen.offsetWidth, h / screen.offsetHeight) || 1);
  }

  private apply(e: Ev) {
    if (!this.term) return;
    if (e.code === 'o') this.term.write(e.data);
    else if (e.code === 'r') {
      const m = /^(\d+)x(\d+)$/.exec(e.data);
      if (m) { this.term.resize(+m[1], +m[2]); setTimeout(() => this.fitScale()); }
    } else if (e.code === 'm') {
      this.flash.set(e.data || 'Marker');
      clearTimeout(this.flashTimer);
      this.flashTimer = setTimeout(() => this.flash.set(''), 4000);
    }
  }

  toggle() {
    if (!this.events.length) return;
    if (this.playing()) { this.pause(); return; }
    if (this.pos() >= this.total()) this.seek(0);
    this.playing.set(true);
    this.last = performance.now();
    this.timer = setInterval(() => this.tick(), 33);
    this.stage().nativeElement.focus();
  }

  private pause() { this.playing.set(false); clearInterval(this.timer); }

  private tick() {
    const now = performance.now();
    const p = Math.min(this.total(), this.pos() + ((now - this.last) / 1000) * this.speed());
    this.last = now;
    this.runTo(p);
    this.pos.set(p);
    if (p >= this.total()) this.pause();
  }

  /** Applies the events up to playback time p (output batched into one write). */
  private runTo(p: number) {
    let out = '';
    while (this.idx < this.events.length && this.events[this.idx].v <= p) {
      const e = this.events[this.idx++];
      if (e.code === 'o') { out += e.data; continue; }
      if (out) { this.term?.write(out); out = ''; }
      this.apply(e);
    }
    if (out) this.term?.write(out);
  }

  seek(p: number) {
    p = Math.max(0, Math.min(p, this.total()));
    if (p < this.pos() || this.idx === 0) {
      const h = this.rec?.header;
      this.term?.reset();
      if (h) this.term?.resize(Math.max(h.width, 10), Math.max(h.height, 4));
      this.idx = 0;
    }
    const keepFlash = this.flash();
    this.runTo(p);
    this.flash.set(keepFlash);
    this.pos.set(p);
    this.last = performance.now();
  }

  jumpMarker(i: number) {
    const m = this.markers()[i];
    if (m) { this.seek(m.v); this.flash.set(m.data || 'Marker'); }
  }

  /** Next output (after the current point, then from the start) containing the text. */
  find() {
    const q = this.query.trim().toLowerCase();
    if (!q) return;
    const strip = (s: string) => s.replace(/\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][0-9A-Za-z]|\x1b[@-Z\\-_]/g, '').toLowerCase();
    const from = this.idx;
    const order = [...this.events.keys()].slice(from).concat([...this.events.keys()].slice(0, from));
    // also match text split over two output events
    for (const i of order) {
      const e = this.events[i];
      if (e.code !== 'o') continue;
      const prev = this.events.slice(Math.max(0, i - 3), i).filter(x => x.code === 'o').map(x => x.data).join('');
      if (strip(e.data).includes(q) || (strip(prev + e.data).includes(q) && !strip(prev).includes(q))) {
        this.pause();
        this.seek(e.v);
        setTimeout(() => this.search?.findPrevious(this.query.trim(), { caseSensitive: false, decorations: { matchOverviewRuler: '#f59e0b', activeMatchColorOverviewRuler: '#f59e0b', activeMatchBackground: '#f59e0b' } }), 60);
        return;
      }
    }
    this.ui.toast(`"${this.query.trim()}" is not in this recording.`, 'error');
  }

  key(ev: KeyboardEvent) {
    if (ev.key === ' ') { ev.preventDefault(); this.toggle(); }
    else if (ev.key === 'ArrowRight') { ev.preventDefault(); this.seek(this.pos() + 5); }
    else if (ev.key === 'ArrowLeft') { ev.preventDefault(); this.seek(this.pos() - 5); }
    else if (ev.key === 'Home') { ev.preventDefault(); this.seek(0); }
    else if (ev.key === 'End') { ev.preventDefault(); this.seek(this.total()); }
  }

  /** Everything the device showed, as plain text (played into a hidden terminal so redraws come out right). */
  async exportText() {
    const h = this.rec?.header;
    if (!h) return;
    const t = new Terminal({ cols: Math.max(h.width, 10), rows: Math.max(h.height, 4), scrollback: 1_000_000, allowProposedApi: true });
    try {
      for (const e of this.events) {
        if (e.code === 'o') await new Promise<void>(r => t.write(e.data, r));
        else if (e.code === 'r') { const m = /^(\d+)x(\d+)$/.exec(e.data); if (m) t.resize(+m[1], +m[2]); }
        else if (e.code === 'm') await new Promise<void>(r => t.write(`\r\n--- ${e.data || 'Marker'} ---\r\n`, r));
      }
      const b = t.buffer.active, lines: string[] = [];
      for (let i = 0; i < b.length; i++) {
        const l = b.getLine(i);
        if (!l) continue;
        const s = l.translateToString(true);
        if (l.isWrapped && lines.length) lines[lines.length - 1] += s; else lines.push(s);
      }
      while (lines.length && !lines[lines.length - 1].trim()) lines.pop();
      const name = (this.tab().playFile ?? 'recording').split(/[\\/]/).pop()!.replace(/\.cast$/i, '') + '.txt';
      const p = await this.backend.saveTextAs('Export Text', name, lines.join('\n') + '\n');
      if (p) this.ui.toast('Saved ' + p, 'success');
    } catch (e) { this.ui.error(e); } finally { t.dispose(); }
  }

  async reveal() { await this.backend.showInFolder(this.tab().playFile!).catch(e => this.ui.error(e)); }

  fmt(s: number) {
    const t = Math.floor(s), h = Math.floor(t / 3600), m = Math.floor((t % 3600) / 60), x = t % 60;
    return (h ? h + ':' + String(m).padStart(2, '0') : String(m)) + ':' + String(x).padStart(2, '0');
  }
}
