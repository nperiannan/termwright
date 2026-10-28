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
import { DeviceIcon, deviceKindOf } from './device-icon';
import { History, commandOfLine } from '../core/history.service';
import { FormsModule } from '@angular/forms';

/** Black or white, whichever is more readable on the given background. */
/** The keyword colour as text on the terminal: the same hue, light enough
 *  on a dark terminal and dark enough on a light one. */
function textTone(hex: string, lightTerm: boolean): string {
  const n = parseInt(hex.slice(1), 16);
  let [r, g, b] = [((n >> 16) & 255) / 255, ((n >> 8) & 255) / 255, (n & 255) / 255];
  const mx = Math.max(r, g, b), mn = Math.min(r, g, b), d = mx - mn;
  let h = 0;
  if (d) h = mx === r ? ((g - b) / d) % 6 : mx === g ? (b - r) / d + 2 : (r - g) / d + 4;
  h = (h * 60 + 360) % 360;
  const sat = d ? Math.max(0.6, d / (1 - Math.abs(mx + mn - 1))) : 0;
  const l = lightTerm ? 0.38 : 0.64;
  const c = (1 - Math.abs(2 * l - 1)) * Math.min(sat, 0.85), x = c * (1 - Math.abs(((h / 60) % 2) - 1)), m = l - c / 2;
  [r, g, b] = h < 60 ? [c, x, 0] : h < 120 ? [x, c, 0] : h < 180 ? [0, c, x] : h < 240 ? [0, x, c] : h < 300 ? [x, 0, c] : [c, 0, x];
  const to = (v: number) => Math.round((v + m) * 255).toString(16).padStart(2, '0');
  return '#' + to(r) + to(g) + to(b);
}

