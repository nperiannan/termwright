import { Component, ElementRef, SecurityContext, OnDestroy, computed, effect, inject, input, output, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DomSanitizer } from '@angular/platform-browser';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { kindOf, type FileKind } from '../core/file-kind';
import type { TextEditor } from '../core/cm-editor';

const MAX = 5 << 20;

/** The file viewer: one text file of a Files panel (SSH or this computer) to look at, check, follow and make
 * small edits to - not an IDE. Big files and programs go to their own app. */
@Component({
  selector: 'app-text-viewer',
  imports: [FormsModule],
  host: { '(keydown.escape)': 'esc($event)', tabindex: '-1', '[style.width.px]': 'width()', '[class]': "'k-' + kind()" },
  template: `
    <div class="grip" (mousedown)="resize($event)" (dblclick)="setWidth(0)" title="Drag to change the width (double-click: default)"></div>
    <div class="head">
      <div class="title">
        <span class="fi">{{ icon() }}</span><b class="nm" [title]="path()">{{ name() }}</b>
        @if (dirty()) { <span class="dot" title="Not saved">●</span> }
        <span class="crumb" [title]="path()">{{ folder() }}</span>
      </div>
      <div class="tools">
        @if (kind() === 'md' && !error()) {
          <div class="seg" role="group" aria-label="View"><button type="button" [class.on]="!preview()" (click)="setPreview(false)">Code</button><button type="button" [class.on]="preview()" (click)="setPreview(true)" title="Show the Markdown as it reads">Preview</button></div>
        }
        @if (kind() === 'json' && !tail() && !error()) { <button type="button" (click)="pretty()" title="Indent the JSON (or put it on one line)">{{ prettyOn ? 'Compact' : 'Pretty' }}</button> }
        @if (kind() === 'csv' && !error()) { <button type="button" [class.on]="table()" (click)="table.set(!table())" title="Show as a table">Table</button> }
        @if (kind() === 'log' && !error()) { <button type="button" [class.on]="following()" (click)="setFollow(!following())" title="Show new lines as they are written (like tail -f)">{{ following() ? '● Following' : 'Follow' }}</button> }
        <div class="seg" role="group" aria-label="Text">
          <button type="button" (click)="ed?.find()" [disabled]="!!error() || preview()" title="Find and replace (⌘F / Ctrl+F)">Find</button>
          <button type="button" [class.on]="wrap()" (click)="setWrap(!wrap())" [disabled]="!!error() || preview()" title="Wrap long lines">Wrap</button>
          <button type="button" (click)="copy()" [disabled]="!!error()" title="Copy the selection, or the whole file">Copy</button>
        </div>
        @if (canEdit() && !error()) {
          @if (!editing()) { <button type="button" (click)="edit()" [disabled]="!!filter() || errOnly() || tail()" [title]="tail() ? 'Only the end of the file is shown - open it in its app to edit' : 'Edit the file here'">Edit</button> }
          @else { <button type="button" class="pri" (click)="save()" [disabled]="!dirty() || saving()">{{ saving() ? 'Saving…' : 'Save' }}</button> }
        }
        <button type="button" (click)="openApp.emit()" [title]="kind() === 'code' ? 'Edit it in your editor' : 'Open it in the app this computer uses for it'">Open in App</button>
        <button type="button" class="x" (click)="close()" title="Close (Esc)" aria-label="Close">✕</button>
      </div>
    </div>
    @if (kind() === 'log' && !error()) {
      <div class="sub"><input class="flt" [ngModel]="filter()" (ngModelChange)="setFilter($event)" placeholder="Show only lines with… (text or /regex/)" spellcheck="false" />
        <button type="button" [class.on]="errOnly()" (click)="errOnly.set(!errOnly()); setFilter(filter())" title="Only errors and failures (error, fail, crit, alert, emerg, fatal)">Errors only</button>
        @if (filter() || errOnly()) { <span class="mut">{{ shownLines() }} of {{ allLines() }} lines</span> }</div>
    }
    @if (note(); as n) { <div class="note" [class.bad]="n.bad">{{ n.text }}@if (n.line) { <button type="button" class="lnk" (click)="ed?.goLine(n.line)">Go to line {{ n.line }}</button> }@if (n.reload) { <button type="button" class="lnk" (click)="load()">Reload</button> }</div> }
    @if (error()) { <div class="empty">{{ error() }}<button type="button" (click)="openApp.emit()">Open in App</button></div> }
    @else if (table() && kind() === 'csv') {
      <div class="tbl"><table>@for (r of rows(); track $index; let i = $index) { <tr>@for (c of r; track $index) { @if (i === 0) { <th>{{ c }}</th> } @else { <td>{{ c }}</td> } }</tr> }</table>
        @if (rowsCut()) { <div class="mut pad">The first 1000 rows.</div> }</div>
    }
    @else if (preview()) { <div class="md" (click)="mdClick($event)" [innerHTML]="html()"></div> }
    <div class="cm" #host [hidden]="!!error() || (table() && kind() === 'csv') || preview()"></div>
    <div class="status">
      @if (!error()) { <span>Ln {{ pos().line }}, Col {{ pos().col }}</span>@if (pos().sel) { <span>({{ pos().sel }} selected)</span> } }
      <span class="sp"></span>
      <span>{{ info() }}</span><span>{{ kindLabel() }}</span>@if (!error()) { <span>UTF-8</span><span>{{ eol() }}</span> }
      <span>{{ editing() ? 'Editing' : 'Read-only' }}</span>
    </div>
  `,
  styles: [`
    :host { position: absolute; top: 0; bottom: 0; right: 100%; display: flex; flex-direction: column; z-index: 20; outline: none;
      background: var(--vs-bg); border-left: 1px solid var(--border); box-shadow: -14px 0 36px #0007;
      /* VS Code Dark+ */
      --vs-bg: #1e1e1e; --vs-fg: #d4d4d4; --vs-head: #252526; --vs-head2: #2d2d2d; --vs-btn: #3a3d41; --vs-btn-hover: #45494e; --vs-bd: #3c3c3c;
      --vs-kw: #569cd6; --vs-ctrl: #c586c0; --vs-str: #ce9178; --vs-num: #b5cea8; --vs-re: #d16969; --vs-comment: #6a9955; --vs-prop: #9cdcfe; --vs-attr: #9cdcfe;
      --vs-tag: #569cd6; --vs-type: #4ec9b0; --vs-fn: #dcdcaa; --vs-var: #9cdcfe; --vs-heading: #569cd6; --vs-code: #ce9178; --vs-link: #4fc1ff; --vs-quote: #6a9955;
      --vs-punct: #808080; --vs-meta: #858585; --vs-err: #f44747; --vs-lineno: #858585; --vs-lineno-on: #c6c6c6; --vs-line: #2a2d2e; --vs-sel: #264f78;
      --vs-sel-other: #3a3d41; --vs-match: #623315; --vs-match-on: #515c6a; --vs-caret: #aeafad; --vs-status: #007acc;
      --vs-font: Menlo, Monaco, Consolas, 'Droid Sans Mono', 'Courier New', monospace; --vs-size: calc(12px * var(--ui-scale, 1)); --vs-lh: 18px; }
    :host-context(:root.light) {
      /* VS Code Light+ */
      --vs-bg: #ffffff; --vs-fg: #1f1f1f; --vs-head: #f3f3f3; --vs-head2: #f8f8f8; --vs-btn: #ffffff; --vs-btn-hover: #e8e8e8; --vs-bd: #d4d4d4;
      --vs-kw: #0000ff; --vs-ctrl: #af00db; --vs-str: #a31515; --vs-num: #098658; --vs-re: #811f3f; --vs-comment: #008000; --vs-prop: #0451a5; --vs-attr: #e50000;
      --vs-tag: #800000; --vs-type: #267f99; --vs-fn: #795e26; --vs-var: #001080; --vs-heading: #800000; --vs-code: #800000; --vs-link: #0000ee; --vs-quote: #0451a5;
      --vs-punct: #6e7681; --vs-meta: #6e7681; --vs-err: #cd3131; --vs-lineno: #237893; --vs-lineno-on: #0b216f; --vs-line: #f5f5f5; --vs-sel: #add6ff;
      --vs-sel-other: #e5ebf1; --vs-match: #f8c9ab; --vs-match-on: #a8ac94; --vs-caret: #000000; --vs-status: #007acc; box-shadow: -14px 0 36px #0002; }
    /* YAML keys are VS Code's tag colour, not JSON's property colour */
    :host(.k-yaml) { --vs-prop: var(--vs-tag); }
    :host-context(.tdock.left) { right: auto; left: 100%; border-left: 0; border-right: 1px solid var(--border); box-shadow: 14px 0 36px #0007; }
    .grip { position: absolute; left: -3px; top: 0; bottom: 0; width: 6px; cursor: col-resize; z-index: 2; }
    :host-context(.tdock.left) .grip { left: auto; right: -3px; }
    .head { flex: none; display: flex; align-items: center; gap: 10px; padding: 0 8px 0 12px; height: 40px; background: var(--vs-head); border-bottom: 1px solid var(--vs-bd); color: var(--vs-fg); min-width: 0; }
    .title { display: flex; align-items: baseline; gap: 7px; min-width: 0; flex: 1; }
    .fi { font-size: 12px; } .nm { font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; flex: none; max-width: 60%; }
    .crumb { color: var(--vs-meta); font-size: 11.5px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
    .dot { color: var(--vs-fg); }
    .tools { display: flex; align-items: center; gap: 6px; flex: none; }
    :host .head button, :host .sub button, :host .empty button { background-image: none; box-shadow: none; height: 26px; padding: 0 10px; font-size: 12px; white-space: nowrap; color: var(--vs-fg); background: var(--vs-btn); border: 1px solid var(--vs-bd); border-radius: 5px; cursor: pointer; }
    :host .head button:hover:not(:disabled), :host .sub button:hover:not(:disabled) { background: var(--vs-btn-hover); } :host button:disabled { opacity: .45; cursor: default; }
    :host .head button.on, :host .sub button.on, :host .head button.on:hover { color: #fff !important; background: var(--vs-status) !important; border-color: var(--vs-status) !important; }
    :host .head button.pri { color: #fff !important; background: #0e639c !important; border-color: #0e639c !important; } :host .head button.pri:hover:not(:disabled) { background: #1177bb !important; }
    :host .head button.x { width: 28px; padding: 0; background: transparent; border-color: transparent; font-size: 13px; } :host .head button.x:hover { background: var(--vs-btn-hover); }
    .seg { display: inline-flex; } :host .seg button { border-radius: 0; margin-left: -1px; } :host .seg button:first-child { border-radius: 5px 0 0 5px; margin-left: 0; } :host .seg button:last-child { border-radius: 0 5px 5px 0; }
    .sub { flex: none; display: flex; align-items: center; gap: 6px; padding: 5px 8px 5px 12px; background: var(--vs-head2); border-bottom: 1px solid var(--vs-bd); }
    .flt { flex: 1; min-width: 0; height: 26px; background: var(--vs-bg); color: var(--vs-fg); border: 1px solid var(--vs-bd); border-radius: 4px; padding: 0 8px; }
    .mut { color: var(--vs-meta); font-size: 11.5px; white-space: nowrap; }
    :host .note .lnk { background: none; border: 0; color: var(--vs-link); height: auto; padding: 0 6px; cursor: pointer; }
    .note { flex: none; padding: 6px 12px; font-size: 12px; color: var(--vs-fg); border-bottom: 1px solid var(--vs-bd); background: color-mix(in srgb, var(--vs-status) 14%, var(--vs-bg)); }
    .note.bad { background: color-mix(in srgb, var(--vs-err) 16%, var(--vs-bg)); }
    .cm { flex: 1; min-height: 0; overflow: hidden; background: var(--vs-bg); }
    .empty { flex: 1; display: flex; flex-direction: column; gap: 10px; align-items: center; justify-content: center; color: var(--vs-meta); padding: 24px; text-align: center; }
    .tbl { flex: 1; overflow: auto; font: var(--vs-size)/var(--vs-lh) var(--vs-font); color: var(--vs-fg); } .tbl table { border-collapse: collapse; } .pad { padding: 8px; }
    .tbl th, .tbl td { border: 1px solid var(--vs-bd); padding: 3px 10px; white-space: nowrap; text-align: left; } .tbl th { position: sticky; top: 0; background: var(--vs-head); }
    .status { flex: none; display: flex; gap: 14px; align-items: center; height: 22px; padding: 0 12px; font-size: 11.5px; color: #fff; background: var(--vs-status); white-space: nowrap; overflow: hidden; }
    .sp { flex: 1; }
    /* Markdown preview, as VS Code shows it */
    .md { flex: 1; overflow: auto; padding: 22px 32px 40px; color: var(--vs-fg); background: var(--vs-bg); font: 14px/1.6 -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, Ubuntu, sans-serif; }
    .md ::ng-deep :is(h1, h2) { padding-bottom: .3em; border-bottom: 1px solid var(--vs-bd); } .md ::ng-deep h1 { font-size: 2em; margin: .2em 0 .6em; } .md ::ng-deep h2 { font-size: 1.5em; margin: 1.2em 0 .6em; }
    .md ::ng-deep h3 { font-size: 1.25em; margin: 1.1em 0 .5em; } .md ::ng-deep :is(h4, h5, h6) { margin: 1em 0 .4em; }
    .md ::ng-deep :is(code, pre) { font-family: var(--vs-font); font-size: 0.9em; } .md ::ng-deep code { padding: 1px 4px; border-radius: 3px; background: color-mix(in srgb, var(--vs-fg) 10%, transparent); }
    .md ::ng-deep pre { padding: 12px 14px; border-radius: 4px; overflow: auto; background: color-mix(in srgb, var(--vs-fg) 7%, transparent); } .md ::ng-deep pre code { padding: 0; background: none; }
    .md ::ng-deep table { border-collapse: collapse; margin: 1em 0; } .md ::ng-deep :is(th, td) { border: 1px solid var(--vs-bd); padding: 5px 10px; } .md ::ng-deep th { background: color-mix(in srgb, var(--vs-fg) 6%, transparent); }
    .md ::ng-deep blockquote { margin: 0 0 1em; padding: 0 1em; border-left: 4px solid color-mix(in srgb, var(--vs-status) 60%, transparent); color: var(--vs-meta); }
    .md ::ng-deep a { color: var(--vs-link); } .md ::ng-deep hr { border: 0; border-top: 1px solid var(--vs-bd); } .md ::ng-deep img { max-width: 100%; } .md ::ng-deep :is(ul, ol) { padding-left: 2em; }
  `],
})
export class TextViewer implements OnDestroy {
  /** The Files panel's session, the file (its panel path) and whether it is this computer's. */
  readonly sid = input.required<string>();
  readonly path = input.required<string>();
  readonly local = input('');
  readonly closed = output<void>();
  readonly openApp = output<void>();
  readonly saved = output<void>();

