import {
  AfterViewInit, Component, ElementRef, OnDestroy, computed, effect, inject, input, signal, viewChild,
} from '@angular/core';
import { IDecoration, IMarker, Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { Unicode11Addon } from '@xterm/addon-unicode11';
import { SerializeAddon } from '@xterm/addon-serialize';
import { Backend } from '../core/backend';
import { Tabs, TabView } from '../core/tabs.service';
import { Ui } from '../core/ui.service';
import { terminalTheme } from '../core/themes';
import { comboOf } from '../core/models';
import { FIX_LABELS, FixAction, fixFor } from '../core/fixes';
import { History, commandOfLine } from '../core/history.service';
import { FormsModule } from '@angular/forms';

/** Black or white, whichever is more readable on the given background. */
function contrastText(hex: string): string {
  const n = parseInt(hex.slice(1), 16);
  const [r, g, b] = [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  return 0.299 * r + 0.587 * g + 0.114 * b > 150 ? '#000000' : '#ffffff';
}

function b64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/** One xterm.js terminal bound to a backend tab. */
@Component({
  selector: 'app-terminal-view',
  imports: [FormsModule],
  template: `<div class="term-host" #host (contextmenu)="onContextMenu($event)"></div>
    @if (tab().autoScroll === false) {
      <div class="paused" title="New output does not move the view">Auto scroll is off <button type="button" (click)="setAutoScroll(true)">Turn On</button></div>
    }
    @if (tab().sendBar) {
      <div class="sendbar" [class.compact]="!serial()">
        @if (serial()) {
          <span class="port" [class.on]="tab().state === 'connected'" [title]="tab().info || 'Serial port and speed'"><span class="dot"></span>{{ portLabel() }}</span>
          <input class="msg" #msg [ngModel]="sendText()" (ngModelChange)="sendText.set($event)" (keydown)="sendKey($event)"
            [placeholder]="tab().state === 'connected' ? 'Message to send - Enter sends, ↑/↓ earlier messages' : 'Not connected'" />
          <select [ngModel]="eol()" (ngModelChange)="setEol($event)" title="Added after each message">
            @for (e of eols; track e.l) { <option [ngValue]="e.v">{{ e.l }}</option> }
          </select>
          <button type="button" class="primary" (click)="sendLine()" [disabled]="tab().state !== 'connected'">Send</button>
          <span class="sep"></span>
        } @else {
          <span class="spacer"></span>
        }
        <label title="Local time at the start of each line (display only)"><input type="checkbox" [ngModel]="!!tab().timestamps" (ngModelChange)="setTimestamps($event)" /> Timestamps</label>
        <label title="Follow new output"><input type="checkbox" [ngModel]="tab().autoScroll !== false" (ngModelChange)="setAutoScroll($event)" /> Auto scroll</label>
        <button type="button" (click)="clearAll()" title="Clear the screen and the scrollback">Clear</button>
        <button type="button" class="x" [title]="serial() ? 'Hide the send bar (right-click the terminal → Send Bar shows it again)' : 'Hide this bar (right-click the terminal → Terminal Bar shows it again; Global Options → Terminal hides it for every tab)'" (click)="tabs.patch(tab().id, { sendBar: false })">✕</button>
      </div>
    }`,
  styles: [`
    :host { display: flex; flex-direction: column; position: absolute; inset: 0; padding: 4px 0 0 6px; background: var(--term-bg); }
    .term-host { width: 100%; flex: 1; min-height: 0; }
    .paused { position: absolute; right: 18px; bottom: 44px; z-index: 4; display: flex; gap: 8px; align-items: center; padding: 4px 6px 4px 10px; border-radius: 6px;
      background: var(--panel); color: var(--text); border: 1px solid var(--border); box-shadow: var(--shadow); font-size: 12px;
      button { height: 22px; font-size: 11.5px; padding: 0 8px; } }
    .sendbar { flex: none; display: flex; flex-wrap: wrap; row-gap: 4px; gap: 8px; align-items: center; margin: 4px 0 0 -6px; padding: 5px 8px; background: var(--panel); border-top: 1px solid var(--border); font-size: 12px; color: var(--muted);
      .msg { flex: 1 1 160px; min-width: 160px; font-family: var(--mono); }
      select { flex: none; width: 104px; }
      label { display: flex; gap: 4px; align-items: center; white-space: nowrap; cursor: pointer; }
      .sep { width: 1px; align-self: stretch; background: var(--border); }
      button { height: 24px; } .x { border: 0; background: none; color: var(--muted); padding: 0 4px; }
      .spacer { flex: 1; }
      .port { display: flex; align-items: center; gap: 6px; flex: 0 1 auto; min-width: 0; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
        font-family: var(--mono); font-size: 11.5px; color: var(--text); padding: 2px 8px; border: 1px solid var(--border); border-radius: 12px;
        .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--muted); flex: none; } &.on .dot { background: var(--ok, #22c55e); } } }
    .sendbar.compact { padding: 2px 8px; button { height: 20px; font-size: 11.5px; padding: 0 8px; } }
    .fixbar { position: absolute; top: 10px; right: 14px; z-index: 5; max-width: min(520px, 80%); padding: 10px 12px; border-radius: 8px;
      background: var(--panel); color: var(--text); border: 1px solid var(--border); border-left: 4px solid var(--danger); box-shadow: var(--shadow); font-size: 12.5px; }
    .fmsg { font-weight: 600; margin-right: 18px; word-break: break-word; }
    .fhint { color: var(--muted); margin-top: 4px; }
    .facts { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; button { height: 24px; font-size: 12px; } }
    .fx { position: absolute; top: 6px; right: 6px; border: 0; background: none; height: auto; padding: 0 2px; color: var(--muted); cursor: pointer; }
  `],
})
export class TerminalView implements AfterViewInit, OnDestroy {
  readonly tab = input.required<TabView>();
  readonly visible = input<boolean>(false);
  private host = viewChild.required<ElementRef<HTMLDivElement>>('host');
  private backend = inject(Backend);
  protected tabs = inject(Tabs);
  private ui = inject(Ui);
  private term?: Terminal;
  private fit?: FitAddon;
  private ro?: ResizeObserver;
  private unsub: (() => void)[] = [];
  private fitTimer?: ReturnType<typeof setTimeout>;
  private history = inject(History);

  // ---- send bar (serial tabs; any tab from the right-click menu) ----
  readonly serial = computed(() => this.tab().session.protocol === 'Serial');
  /** "/dev/cu.usbserial-A10K · 115200 8N1": what the connection reports, else the session's settings. */
  readonly portLabel = computed(() => {
    const t = this.tab();
    const m = /^Serial (\S+) (\d+) (\d[NOEMS][\d.]+)(.*)$/.exec(t.info || '');
    if (m) return `${m[1]} · ${m[2]} ${m[3]}${m[4]}`;
    const s = t.session;
    return `${s.comPort || 'no port'} · ${s.serialAutoBaud ? 'speed detected on connect' : (s.baudRate || 9600)}`;
  });
  readonly eols = [{ v: '\r', l: 'CR' }, { v: '\n', l: 'LF' }, { v: '\r\n', l: 'CR+LF' }, { v: '', l: 'No line ending' }];
  readonly sendText = signal('');
  readonly eol = signal<string>((() => { try { const v = JSON.parse(localStorage.getItem('termwright.sendEol') ?? '"\\r"'); return typeof v === 'string' ? v : '\r'; } catch { return '\r'; } })());
  private sent: string[] = [];
  private sentIdx = -1;

  setEol(v: string) {
    this.eol.set(v);
    try { localStorage.setItem('termwright.sendEol', JSON.stringify(v)); } catch { /* storage unavailable */ }
  }

  sendLine() {
    const t = this.tab();
    if (t.state !== 'connected' || t.locked) return;
    const text = this.sendText();
    void this.backend.sendInput(t.id, text + this.eol());
    if (text.trim()) {
      this.sent = [text, ...this.sent.filter(x => x !== text)].slice(0, 100);
      this.history.add(text, { session: t.session.path, title: t.title, deviceType: t.session.deviceType });
    }
    this.sentIdx = -1;
    this.sendText.set('');
  }

  sendKey(ev: KeyboardEvent) {
    if (ev.key === 'Enter') { ev.preventDefault(); this.sendLine(); return; }
    if ((ev.key === 'ArrowUp' || ev.key === 'ArrowDown') && this.sent.length) {
      ev.preventDefault();
      this.sentIdx = ev.key === 'ArrowUp' ? Math.min(this.sentIdx + 1, this.sent.length - 1) : Math.max(this.sentIdx - 1, -1);
      this.sendText.set(this.sentIdx < 0 ? '' : this.sent[this.sentIdx]);
    }
  }

  setTimestamps(on: boolean) { this.tabs.patch(this.tab().id, { timestamps: on }); }

  setAutoScroll(on: boolean) {
    this.tabs.patch(this.tab().id, { autoScroll: on });
    if (on) this.term?.scrollToBottom();
  }

  /** Clears the screen and the scrollback (the line with the cursor stays). */
  clearAll() { this.term?.clear(); this.clearHighlights(); this.term?.focus(); }

  /** Remembers the command on the cursor line (prompt removed) for history search. */
  private remember(term: Terminal, t: TabView) {
    const b = term.buffer.active;
    let row = b.baseY + b.cursorY;
    let text = b.getLine(row)?.translateToString(true) ?? '';
    while (row > 0 && b.getLine(row)?.isWrapped) { row--; text = (b.getLine(row)?.translateToString(true) ?? '') + text; }
    const cmd = commandOfLine(text);
    if (cmd) this.history.add(cmd, { session: t.session.path, title: t.title, deviceType: t.session.deviceType });
  }
  readonly labels = FIX_LABELS;
  /** The error message the user closed the fix bar for. */
  readonly dismissed = signal('');
  readonly fix = computed(() => {
    const t = this.tab();
    if (t.state !== 'error' || !t.message || this.dismissed() === t.message) return null;
    return fixFor(t.message, t.session);
  });

  async runFix(a: FixAction) {
    const t = this.tab();
    const s = t.session;
    switch (a) {
      case 'reconnect': await this.tabs.reconnect(t.id); break;
      case 'edit': if (s.path) await this.ui.editSession(s.path, t.id); break;
      case 'edit-jump': if (s.jumpSession) await this.ui.editSession(s.jumpSession); break;
      case 'device-types': this.ui.openOptions('devices'); break;
      case 'forget-password':
        await this.backend.forgetPassword(s.path).catch(e => this.ui.error(e));
        this.ui.toast('Saved password removed - you will be asked for it', 'info');
        await this.tabs.reconnect(t.id);
        break;
      case 'copy': await this.backend.clipboardSet(t.message); break;
      case 'find-mac': this.ui.movedInput.set({ find: s.path, tab: t.id }); this.ui.open('moved'); break;
    }
  }

  constructor() {
    // Apply settings changes (font, scheme, cursor) live.
    effect(() => {
      const s = this.ui.settings();
      const t = this.term;
      if (!s || !t) return;
      const sess = this.tab().session;
      t.options.fontFamily = sess.fontFamily || s.fontFamily;
      t.options.fontSize = sess.fontSize > 0 ? sess.fontSize + (s.fontSize - 14) : s.fontSize;
      this.compileKeywords();
      t.options.cursorStyle = s.cursorStyle;
      t.options.cursorBlink = s.cursorBlink;
      t.options.theme = terminalTheme(this.tab().session.colorScheme, s, this.ui.lightUi());
      this.host().nativeElement.parentElement?.style.setProperty('--term-bg', String(t.options.theme.background));
      this.scheduleFit();
    });
    effect(() => {
      if (this.visible()) this.scheduleFit();
    });
    effect(() => {
      if (this.visible() && this.tabs.activeId() === this.tab().id) setTimeout(() => this.term?.focus(), 0);
    });
  }

  ngAfterViewInit() {
    const s = this.ui.settings();
    const tab = this.tab();
    const theme = terminalTheme(tab.session.colorScheme, s, this.ui.lightUi());
    const term = new Terminal({
      fontFamily: tab.session.fontFamily || s?.fontFamily || 'monospace',
      fontSize: tab.session.fontSize || s?.fontSize || 14,
      cursorStyle: s?.cursorStyle ?? 'block',
      cursorBlink: s?.cursorBlink ?? true,
      scrollback: tab.session.scrollback || s?.scrollback || 10000,
      theme,
      allowProposedApi: true,
      macOptionIsMeta: true,
      rightClickSelectsWord: false,
      convertEol: false,
      // OSC 8 hyperlinks printed by devices and tools
      linkHandler: { activate: (_e, uri) => this.backend.openURL(uri), allowNonHttpProtocols: false },
    });
    const fit = new FitAddon();
    const search = new SearchAddon();
    term.loadAddon(fit);
    term.loadAddon(search);
    term.loadAddon(new WebLinksAddon((_e, uri) => this.backend.openURL(uri)));
    const uni = new Unicode11Addon();
    term.loadAddon(uni);
    this.serialize = new SerializeAddon();
    term.loadAddon(this.serialize);
    term.unicode.activeVersion = '11';
    term.open(this.host().nativeElement);
    (this.host().nativeElement.parentElement as HTMLElement).style.setProperty('--term-bg', String(theme.background));
    this.term = term;
    this.fit = fit;
    this.tabs.handles.set(tab.id, { term, fit, search, serialize: this.serialize, keywords: () => this.keywords });

    const id = tab.id;
    // Working directory reported by the shell: OSC 7 (file://host/path),
    // iTerm2's OSC 1337 CurrentDir=, VS Code's OSC 633 P;Cwd=.
    const setCwd = (dir: string) => {
      dir = dir.trim();
      if (dir.startsWith('/') && this.tabs.get(id)?.cwd !== dir) this.tabs.patch(id, { cwd: dir });
      return true;
    };
    term.parser.registerOscHandler(7, d => {
      const m = /^file:\/\/[^/]*(\/.*)$/.exec(d.trim());
      if (m) { try { setCwd(decodeURIComponent(m[1])); } catch { setCwd(m[1]); } }
      return true;
    });
    term.parser.registerOscHandler(1337, d => {
      if (d.startsWith('CurrentDir=')) return setCwd(d.slice(11));
      if (d.startsWith('RemoteHost=')) {
        const user = d.slice(11).split('@')[0].trim();
        if (user && this.tabs.get(id)?.shellUser !== user) this.tabs.patch(id, { shellUser: user });
        return true;
      }
      return false;
    });
    term.parser.registerOscHandler(633, d => d.startsWith('P;Cwd=') ? setCwd(d.slice(6)) : false);
    const dec = new TextDecoder();
    const quietDec = new TextDecoder();
    let atLineStart = false;
    this.unsub.push(this.backend.on('term:data:' + id, (b64: string) => {
      const bytes = b64ToBytes(b64);
      const q = this.tabs.quiet.get(id);
      if (q) {
        q.held += quietDec.decode(bytes, { stream: true });
        const i = q.held.indexOf('\x1b]7;');
        if (i >= 0) { this.tabs.quiet.delete(id); term.write('\r\x1b[2K' + q.held.slice(i)); }
        return;
      }
      const tv = this.tabs.get(id);
      // Auto scroll off: the view stays where it is while output arrives.
      const write = (d: string | Uint8Array) => {
        if (tv?.autoScroll !== false || term.buffer.active.type !== 'normal') { term.write(d); return; }
        const y = term.buffer.active.viewportY;
        term.write(d, () => { if (term.buffer.active.viewportY !== y) term.scrollToLine(y); });
      };
      if (!tv?.timestamps || term.buffer.active.type !== 'normal') { write(bytes); return; }
      // Per-line timestamps (display only): local time after every newline in line-based output.
      const stamp = () => {
        const d = new Date();
        return '\x1b[90m[' + d.toTimeString().slice(0, 8) + '.' + String(d.getMilliseconds()).padStart(3, '0') + ']\x1b[0m ';
      };
      let text = dec.decode(bytes, { stream: true });
      if (atLineStart && text.length) text = stamp() + text;
      atLineStart = text.endsWith('\n');
      write(text.replace(/\n(?=.)/gs, () => '\n' + stamp()));
    }));
    term.onData(d => {
      const t = this.tabs.get(id);
      if (t?.locked) return;
      if (t && (t.state === 'disconnected' || t.state === 'error')) {
        if (d === '\r') void this.tabs.reconnect(id);
        return;
      }
      if (d === '\r' && t?.state === 'connected' && term.buffer.active.type === 'normal') this.remember(term, t);
      void this.backend.sendInput(id, d);
    });
    term.onBinary(d => void this.backend.sendBinary(id, btoa(d)));
    // Route paste through the tab service (multi-line confirmation, line delay).
    this.host().nativeElement.addEventListener('paste', (ev: ClipboardEvent) => {
      ev.preventDefault();
      ev.stopImmediatePropagation();
      void this.tabs.paste(id, ev.clipboardData?.getData('text/plain') ?? '');
    }, true);
    term.onResize(({ cols, rows }) => {
      this.tabs.patch(id, { cols, rows });
      void this.backend.resizeTab(id, cols, rows);
    });
    term.onSelectionChange(() => {
      if (this.ui.settings()?.copyOnSelect && term.hasSelection()) void this.backend.clipboardSet(term.getSelection());
    });
    term.attachCustomKeyEventHandler(ev => {
      if (ev.type !== 'keydown') return true;
      const km = this.ui.settings()?.keyMap;
      if (km?.length) {
        const combo = comboOf(ev);
        const b = combo ? km.find(k => k.keys === combo) : undefined;
        if (b) {
          ev.preventDefault();
          if (b.send) void this.tabs.send(b.send);
          if (b.action) this.ui.actionHandler(b.action);
          if (b.script) void this.tabs.runScriptPath(b.script, id);
          return false;
        }
      }
      if (ev.ctrlKey && !ev.shiftKey && ev.key === 'Insert') { void this.tabs.copy(); return false; }
      if (ev.shiftKey && !ev.ctrlKey && ev.key === 'Insert') { void this.tabs.paste(id); return false; }
      const t = this.tabs.get(id);
      if (t?.session.backspace === 'bs' && ev.key === 'Backspace' && !ev.ctrlKey && !ev.altKey && !ev.metaKey && !t.locked) {
        if (t.state === 'connected') void this.backend.sendInput(id, '\b');
        return false;
      }
      return true;
    });

    // keyword highlighting of the rows in view, at most once per frame
    term.onWriteParsed(() => this.scheduleHighlight());
    term.onRender(() => this.scheduleHighlight());
    this.compileKeywords();

    this.ro = new ResizeObserver(() => this.scheduleFit());
    this.ro.observe(this.host().nativeElement);
    this.doFit();
    this.tabs.patch(id, { cols: term.cols, rows: term.rows });
    this.tabs.start(id);
    if (this.visible()) term.focus();
  }

  private keywords: { re: RegExp; color: string; fg: string }[] = [];
  /** Highlighted rows: one marker per row, its text when highlighted, its decorations. */
  private hl: { marker: IMarker; text: string; decos: IDecoration[] }[] = [];
  private hlFrame = 0;

  /** Builds the regexes for the session's keyword set (or the default set). */
  private compileKeywords() {
    const s = this.ui.settings();
    const want = this.tab().session.keywordSet || s?.defaultKeywordSet || '';
    const set = want === 'None' ? undefined : (s?.keywordSets ?? []).find(k => k.name === want);
    this.keywords = [];
    for (const k of set?.keywords ?? []) {
      if (!k.text) continue;
      try {
        const src = k.regex ? k.text : k.text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        const color = /^#[0-9a-f]{6}$/i.test(k.color) ? k.color : '#6b5310';
        this.keywords.push({ re: new RegExp(src, k.caseSensitive ? 'g' : 'gi'), color, fg: contrastText(color) });
      } catch { /* invalid regex: skip */ }
    }
    this.clearHighlights();
    this.scheduleHighlight();
  }

  private scheduleHighlight() {
    if (!this.hlFrame) this.hlFrame = requestAnimationFrame(() => { this.hlFrame = 0; this.highlight(); });
  }

  private clearHighlights() {
    for (const e of this.hl) { e.decos.forEach(d => d.dispose()); e.marker.dispose(); }
    this.hl = [];
  }

  /** Highlights keywords in the completed rows in view (and a few around
   *  them). Only those rows carry decorations: every decoration is redrawn on
   *  each frame, so highlighting the whole scrollback made typing slower the
   *  longer a session ran. Rows whose text did not change keep theirs. */
  private highlight() {
    const term = this.term;
    if (!term) return;
    const buf = term.buffer.active;
    if (!this.keywords.length || buf.type === 'alternate') { this.clearHighlights(); return; } // full-screen apps (vi, less)
    const cursorAbs = buf.baseY + buf.cursorY;
    const from = Math.max(0, buf.viewportY - 10), to = Math.min(cursorAbs, buf.viewportY + term.rows + 10); // the cursor row is still being typed
    const kept = new Map<number, (typeof this.hl)[number]>();
    for (const e of this.hl) {
      const y = e.marker.line;
      if (e.marker.isDisposed || y < from || y >= to || kept.has(y)) { e.decos.forEach(d => d.dispose()); e.marker.dispose(); continue; }
      kept.set(y, e);
    }
    for (let y = from; y < to; y++) {
      const text = buf.getLine(y)?.translateToString(true) ?? '';
      const old = kept.get(y);
      if (old && old.text === text) continue;
      if (old) { old.decos.forEach(d => d.dispose()); old.marker.dispose(); kept.delete(y); }
      if (!text) continue;
      const hits: { x: number; w: number; color: string; fg: string }[] = [];
      for (const k of this.keywords) {
        k.re.lastIndex = 0;
        let m: RegExpExecArray | null;
        let guard = 0;
        while ((m = k.re.exec(text)) && guard++ < 50) {
          if (!m[0].length) { k.re.lastIndex++; continue; }
          hits.push({ x: m.index, w: m[0].length, color: k.color, fg: k.fg });
        }
      }
      if (!hits.length) continue;
      const marker = term.registerMarker(y - cursorAbs);
      if (!marker) continue;
      const decos: IDecoration[] = [];
      for (const h of hits) {
        const d = term.registerDecoration({ marker, x: h.x, width: h.w, backgroundColor: h.color, foregroundColor: h.fg, layer: 'top' });
        if (d) decos.push(d);
      }
      kept.set(y, { marker, text, decos });
    }
    this.hl = [...kept.values()];
  }

  private scheduleFit() {
    clearTimeout(this.fitTimer);
    this.fitTimer = setTimeout(() => this.doFit(), 30);
  }

  private doFit() {
    const el = this.host()?.nativeElement;
    if (!this.fit || !el || el.offsetWidth === 0 || el.offsetHeight === 0) return;
    try { this.fit.fit(); } catch { /* not yet rendered */ }
  }

  onContextMenu(ev: MouseEvent) {
    const id = this.tab().id;
    if (this.ui.settings()?.rightClickPaste && !ev.shiftKey) {
      ev.preventDefault();
      void this.tabs.paste(id);
      return;
    }
    const t = this.term;
    this.ui.showMenu(ev, [
      { label: 'Copy', action: () => void this.tabs.copy(), disabled: !t?.hasSelection() },
      { label: 'Paste', action: () => void this.tabs.paste(id) },
      { label: 'Select All', action: () => t?.selectAll() },
      { label: 'Copy as HTML (with colors)', action: () => void this.copyHtml(), disabled: !t?.hasSelection() },
      { label: 'Save Scrollback As HTML...', action: () => void this.tabs.saveScrollback(id, 'html') },
      { separator: true },
      { label: (this.tabs.get(id)?.timestamps ? '✓ ' : '') + 'Timestamps', action: () => this.setTimestamps(!this.tabs.get(id)?.timestamps) },
      { label: (this.tabs.get(id)?.autoScroll !== false ? '✓ ' : '') + 'Auto Scroll', action: () => this.setAutoScroll(this.tabs.get(id)?.autoScroll === false) },
      { label: (this.tabs.get(id)?.sendBar ? '✓ ' : '') + (this.serial() ? 'Send Bar' : 'Terminal Bar'), action: () => this.tabs.patch(id, { sendBar: !this.tabs.get(id)?.sendBar }) },
      { separator: true },
      { label: 'Clear Screen', action: () => this.clearAll() },
      { label: 'Clear Scrollback', action: () => t?.clear() },
      { label: 'Reset Terminal', action: () => t?.reset() },
    ]);
  }

  private serialize?: SerializeAddon;

  /** Copies the selection with colors as HTML (falls back to saving an .html file). */
  async copyHtml() {
    const html = this.serialize?.serializeAsHTML({ onlySelection: true, includeGlobalBackground: true });
    if (!html) return;
    try {
      await navigator.clipboard.write([new ClipboardItem({
        'text/html': new Blob([html], { type: 'text/html' }),
        'text/plain': new Blob([this.term?.getSelection() ?? ''], { type: 'text/plain' }),
      })]);
      this.ui.toast('Copied with colors');
    } catch {
      const p = await this.backend.chooseSaveFile('Save Selection as HTML', (this.tab().title || 'output') + '.html').catch(() => '');
      if (p) { await this.backend.saveTextFile(p, html); this.ui.toast('Saved ' + p, 'success'); }
    }
  }

  ngOnDestroy() {
    cancelAnimationFrame(this.hlFrame);
    this.unsub.forEach(u => u());
    this.ro?.disconnect();
    this.tabs.handles.delete(this.tab().id);
    this.term?.dispose();
  }
}