function contrastText(hex: string): string {
  const n = parseInt(hex.slice(1), 16);
  const [r, g, b] = [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  return 0.299 * r + 0.587 * g + 0.114 * b > 150 ? '#000000' : '#ffffff';
}

/** Bytes of a hex message: "48 65 6C", "0x48,0x65", "48:65" or "4865"; an error text when it is not hex. */
export function parseHex(s: string): number[] | string {
  const t = s.trim();
  if (!t) return [];
  const parts = /[\s,;:]/.test(t) ? t.split(/[\s,;:]+/).filter(Boolean) : (t.replace(/^0x/i, '').match(/.{1,2}/g) ?? []);
  const out: number[] = [];
  for (let p of parts) {
    p = p.replace(/^0x/i, '');
    if (!/^[0-9a-f]{1,2}$/i.test(p)) return `"${p}" is not a hex byte - use pairs of 0-9 / A-F, e.g. 48 65 0D 0A`;
    out.push(parseInt(p, 16));
  }
  if (!/[\s,;:]/.test(t) && t.replace(/^0x/i, '').length % 2) return 'An odd number of hex digits - each byte has two (e.g. 0D, not D)';
  return out;
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
  imports: [FormsModule, DeviceIcon],
  host: { '[class.modern]': "ui.settings()?.layout !== 'classic'" },
  template: `<div class="term-host" #host (contextmenu)="onContextMenu($event)"></div>
    @if (tab().autoScroll === false) {
      <div class="paused" title="New output does not move the view">Auto scroll is off <button type="button" (click)="setAutoScroll(true)">Turn On</button></div>
    }
    @if (tab().sendBar) {
      <div class="sendbar" [class.compact]="!serial()">
        @if (serial()) {
          <span class="port" [class.on]="tab().state === 'connected'" [title]="tab().info || 'Serial port and speed'"><span class="dot"></span>{{ portLabel() }}</span>
          <select class="mode" [ngModel]="sendMode()" (ngModelChange)="setSendMode($event)" title="Send the message as text, or as hex bytes (48 65 6C 6C 6F, 0x0D 0x0A or 48656C6C6F)">
            <option value="text">Text</option><option value="hex">Hex</option>
          </select>
          <input class="msg" #msg [class.bad]="hexError()" [ngModel]="sendText()" (ngModelChange)="sendText.set($event); hexError.set('')" (keydown)="sendKey($event)"
            [title]="hexError()"
            [placeholder]="tab().state !== 'connected' ? 'Not connected' : sendMode() === 'hex' ? 'Hex bytes, e.g. 48 65 6C 6C 6F 0D 0A - Enter sends' : 'Message to send - Enter sends, ↑/↓ earlier messages'" />
          <select [ngModel]="eol()" (ngModelChange)="setEol($event)" [disabled]="sendMode() === 'hex'" [title]="sendMode() === 'hex' ? 'Hex messages are sent exactly as typed (add 0D 0A yourself)' : 'Added after each message'">
            @for (e of eols; track e.l) { <option [ngValue]="e.v">{{ e.l }}</option> }
          </select>
          <button type="button" class="primary" (click)="sendLine()" [disabled]="tab().state !== 'connected'">Send</button>
          <span class="sep"></span>
          <select class="mode" [ngModel]="tab().hexView ? 'hex' : 'text'" (ngModelChange)="setHexView($event === 'hex')" title="Show what the device sends as text or as hex bytes">
            <option value="text">View: Text</option><option value="hex">View: Hex</option>
          </select>
        } @else {
          <span class="spacer"></span>
        }
        <label title="Local time at the start of each line (display only)"><input type="checkbox" [ngModel]="!!tab().timestamps" (ngModelChange)="setTimestamps($event)" /> Timestamps</label>
        <label title="Follow new output"><input type="checkbox" [ngModel]="tab().autoScroll !== false" (ngModelChange)="setAutoScroll($event)" /> Auto scroll</label>
        <button type="button" (click)="clearAll()" title="Clear the screen and the scrollback">Clear</button>
        @if (serial()) { <button type="button" (click)="tabs.saveScrollback(tab().id)" title="Save the output to a text file">Save…</button> }
        <button type="button" class="x" [title]="serial() ? 'Hide the send bar (right-click the terminal → Send Bar shows it again)' : 'Hide this bar (right-click the terminal → Terminal Bar shows it again; Global Options → Terminal hides it for every tab)'" (click)="tabs.patch(tab().id, { sendBar: false })">✕</button>
      </div>
    }
    @if (card() === 'connecting') {
      <div class="ccard-wrap">
        <div class="ccard" role="status">
          <div class="chd">
            <app-device-icon [kind]="cardIcon()" [size]="34" />
            <div><div class="ct">Connecting to {{ tab().title }}</div><div class="cs">{{ cardTarget() }}</div></div>
          </div>
          <ol class="steps">
            @for (st of steps(); track st.id; let i = $index) {
              <li [class.done]="i < stepIndex()" [class.now]="i === stepIndex()">
                <span class="mk">@if (i < stepIndex()) { <svg viewBox="0 0 24 24"><path d="M5 12.5l4.5 4.5L19 7.5" /></svg> }</span>
                <span class="sl">{{ i < stepIndex() ? st.done : i === stepIndex() ? st.now : st.todo }}</span>
                @if (i < stepIndex() && stepMs()[st.id] != null) { <span class="ms">{{ stepMs()[st.id] }} ms</span> }
              </li>
            }
          </ol>
          <div class="cfoot"><button type="button" (click)="tabs.close(tab().id)">Cancel</button></div>
        </div>
      </div>
    } @else if (card() === 'error' && fix(); as f) {
      <div class="ccard-wrap">
        <div class="ccard err" role="alert">
          <div class="chd">
            <div class="eic"><svg viewBox="0 0 24 24"><path d="M12 3l10 18H2zM12 10v5M12 18h.01" /></svg></div>
            <div><div class="ct">Could not connect to {{ tab().title }}</div><div class="cs">{{ errTarget() }}</div></div>
          </div>
          <p class="esum">{{ errSummary(f.hint) }}</p>
          @if (errBullets(f.hint); as bl) { @if (bl.length) { <ul class="ebul">@for (b of bl; track b) { <li>{{ b }}</li> }</ul> } }
          <details class="edet"><summary>Details</summary><code>{{ tab().message }}</code></details>
          <div class="eacts">
            <button type="button" class="eclose" title="Close this tab" (click)="tabs.close(tab().id)">Close Tab</button>
            @for (a of cardActions(f.actions); track a) { <button type="button" (click)="runFix(a)">{{ labels[a] }}</button> }
            <button type="button" class="primary" (click)="runFix('reconnect')">Try Again</button>
          </div>
        </div>
      </div>
    } @else if (fix(); as f) {
      <div class="fixbar" role="alert">
        <button type="button" class="fx" title="Dismiss" (click)="dismissed.set(tab().message)">✕</button>
        <div class="fmsg">{{ tab().message }}</div>
        @if (f.hint) { <div class="fhint">{{ f.hint }}</div> }
        <div class="facts">@for (a of f.actions; track a) { <button type="button" [class.primary]="$first" (click)="runFix(a)">{{ labels[a] }}</button> }</div>
      </div>
    }`,
  styles: [`
    .ccard-wrap { position: absolute; inset: 0; z-index: 6; display: flex; align-items: center; justify-content: center; background: var(--term-bg, var(--bg)); }
    .ccard { width: min(440px, calc(100% - 32px)); padding: 20px 22px 16px; border-radius: var(--r-lg, 12px); background: var(--surface, var(--panel)); border: 1px solid var(--line, var(--border));
      box-shadow: var(--shadow); display: flex; flex-direction: column; gap: 4px; color: var(--text);
      .chd { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; }
      .ct { font-weight: 600; font-size: calc(15px * var(--ui-scale, 1)); }
      .cs { color: var(--muted); font-family: var(--mono); font-size: calc(11.5px * var(--ui-scale, 1)); word-break: break-all; }
      .cfoot { display: flex; justify-content: flex-end; margin-top: 12px; } }
    @keyframes twspin { to { transform: rotate(360deg); } }
    .steps { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 9px;
      li { display: flex; align-items: center; gap: 10px; color: var(--faint, var(--muted)); }
      li.done { color: var(--text); } li.now { color: var(--text); }
      .sl { flex: 1; min-width: 0; }
      .ms { color: var(--faint, var(--muted)); font-family: var(--mono); font-size: calc(11px * var(--ui-scale, 1)); }
      .mk { width: 16px; height: 16px; flex: none; border-radius: 50%; border: 1.5px solid var(--line, var(--border)); display: inline-flex; align-items: center; justify-content: center;
        svg { width: 11px; height: 11px; fill: none; stroke: #fff; stroke-width: 2.8; stroke-linecap: round; stroke-linejoin: round; } }
      li.done .mk { background: var(--ok); border-color: var(--ok); }
      li.now .mk { border-color: var(--accent); border-top-color: transparent; animation: twspin .9s linear infinite; } }
    .ccard.err {
      .eic { width: 34px; height: 34px; flex: none; border-radius: 9px; display: flex; align-items: center; justify-content: center; background: rgba(229, 72, 77, .14); color: var(--danger);
        svg { width: 18px; height: 18px; fill: none; stroke: currentColor; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; } }
      .esum { margin: 0; line-height: 1.5; }
      .ebul { margin: 6px 0 0; padding-left: 18px; color: var(--muted); line-height: 1.5; font-size: calc(12.5px * var(--ui-scale, 1)); }
      .edet { margin-top: 8px; color: var(--faint, var(--muted)); font-size: calc(12px * var(--ui-scale, 1));
        summary { cursor: pointer; } code { display: block; margin-top: 6px; padding: 6px 8px; border-radius: var(--r-sm, 6px); background: var(--surface-2, var(--bg)); font-family: var(--mono); word-break: break-word; color: var(--muted); } }
      .eacts { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; margin-top: 14px; .eclose { margin-right: auto; } } }
    :host { display: flex; flex-direction: column; position: absolute; inset: 0; padding: 4px 0 0 6px; background: var(--term-bg); }
    .term-host { width: 100%; flex: 1; min-height: 0; }
    .paused { position: absolute; right: 18px; bottom: 44px; z-index: 4; display: flex; gap: 8px; align-items: center; padding: 4px 6px 4px 10px; border-radius: 6px;
      background: var(--panel); color: var(--text); border: 1px solid var(--border); box-shadow: var(--shadow); font-size: calc(12px * var(--ui-scale, 1));
      button { height: 22px; font-size: calc(11.5px * var(--ui-scale, 1)); padding: 0 8px; } }
    .sendbar { flex: none; display: flex; flex-wrap: wrap; row-gap: 4px; gap: 8px; align-items: center; margin: 4px 0 0 -6px; padding: 5px 8px; background: var(--panel); border-top: 1px solid var(--border); font-size: calc(12px * var(--ui-scale, 1)); color: var(--muted);
      .msg { flex: 1 1 160px; min-width: 160px; font-family: var(--mono); &.bad { border-color: var(--danger); } }
      select.mode { width: auto; }
      select { flex: none; width: 104px; }
      label { display: flex; gap: 4px; align-items: center; white-space: nowrap; cursor: pointer; }
      .sep { width: 1px; align-self: stretch; background: var(--border); }
      button { height: 24px; } .x { border: 0; background: none; color: var(--muted); padding: 0 4px; }
      .spacer { flex: 1; }
      .port { display: flex; align-items: center; gap: 6px; flex: 0 1 auto; min-width: 0; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
        font-family: var(--mono); font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--text); padding: 2px 8px; border: 1px solid var(--border); border-radius: 12px;
        .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--muted); flex: none; } &.on .dot { background: var(--ok, #22c55e); } } }
    :host(.modern) { padding: 12px 0 0 16px;
      .sendbar { margin: 8px 0 0 -16px; min-height: 34px; padding: 4px 12px; background: var(--surface); border-top-color: var(--line);
        button { border-radius: var(--r-sm); background: var(--surface-2); border-color: var(--line); } } }
    .sendbar.compact { padding: 2px 8px; button { height: 20px; font-size: calc(11.5px * var(--ui-scale, 1)); padding: 0 8px; } }
    .fixbar { position: absolute; top: 10px; right: 14px; z-index: 5; max-width: min(520px, 80%); padding: 10px 12px; border-radius: 8px;
      background: var(--panel); color: var(--text); border: 1px solid var(--border); border-left: 4px solid var(--danger); box-shadow: var(--shadow); font-size: calc(12.5px * var(--ui-scale, 1)); }
    .fmsg { font-weight: 600; margin-right: 18px; word-break: break-word; }
    .fhint { color: var(--muted); margin-top: 4px; }
    .facts { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; button { height: 24px; font-size: calc(12px * var(--ui-scale, 1)); } }
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

  readonly sendMode = signal<'text' | 'hex'>((() => { try { return localStorage.getItem('termwright.sendMode') === 'hex' ? 'hex' : 'text'; } catch { return 'text'; } })());
  readonly hexError = signal('');

  /** Text or hex view of what the device sends; the next output starts on a new line. */
  setHexView(on: boolean) {
    this.tabs.patch(this.tab().id, { hexView: on });
    const t = this.term;
    if (t && t.buffer.active.cursorX > 0) t.write('\r\n');
  }

  setSendMode(v: 'text' | 'hex') {
    this.sendMode.set(v);
    this.hexError.set('');
    try { localStorage.setItem('termwright.sendMode', v); } catch { /* storage unavailable */ }
  }

  sendLine() {
    const t = this.tab();
    if (t.state !== 'connected' || t.locked) return;
    const text = this.sendText();
    if (this.sendMode() === 'hex') {
      const bytes = parseHex(text);
      if (typeof bytes === 'string') { this.hexError.set(bytes); return; }
      if (!bytes.length) return;
      let bin = '';
      for (const b of bytes) bin += String.fromCharCode(b);
      void this.backend.sendBinary(t.id, btoa(bin));
      this.sent = [text, ...this.sent.filter(x => x !== text)].slice(0, 100);
      this.sentIdx = -1;
      this.sendText.set('');
      return;
    }
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
  /** Modern layout, before the first successful connect: a card instead of an empty terminal. */
  readonly card = computed(() => {
    const t = this.tab();
    if (t.wasConnected || this.ui.settings()?.layout === 'classic') return '';
    return t.state === 'connecting' ? 'connecting' : t.state === 'error' ? 'error' : '';
  });
  readonly steps = computed(() => {
    const s = this.tab().session;
    const host = s.hostname || 'the device';
    if (s.protocol === 'Serial') { const p = s.comPort || 'the serial port'; return [{ id: 'tcp', todo: 'Open ' + p, now: 'Opening ' + p + '…', done: 'Opened ' + p }]; }
    if (s.protocol === 'Local Shell') return [{ id: 'tcp', todo: 'Start the shell', now: 'Starting the shell…', done: 'Shell started' }];
    const out: { id: string; todo: string; now: string; done: string }[] = [];
    if (s.jumpSession) {
      const j = s.jumpSession.split('/').pop();
      out.push({ id: 'route', todo: 'Reach ' + j, now: 'Reaching ' + j + '…', done: 'Reached ' + j });
    } else out.push({ id: 'tcp', todo: 'Connect to ' + host, now: 'Connecting to ' + host + '…', done: 'Connected to ' + host });
    if (s.protocol === 'SSH2') {
      const who = s.username ? ' as ' + s.username : '';
      out.push({ id: 'hostkey', todo: 'Check the host key', now: 'Checking the host key…', done: 'Host key matches the saved one' },
        { id: 'auth', todo: 'Log in' + who, now: 'Logging in' + who + '…', done: 'Logged in' + who },
        { id: 'shell', todo: 'Start the shell', now: 'Starting the shell…', done: 'Shell started' });
    }
    return out;
  });
  readonly stepIndex = computed(() => Math.max(0, this.steps().findIndex(x => x.id === this.tab().step)));
  /** Milliseconds each finished step took. */
  readonly stepMs = signal<Record<string, number>>({});
  private stepAt = 0;
  private stepNow = '';
  private readonly trackSteps = effect(() => {
    const t = this.tab();
    const ids = this.steps().map(x => x.id);
    const cur = t.state === 'connecting' ? (t.step || ids[0]) : '';
    if (cur === this.stepNow) return;
    const now = performance.now();
    if (this.stepNow && this.stepAt) {
      const done = this.stepNow, ms = Math.round(now - this.stepAt);
      queueMicrotask(() => this.stepMs.update(m => ({ ...m, [done]: ms })));
    } else if (cur) queueMicrotask(() => this.stepMs.set({}));
    this.stepNow = cur;
    this.stepAt = cur ? now : 0;
  });
  /** Icon of the device type or protocol for the cards (public/device-*.svg). */
  cardIcon() {
    const s = this.tab().session;
    return deviceKindOf(s.deviceType) || (s.protocol === 'Serial' || s.protocol === 'Console' ? 'iot' : 'server');
  }
  cardTarget() {
    const s = this.tab().session;
    if (s.protocol === 'Serial') return `${s.comPort} · ${s.baudRate || 9600} baud`;
    if (s.protocol === 'Local Shell') return 'Local shell';
    return `${s.username ? s.username + '@' : ''}${s.hostname}${s.port ? ':' + s.port : ''}` + (s.jumpSession ? ' · through ' + s.jumpSession.split('/').pop() : '');
  }
  errTarget() {
    const s = this.tab().session;
    if (s.protocol === 'Serial') return `${s.comPort} · Serial`;
    return `${s.hostname}${s.port ? ':' + s.port : ''} · ${s.protocol}`;
  }
  /** One plain sentence for the error card. */
  errSummary(hint: string) {
    const m = this.tab().message, s = this.tab().session;
    const where = `${s.hostname}`, port = s.port || 22;
    if (/i\/o timeout|timed out|timeout/i.test(m)) return `No answer from ${where} on port ${port}.`;
    if (/connection refused/i.test(m)) return `${where} refused the connection on port ${port} - nothing is listening there, or a firewall rejects it.`;
    if (/no route to host|network is unreachable|host is down/i.test(m)) return `${where} cannot be reached from this computer (no route to it).`;
    const first = hint.split(/(?<=\.)\s+/)[0];
    return first || m;
  }
  /** What to check, one point each. */
  errBullets(hint: string) {
    const m = this.tab().message, s = this.tab().session;
    if (/i\/o timeout|timed out|timeout|connection refused|no route to host|network is unreachable|host is down/i.test(m) && !/jump host/i.test(m)) {
      const proto = s.protocol === 'SSH2' ? 'SSH' : s.protocol;
      return [
        'Check the address and the port of the session.',
        'The device may be off, or its address changed (DHCP)' + (s.mac ? ` - it was last seen with MAC ${s.mac}.` : '.'),
        `A firewall may block ${proto} from this computer, or a VPN may be needed.`,
      ];
    }
    const sum = this.errSummary(hint);
    return hint.split(/(?<=\.)\s+/).map(x => x.trim()).filter(x => x && x !== sum);
  }
  /** The error card's buttons after Try Again (Edit Session, Find by MAC, …). */
  cardActions(a: FixAction[]) { return a.filter(x => x !== 'reconnect'); }
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
      t.options.cursorStyle = s.cursorStyle;
      t.options.cursorBlink = s.cursorBlink;
      const theme = terminalTheme(this.tab().session.colorScheme, s, this.ui.lightUi());
      // modern layout with the automatic scheme: the cursor takes the accent colour
      if (s.layout !== 'classic' && !sess.colorScheme && (s.terminalScheme || 'Auto') === 'Auto') {
        theme.cursor = s.accentColor || (this.ui.lightUi() ? '#1f6fd6' : '#4f9cf7');
        theme.cursorAccent = String(theme.background);
      }
      t.options.theme = theme;
      this.compileKeywords();
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
    let hexCol = 0;
    this.unsub.push(this.backend.on('term:data:' + id, (b64: string) => {
      let bytes = b64ToBytes(b64);
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
      // Hex view: every byte as two hex digits, a new line after a line feed or 16 bytes.
      if (tv?.hexView) {
        let out = '';
        for (const b of bytes) {
          out += b.toString(16).toUpperCase().padStart(2, '0') + ' ';
          if (++hexCol >= 16 || b === 0x0a) { out += '\r\n'; hexCol = 0; }
        }
        bytes = new TextEncoder().encode(out);
      } else if (hexCol) { hexCol = 0; write('\r\n'); }
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
    const textStyle = s?.highlightStyle !== 'background';
    const bg = String(this.term?.options.theme?.background ?? '#1b1c1f');
    const lightTerm = /^#[0-9a-f]{6}$/i.test(bg) && contrastText(bg) === '#000000';
    for (const k of set?.keywords ?? []) {
      if (!k.text) continue;
      try {
        const src = k.regex ? k.text : k.text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        const color = /^#[0-9a-f]{6}$/i.test(k.color) ? k.color : '#6b5310';
        this.keywords.push(textStyle
          ? { re: new RegExp(src, k.caseSensitive ? 'g' : 'gi'), color: '', fg: textTone(color, lightTerm) }
          : { re: new RegExp(src, k.caseSensitive ? 'g' : 'gi'), color, fg: contrastText(color) });
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
        const d = term.registerDecoration(h.color
          ? { marker, x: h.x, width: h.w, backgroundColor: h.color, foregroundColor: h.fg, layer: 'top' }
          : { marker, x: h.x, width: h.w, foregroundColor: h.fg });
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