  private backend = inject(Backend);
  private ui = inject(Ui);
  private host = viewChild<ElementRef<HTMLElement>>('host');
  ed?: TextEditor;

  readonly name = computed(() => this.path().slice(this.path().lastIndexOf('/') + 1));
  readonly kind = computed<FileKind>(() => kindOf(this.name()) || 'text');
  readonly kindLabel = computed(() => ({ json: 'JSON', yaml: 'YAML', xml: 'XML', yang: 'YANG', log: 'Log', config: 'Device config', csv: 'CSV', ini: 'Settings', shell: 'Shell script', code: this.codeLang() + ' (read-only)', md: 'Markdown', text: 'Plain text' })[this.kind()]);
  private codeLang() { const e = this.name().split('.').pop()?.toLowerCase() ?? ''; return ({ go: 'Go', py: 'Python', js: 'JavaScript', ts: 'TypeScript', tcl: 'TCL', c: 'C', h: 'C', cpp: 'C++', java: 'Java', rs: 'Rust', rb: 'Ruby', ps1: 'PowerShell' } as Record<string, string>)[e] ?? 'Code'; }
  readonly icon = computed(() => ({ json: '{ }', yaml: '≡', xml: '</>', yang: 'Y', log: '☰', config: '⚙', csv: '▦', ini: '⚙', shell: '$', code: '‹›', md: 'M↓', text: '¶' })[this.kind()]);
  /** The folder of the file, shown after its name. */
  readonly folder = computed(() => this.path().slice(0, this.path().lastIndexOf('/')) || '/');
  // ---- width: wider than the Files panel; drag the edge, remembered ----
  private readonly defWidth = () => Math.round(Math.max(600, Math.min(1280, window.innerWidth * 0.68)));
  readonly width = signal(Number(localStorage.getItem('termwright.viewerWidth')) || this.defWidth());
  setWidth(w: number) { const x = w || this.defWidth(); this.width.set(x); try { if (w) localStorage.setItem('termwright.viewerWidth', String(x)); else localStorage.removeItem('termwright.viewerWidth'); } catch { /* not kept */ } }
  resize(ev: MouseEvent) {
    ev.preventDefault();
    const x0 = ev.clientX, w0 = this.width(), left = !!(ev.currentTarget as HTMLElement).closest('.tdock.left');
    const move = (e: MouseEvent) => this.width.set(Math.max(420, Math.min(window.innerWidth - 360, w0 + (left ? e.clientX - x0 : x0 - e.clientX))));
    const up = () => { removeEventListener('mousemove', move); removeEventListener('mouseup', up); this.setWidth(this.width()); };
    addEventListener('mousemove', move); addEventListener('mouseup', up);
  }
  // ---- Markdown preview ----
  private sanitizer = inject(DomSanitizer);
  readonly preview = signal(false);
  readonly html = signal('');
  async setPreview(on: boolean) {
    try { localStorage.setItem('termwright.mdPreview', on ? '1' : '0'); } catch { /* not kept */ }
    if (on) {
      const { marked } = await import('marked');
      const src = this.ed ? this.ed.text() : this.full;
      // Angular's sanitizer drops scripts, event handlers and javascript: links of the file's HTML
      this.html.set(this.sanitizer.sanitize(SecurityContext.HTML, await marked.parse(src, { gfm: true })) ?? '');
    }
    this.preview.set(on);
  }
  /** Links in the preview open in the browser, never inside Termwright. */
  mdClick(ev: MouseEvent) {
    const a = (ev.target as HTMLElement).closest('a'); if (!a) return;
    ev.preventDefault();
    const href = a.getAttribute('href') ?? '';
    if (/^https?:\/\//i.test(href)) window.runtime?.BrowserOpenURL(href);
  }
  // ---- the status line ----
  readonly pos = signal({ line: 1, col: 1, sel: 0 });
  readonly eol = computed(() => { this.allLines(); return this.full.includes('\r\n') ? 'CRLF' : 'LF'; });
  /** Programs are only looked at here; editing them belongs in an editor. */
  readonly canEdit = computed(() => this.kind() !== 'code');
  readonly info = signal('');
  readonly error = signal('');
  readonly note = signal<{ text: string; bad?: boolean; line?: number; reload?: boolean } | null>(null);
  readonly editing = signal(false);
  readonly dirty = signal(false);
  readonly saving = signal(false);
  readonly wrap = signal(false);
  readonly table = signal(false);
  readonly following = signal(false);
  /** Only the end of a big file is shown (logs). */
  readonly tail = signal(false);
  readonly filter = signal('');
  readonly allLines = signal(0);
  readonly shownLines = signal(0);
  readonly errOnly = signal(false);
  private readonly errRe = /\b(emerg|alert|crit|critical|fatal|err|error|fail|failed|failure)\b/i;
  prettyOn = false;
  private full = '';
  private mod = 0;
  private timer?: ReturnType<typeof setInterval>;
  private check?: ReturnType<typeof setTimeout>;
  private setting = false;

  constructor() {
    effect(() => { this.sid(); this.path(); if (this.host()) void this.load(); });
  }

  async load() {
    this.error.set(''); this.note.set(null); this.preview.set(false); this.editing.set(false); this.dirty.set(false); this.tail.set(false); this.setFollow(false); this.filter.set(''); this.errOnly.set(false);
    try {
      const st = await this.backend.sftpStat(this.sid(), this.path());
      this.mod = st.mod;
      let text: string;
      if (st.size > MAX) {
        if (this.kind() !== 'log') { this.error.set(`${this.name()} is ${(st.size / 1048576).toFixed(1)} MB - too big for the viewer.`); return; }
        const t = await this.backend.sftpReadTail(this.sid(), this.path()); text = t.text; this.tail.set(true);
      } else text = await this.backend.sftpReadText(this.sid(), this.path());
      if (text.slice(0, 8000).includes('\u0000')) { this.error.set(`${this.name()} is not a text file.`); return; }
      this.full = text;
      this.info.set(this.sizeText(st.size) + (this.tail() ? ' · the last 2 MB' : ''));
      await this.show(text);
      if (this.kind() === 'json') this.checkJson();
      if (this.kind() === 'log') { this.setWrap(true); this.ed?.toEnd(); }
      if (this.kind() === 'md') { this.setWrap(true); let p = '0'; try { p = localStorage.getItem('termwright.mdPreview') ?? '0'; } catch { /* none */ } await this.setPreview(p === '1'); }
    } catch (e) { this.error.set(errText(e)); }
  }

  /** The file the editor was made for: another file gets a new editor (its own language and colours). */
  private edFor = '';
  private async show(text: string) {
    this.allLines.set(this.count(text)); this.shownLines.set(this.allLines());
    if (this.ed && this.edFor !== this.path()) { this.ed.destroy(); this.ed = undefined; }
    if (this.ed) { this.setting = true; this.ed.setText(text); this.setting = false; this.ed.setReadOnly(!this.editing()); return; }
    const { createEditor } = await import('../core/cm-editor');
    const el = this.host()?.nativeElement; if (!el) return;
    this.edFor = this.path();
    const k = this.kind();
    this.ed = createEditor(el, { text, kind: k, name: this.name(), readOnly: true, wrap: this.wrap(), numbers: !['log', 'text'].includes(k),
      onCursor: (line, col, sel) => this.pos.set({ line, col, sel }),
      onChange: () => { if (this.setting) return; this.dirty.set(true); if (this.kind() === 'json') { clearTimeout(this.check); this.check = setTimeout(() => this.checkJson(), 400); } } });
  }

  edit() { if (this.preview()) void this.setPreview(false); this.editing.set(true); this.ed?.setReadOnly(false); this.ed?.view.focus(); }

  async save() {
    if (!this.ed) return;
    const text = this.ed.text();
    if (this.kind() === 'json' && this.jsonError(text) && !(await this.ui.confirm('Save Invalid JSON?', 'The JSON is not valid: ' + this.jsonError(text)!.text, 'Save Anyway', true))) return;
    this.saving.set(true);
    try {
      this.mod = await this.backend.sftpSaveText(this.sid(), this.path(), text, this.mod);
      this.full = text; this.dirty.set(false); this.info.set(this.sizeText(new Blob([text]).size));
      this.note.set({ text: 'Saved ' + this.name() + (this.local() ? '' : ' on the server') + '.' }); setTimeout(() => this.note.set(null), 2500);
      this.saved.emit();
    } catch (e) { this.note.set({ text: errText(e), bad: true, reload: /changed since/.test(errText(e)) }); }
    finally { this.saving.set(false); }
  }

  pretty() {
    if (!this.ed) return;
    try {
      const v = JSON.parse(this.ed.text());
      this.prettyOn = !this.prettyOn;
      const edited = this.editing();
      this.setting = !edited;
      this.ed.setText(this.prettyOn ? JSON.stringify(v, null, 2) : JSON.stringify(v));
      this.setting = false;
    } catch { this.checkJson(); }
  }

  private jsonError(text: string): { text: string; line?: number } | null {
    try { JSON.parse(text); return null; } catch (e) {
      const m = /position (\d+)/.exec(String(e)); const lm = /line (\d+)/.exec(String(e));
      const line = lm ? Number(lm[1]) : m ? text.slice(0, Number(m[1])).split('\n').length : undefined;
      return { text: String((e as Error).message ?? e).replace(/^JSON\.parse: /, ''), line };
    }
  }
  private checkJson() {
    if (!this.ed) return;
    const e = this.jsonError(this.ed.text());
    if (e) this.note.set({ text: 'Not valid JSON' + (e.line ? ' at line ' + e.line : '') + ': ' + e.text, bad: true, line: e.line });
    else if (this.note()?.bad) this.note.set(null);
  }

  setWrap(on: boolean) { this.wrap.set(on); this.ed?.setWrap(on); }

  setFollow(on: boolean) {
    this.following.set(on);
    clearInterval(this.timer); this.timer = undefined;
    if (!on) return;
    this.timer = setInterval(async () => {
      try {
        const st = await this.backend.sftpStat(this.sid(), this.path());
        if (st.mod === this.mod) return;
        this.mod = st.mod;
        const text = st.size > MAX ? (await this.backend.sftpReadTail(this.sid(), this.path())).text : await this.backend.sftpReadText(this.sid(), this.path());
        this.full = text; this.info.set(this.sizeText(st.size) + (st.size > MAX ? ' · the last 2 MB' : ''));
        this.setting = true; this.ed?.setText(this.filtered(text)); this.setting = false; this.ed?.toEnd();
      } catch { /* gone for now */ }
    }, 2000);
  }

  setFilter(f: string) {
    this.filter.set(f);
    if (!this.ed) return;
    const out = this.filtered(this.full);
    this.setting = true; this.ed.setText(out); this.setting = false;
    this.shownLines.set(f || this.errOnly() ? this.count(out) : this.allLines());
  }
  private filtered(text: string) {
    if (this.errOnly()) text = text.split('\n').filter(l => this.errRe.test(l)).join('\n');
    const f = this.filter().trim(); if (!f) return text;
    let test: (l: string) => boolean;
    const m = /^\/(.*)\/([a-z]*)$/.exec(f);
    try { const re = m ? new RegExp(m[1], m[2]) : null; test = re ? l => re.test(l) : l => l.toLowerCase().includes(f.toLowerCase()); }
    catch { test = l => l.includes(f); }
    return text.split('\n').filter(test).join('\n');
  }

  readonly rows = computed(() => { this.table(); return this.parseCsv(this.full).slice(0, 1001); });
  readonly rowsCut = computed(() => this.rows().length > 1000);
  private parseCsv(text: string): string[][] {
    const sep = this.name().toLowerCase().endsWith('.tsv') ? '\t' : (text.split('\n')[0].split(';').length > text.split('\n')[0].split(',').length ? ';' : ',');
    const out: string[][] = []; let row: string[] = [], cell = '', q = false;
    for (let i = 0; i < text.length && out.length < 1002; i++) {
      const c = text[i];
      if (q) { if (c === '"' && text[i + 1] === '"') { cell += '"'; i++; } else if (c === '"') q = false; else cell += c; }
      else if (c === '"') q = true; else if (c === sep) { row.push(cell); cell = ''; }
      else if (c === '\n' || c === '\r') { if (c === '\r' && text[i + 1] === '\n') i++; row.push(cell); cell = ''; out.push(row); row = []; }
      else cell += c;
    }
    if (cell || row.length) { row.push(cell); out.push(row); }
    return out;
  }

  async copy() {
    if (!this.ed) return;
    const s = this.ed.view.state, sel = s.selection.main;
    const text = sel.empty ? s.doc.toString() : s.sliceDoc(sel.from, sel.to);
    await this.backend.clipboardSet(text);
    this.ui.toast(sel.empty ? 'Copied the whole file' : 'Copied', 'success', 1500);
  }

  async close() {
    if (this.dirty() && !(await this.ui.confirm('Close Without Saving?', this.name() + ' has changes that are not saved.', 'Close', true))) return;
    this.closed.emit();
  }
  esc(ev: Event) {
    if ((ev.target as HTMLElement)?.closest?.('.cm-panel')) return; // Esc closes the find bar first
    ev.preventDefault(); void this.close();
  }

  private count(s: string) { s = s.replace(/\n$/, ''); return s ? s.split('\n').length : 0; }
  private sizeText(n: number) { return n < 1024 ? n + ' B' : n < 1048576 ? (n / 1024).toFixed(1) + ' KB' : (n / 1048576).toFixed(1) + ' MB'; }

  ngOnDestroy() { clearInterval(this.timer); clearTimeout(this.check); this.ed?.destroy(); }
}
