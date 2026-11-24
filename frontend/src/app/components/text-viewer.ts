import { Component, ElementRef, OnDestroy, computed, effect, inject, input, output, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
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
  host: { '(keydown.escape)': 'esc($event)', tabindex: '-1' },
  template: `
    <div class="bar">
      <b class="nm" [title]="path()">{{ name() }}</b><span class="k">{{ kindLabel() }}</span>
      <span class="mut">{{ info() }}</span>
      @if (dirty()) { <span class="dot" title="Not saved">●</span> }
      <span class="sp"></span>
      @if (kind() === 'json' && !tail()) { <button type="button" (click)="pretty()" title="Indent the JSON (or put it on one line)">{{ prettyOn ? 'Compact' : 'Pretty' }}</button> }
      @if (kind() === 'csv') { <button type="button" [class.on]="table()" (click)="table.set(!table())" title="Show as a table">Table</button> }
      @if (kind() === 'log') { <button type="button" [class.on]="following()" (click)="setFollow(!following())" title="Show new lines as they are written (like tail -f)">{{ following() ? '● Following' : 'Follow' }}</button> }
      <button type="button" (click)="ed?.find()" title="Find and replace (⌘F / Ctrl+F)">Find</button>
      <button type="button" [class.on]="wrap()" (click)="setWrap(!wrap())" title="Wrap long lines">Wrap</button>
      <button type="button" (click)="copy()" title="Copy the selection, or the whole file">Copy</button>
      @if (canEdit()) {
        @if (!editing()) { <button type="button" (click)="edit()" [disabled]="!!filter() || errOnly() || tail()" [title]="tail() ? 'Only the end of the file is shown - open it in its app to edit' : 'Edit the file here'">Edit</button> }
        @else { <button type="button" class="pri" (click)="save()" [disabled]="!dirty() || saving()">{{ saving() ? 'Saving…' : 'Save' }}</button> }
      }
      <button type="button" (click)="openApp.emit()" [title]="kind() === 'code' ? 'Edit it in your editor' : 'Open it in the app this computer uses for it'">Open in App</button>
      <button type="button" class="x" (click)="close()" title="Close (Esc)">✕</button>
    </div>
    @if (kind() === 'log' && !error()) {
      <div class="bar sub"><input class="flt" [ngModel]="filter()" (ngModelChange)="setFilter($event)" placeholder="Show only lines with… (text or /regex/)" spellcheck="false" />
        <button type="button" [class.on]="errOnly()" (click)="errOnly.set(!errOnly()); setFilter(filter())" title="Only errors and failures (error, fail, crit, alert, emerg, fatal)">Errors only</button>
        @if (filter() || errOnly()) { <span class="mut">{{ shownLines() }} of {{ allLines() }} lines</span> }</div>
    }
    @if (note(); as n) { <div class="note" [class.bad]="n.bad">{{ n.text }}@if (n.line) { <button type="button" class="lnk" (click)="ed?.goLine(n.line)">Go to line {{ n.line }}</button> }@if (n.reload) { <button type="button" class="lnk" (click)="load()">Reload</button> }</div> }
    @if (error()) { <div class="empty">{{ error() }}<button type="button" (click)="openApp.emit()">Open in App</button></div> }
    @else if (table() && kind() === 'csv') {
      <div class="tbl"><table>@for (r of rows(); track $index; let i = $index) { <tr>@for (c of r; track $index) { @if (i === 0) { <th>{{ c }}</th> } @else { <td>{{ c }}</td> } }</tr> }</table>
        @if (rowsCut()) { <div class="mut pad">The first 1000 rows.</div> }</div>
    }
    <div class="cm" #host [hidden]="!!error() || (table() && kind() === 'csv')"></div>
  `,
  styles: [`
    :host { position: absolute; top: 0; bottom: 0; right: 100%; width: min(920px, 62vw); display: flex; flex-direction: column; background: var(--bg); border-left: 1px solid var(--border);
      box-shadow: -12px 0 32px #0006; z-index: 20; outline: none; }
    :host-context(.tdock.left) { right: auto; left: 100%; border-left: 0; border-right: 1px solid var(--border); box-shadow: 12px 0 32px #0006; }
    .bar { flex: none; display: flex; align-items: center; gap: 6px; padding: 6px 8px 6px 12px; border-bottom: 1px solid var(--border); min-width: 0; }
    .bar.sub { padding: 4px 8px 4px 12px; } .flt { flex: 1; min-width: 0; height: 24px; }
    .nm { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 30%; } .k { font-size: 11px; color: var(--muted); border: 1px solid var(--border); border-radius: 8px; padding: 0 6px; }
    .mut { color: var(--muted); font-size: 11.5px; white-space: nowrap; } .sp { flex: 1; } .dot { color: var(--accent); }
    button { height: 24px; padding: 0 8px; font-size: 12px; white-space: nowrap; } button.on { color: var(--accent); border-color: var(--accent); } button.x { width: 26px; padding: 0; }
    .lnk { background: none; border: 0; color: var(--accent); cursor: pointer; }
    .note { flex: none; padding: 5px 12px; font-size: 12px; border-bottom: 1px solid var(--border); background: color-mix(in srgb, var(--accent) 8%, transparent); }
    .note.bad { background: color-mix(in srgb, var(--danger, #f85149) 12%, transparent); }
    .cm { flex: 1; min-height: 0; overflow: hidden; }
    .empty { flex: 1; display: flex; flex-direction: column; gap: 10px; align-items: center; justify-content: center; color: var(--muted); padding: 24px; text-align: center; }
    .tbl { flex: 1; overflow: auto; font-size: 12px; } .tbl table { border-collapse: collapse; } .pad { padding: 8px; }
    .tbl th, .tbl td { border: 1px solid var(--border); padding: 3px 8px; white-space: nowrap; text-align: left; } .tbl th { position: sticky; top: 0; background: var(--panel); }
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
  readonly kindLabel = computed(() => ({ json: 'JSON', yaml: 'YAML', xml: 'XML', yang: 'YANG', log: 'log', config: 'config', csv: 'CSV', ini: 'settings', shell: 'shell script', code: 'code (read-only)', text: 'text' })[this.kind()]);
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
    this.error.set(''); this.note.set(null); this.editing.set(false); this.dirty.set(false); this.tail.set(false); this.setFollow(false); this.filter.set(''); this.errOnly.set(false);
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
    } catch (e) { this.error.set(errText(e)); }
  }

  private async show(text: string) {
    this.allLines.set(this.count(text)); this.shownLines.set(this.allLines());
    if (this.ed) { this.setting = true; this.ed.setText(text); this.setting = false; this.ed.setReadOnly(!this.editing()); return; }
    const { createEditor } = await import('../core/cm-editor');
    const el = this.host()?.nativeElement; if (!el) return;
    const k = this.kind();
    this.ed = createEditor(el, { text, kind: k, name: this.name(), readOnly: true, wrap: this.wrap(), numbers: !['log', 'text'].includes(k),
      onChange: () => { if (this.setting) return; this.dirty.set(true); if (this.kind() === 'json') { clearTimeout(this.check); this.check = setTimeout(() => this.checkJson(), 400); } } });
  }

  edit() { this.editing.set(true); this.ed?.setReadOnly(false); this.ed?.view.focus(); }

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
