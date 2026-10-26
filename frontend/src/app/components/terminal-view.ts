import {
  AfterViewInit, Component, ElementRef, OnDestroy, computed, effect, inject, input, signal, viewChild,
} from '@angular/core';
import { Terminal } from '@xterm/xterm';
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
  template: `<div class="term-host" #host (contextmenu)="onContextMenu($event)"></div>
    @if (fix(); as f) {
      <div class="fixbar" role="alert">
        <button type="button" class="fx" title="Dismiss" (click)="dismissed.set(tab().message)">✕</button>
        <div class="fmsg">{{ tab().message }}</div>
        @if (f.hint) { <div class="fhint">{{ f.hint }}</div> }
        <div class="facts">@for (a of f.actions; track a) { <button type="button" [class.primary]="$first" (click)="runFix(a)">{{ labels[a] }}</button> }</div>
      </div>
    }`,
  styles: [`
    :host { display: block; position: absolute; inset: 0; padding: 4px 0 0 6px; background: var(--term-bg); }
    .term-host { width: 100%; height: 100%; }
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
  private tabs = inject(Tabs);
  private ui = inject(Ui);
  private term?: Terminal;
  private fit?: FitAddon;
  private ro?: ResizeObserver;
  private unsub: (() => void)[] = [];
  private fitTimer?: ReturnType<typeof setTimeout>;
  private history = inject(History);

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
    this.tabs.handles.set(tab.id, { term, fit, search });

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
      if (!this.tabs.get(id)?.timestamps || term.buffer.active.type !== 'normal') { term.write(bytes); return; }
      // Per-line timestamps (display only): after every newline in line-based output.
      const stamp = () => '\x1b[90m[' + new Date().toTimeString().slice(0, 8) + ']\x1b[0m ';
      let text = dec.decode(bytes, { stream: true });
      if (atLineStart && text.length) text = stamp() + text;
      atLineStart = text.endsWith('\n');
      term.write(text.replace(/\n(?=.)/gs, () => '\n' + stamp()));
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

    term.onWriteParsed(() => this.highlight());
    this.compileKeywords();

    this.ro = new ResizeObserver(() => this.scheduleFit());
    this.ro.observe(this.host().nativeElement);
    this.doFit();
    this.tabs.patch(id, { cols: term.cols, rows: term.rows });
    this.tabs.start(id);
    if (this.visible()) term.focus();
  }

  private keywords: { re: RegExp; color: string; fg: string }[] = [];
  private scannedTo = 0;

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
  }

  /** Highlights keywords in newly completed lines using xterm decorations. */
  private highlight() {
    const term = this.term;
    if (!term || !this.keywords.length) return;
    const buf = term.buffer.active;
    if (buf.type === 'alternate') return; // full-screen apps (vi, less)
    const cursorAbs = buf.baseY + buf.cursorY;
    if (this.scannedTo > cursorAbs) this.scannedTo = Math.max(0, cursorAbs - term.rows);
    const start = Math.max(this.scannedTo, cursorAbs - 500);
    for (let y = start; y < cursorAbs; y++) {
      const line = buf.getLine(y);
      if (!line) continue;
      const text = line.translateToString(true);
      if (!text) continue;
      for (const k of this.keywords) {
        k.re.lastIndex = 0;
        let m: RegExpExecArray | null;
        let guard = 0;
        while ((m = k.re.exec(text)) && guard++ < 50) {
          if (!m[0].length) { k.re.lastIndex++; continue; }
          const marker = term.registerMarker(y - cursorAbs);
          if (!marker) continue;
          term.registerDecoration({ marker, x: m.index, width: m[0].length, backgroundColor: k.color, foregroundColor: k.fg, layer: 'top' });
        }
      }
    }
    this.scannedTo = cursorAbs;
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
      { label: this.tabs.get(id)?.timestamps ? 'Hide Timestamps' : 'Show Timestamps', action: () => this.tabs.patch(id, { timestamps: !this.tabs.get(id)?.timestamps }) },
      { separator: true },
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
    this.unsub.forEach(u => u());
    this.ro?.disconnect();
    this.tabs.handles.delete(this.tab().id);
    this.term?.dispose();
  }
}
