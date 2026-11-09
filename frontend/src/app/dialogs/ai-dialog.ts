import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Tabs } from '../core/tabs.service';
import { Icon } from '../components/icon';
import { Autofocus } from '../core/autofocus';
import { AIAnswer, AIStatus } from '../core/models';
import { tr } from '../core/i18n';

type Src = 'selection' | 'clipboard' | 'terminal' | 'tabs';
const nLines = (t: string) => t ? t.split('\n').length : 0;
const tail = (t: string, n: number) => { if (!n) return t; const l = t.split('\n'); return l.length > n ? l.slice(-n).join('\n') : t; };

const PROVIDER: Record<string, string> = { anthropic: 'Claude', openai: 'OpenAI-compatible', ollama: 'Ollama', claudecode: 'Claude Code' };

/**
 * The AI assistant: Explain a selection (output or an error) or Suggest a
 * command for the tab's device type. Only what is shown here is sent, with
 * secrets removed; commands are pasted without Enter, never run.
 */
@Component({
  selector: 'app-ai-dialog',
  imports: [FormsModule, Icon, Autofocus],
  template: `
  @if (ui.aiReq(); as r) {
  <div class="modal-backdrop" (click)="close()">
    <div class="modal ai" (click)="$event.stopPropagation()">
      <div class="modal-head"><span class="mh-ic"><app-icon name="sparkles" [size]="17" /></span>
        <span class="mh-t">AI Assistant<span class="mh-s">{{ st()?.ready ? providerName() + ' · ' + (st()!.model || 'newest model') : 'Off' }}{{ r.deviceType ? ' · ' + r.deviceType : '' }}</span></span></div>
      <div class="modal-body">
        @if (st() && !st()!.ready) {
          <div class="off"><b>The AI assistant is off.</b> It explains device output and errors and suggests commands for your device types, with the AI service you choose: Claude, an OpenAI-compatible service (also Azure OpenAI or a company gateway), or Ollama on your computer or a lab server. Nothing is sent until you ask, and passwords, keys and SNMP communities are removed first.
            <div><button type="button" class="primary" (click)="setUp()">Set Up in Settings…</button></div></div>
        } @else {
          <div class="seg">
            <button type="button" [class.on]="kind() === 'explain'" (click)="kind.set('explain')">Ask About the Output</button>
            <button type="button" [class.on]="kind() === 'suggest'" (click)="kind.set('suggest')">Suggest a Command</button>
          </div>
          @if (kind() === 'explain') {
            <div class="lbl">What to send</div>
            <div class="src" role="radiogroup" aria-label="What to send">
              @for (o of sources(); track o.k) {
                <button type="button" role="radio" [attr.aria-checked]="source() === o.k" [class.on]="source() === o.k" [disabled]="!o.ok" [title]="o.note" (click)="pick(o.k)">
                  <b><span class="rd"></span>{{ o.label }}</b><span>{{ o.note }}</span></button>
              }
            </div>
            @if (heavy()) {
              <div class="warn"><app-icon name="alert" [size]="14" /><span><b>{{ tr('About {0} lines (≈ {1} tokens) will be sent.', lines().toLocaleString(), tokens().toLocaleString()) }}</b>
                {{ tr('That uses much more of your AI allowance than a selection - select the part you need in the terminal, or keep only the last') }}
                <select [ngModel]="keep()" (ngModelChange)="setKeep($event)" name="keep" class="inl" aria-label="Lines to keep">
                  <option [ngValue]="200">200 {{ tr('lines') }}</option><option [ngValue]="500">500 {{ tr('lines') }}</option><option [ngValue]="1000">1,000 {{ tr('lines') }}</option><option [ngValue]="0">{{ tr('all lines') }}</option>
                </select>.</span></div>
            }
            <div class="lbl">Your question <span class="muted">- optional; without one the AI explains the output</span></div>
            <input [(ngModel)]="question" name="qq" appAutofocus (keydown.enter)="ask()" placeholder="e.g. Why is port 3 down? Which ports are free?" />
            <div class="chips">
              @for (c of quick; track c) { <button type="button" (click)="question = tr(c); ask()" [disabled]="busy() || !text.trim()">{{ c }}</button> }
            </div>
            <details class="prev" [open]="source() === 'selection' || source() === 'clipboard'"><summary>{{ tr('What will be sent') }} <span class="muted">- {{ tr('edit it to send less; passwords, keys and SNMP communities are removed') }}</span></summary>
              <textarea class="mono" rows="7" [(ngModel)]="text" name="t" spellcheck="false" [placeholder]="tr('Select output in the terminal, or choose what to send above')"></textarea></details>
          } @else {
            <div class="lbl">What do you want to do?</div>
            <input [(ngModel)]="text" name="q" appAutofocus (keydown.enter)="ask()" placeholder="e.g. show the LLDP neighbours, or find which port a MAC address is on" />
          }
          <div class="row"><button type="button" class="primary" (click)="ask()" [disabled]="busy() || (kind() === 'explain' ? !text.trim() && !question.trim() : !text.trim())">{{ busy() ? 'Asking…' : kind() === 'explain' ? (question.trim() ? 'Ask' : 'Explain') : 'Suggest' }}</button>
            <span class="muted small">Sent to {{ providerName() }} with passwords, keys and SNMP communities removed{{ hideHosts() ? ', and addresses and host names replaced' : '' }}.</span></div>
          @if (error()) { <p class="err">{{ error() }}</p> }
          @if (ans(); as a) {
            <div class="answer">
              @for (p of parts(); track $index) {
                @if (p.code) {
                  <div class="code">
                    @for (c of p.lines; track $index) {
                      <div class="cmd"><code>{{ c }}</code>
                        @if (r.tabId) { <button type="button" (click)="paste(c)" title="Types it at the prompt without Enter and closes this window - you check it and press Enter">Paste</button> }
                        <button type="button" (click)="copy(c)">Copy</button>
                        <button type="button" (click)="save(c)" title="Adds it to the Command Library for this device type">Save</button></div>
                    }
                  </div>
                } @else { <p>{{ p.text }}</p> }
              }
            </div>
            <details class="sent"><summary>What was sent{{ a.removed ? ' (' + a.removed + ' removed)' : '' }}</summary><pre>{{ a.sent }}</pre></details>
            <p class="muted small">{{ a.model }} - AI answers can be wrong: check a command before you press Enter, especially one that changes the configuration.</p>
          }
        }
      </div>
      <div class="modal-foot"><span class="spacer"></span><button type="button" (click)="close()">Close</button></div>
    </div>
  </div>
  }`,
  styles: [`
    .ai { width: min(760px, 94vw); }
    .modal-body { max-height: min(680px, calc(100vh - 160px)); overflow: auto; }
    .off { line-height: 1.55; color: var(--muted); b { color: var(--text); } div { margin-top: 12px; } }
    .seg { width: min(360px, 100%); margin-bottom: 10px; }
    .lbl { color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); margin: 8px 0 5px; }
    textarea, input { width: 100%; } textarea { resize: vertical; font-size: calc(12px * var(--ui-scale, 1)); }
    .row { display: flex; gap: 10px; align-items: center; margin-top: 10px; }
    .muted { color: var(--muted); } .small { font-size: calc(12px * var(--ui-scale, 1)); }
    .err { color: var(--danger); white-space: pre-wrap; }
    .answer { margin-top: 14px; border-top: 1px solid var(--border); padding-top: 10px; line-height: 1.5; p { margin: 6px 0; white-space: pre-wrap; } }
    .code { border: 1px solid var(--border); border-radius: 8px; padding: 4px 8px; margin: 8px 0; background: color-mix(in srgb, var(--text) 4%, transparent); }
    .cmd { display: flex; gap: 6px; align-items: center; padding: 3px 0; code { flex: 1; font-family: var(--mono); font-size: calc(12.5px * var(--ui-scale, 1)); white-space: pre-wrap; }
      button { padding: 2px 8px; font-size: calc(11.5px * var(--ui-scale, 1)); } }
    .src { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px;
      button { text-align: left; display: flex; flex-direction: column; align-items: stretch; gap: 2px; height: auto; padding: 7px 10px; border-radius: var(--r); background: var(--surface); border: 1px solid var(--line); min-width: 0; font-size: calc(12.5px * var(--ui-scale, 1)); }
      button.on { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent) inset; }
      button:disabled { opacity: .45; }
      b { display: flex; gap: 7px; align-items: center; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
      span:not(.rd) { color: var(--muted); font-size: calc(11.5px * var(--ui-scale, 1)); overflow: hidden; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
      .rd { width: 11px; height: 11px; border-radius: 50%; border: 1.5px solid var(--muted); flex: none; }
      button.on .rd { border: 3.5px solid var(--accent); } }
    @media (max-width: 640px) { .src { grid-template-columns: 1fr 1fr; } }
    .warn { display: flex; gap: 8px; align-items: flex-start; font-size: calc(12px * var(--ui-scale, 1)); color: var(--muted); padding: 7px 10px; border-left: 3px solid var(--warn); background: color-mix(in srgb, var(--warn) 10%, transparent); border-radius: var(--r-sm); margin-top: 8px; line-height: 1.6;
      b { color: var(--text); } app-icon { color: var(--warn); margin-top: 3px; flex: none; } select.inl { width: auto; padding: 0 4px; height: auto; font-size: inherit; } }
    .chips { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 7px; button { padding: 2px 10px; border-radius: 999px; font-size: calc(11.5px * var(--ui-scale, 1)); } }
    .prev { margin-top: 10px; summary { cursor: pointer; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); margin-bottom: 5px; } }
    .sent { margin-top: 10px; summary { cursor: pointer; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); }
      pre { max-height: 180px; overflow: auto; font-size: calc(11.5px * var(--ui-scale, 1)); background: color-mix(in srgb, var(--text) 5%, transparent); padding: 8px; border-radius: 6px; } }
  `],
})
export class AIDialog implements OnInit {
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  readonly st = signal<AIStatus | null>(null);
  readonly kind = signal<'explain' | 'suggest'>('explain');
  readonly busy = signal(false);
  readonly error = signal('');
  readonly ans = signal<AIAnswer | null>(null);
  text = '';
  question = '';
  protected readonly tr = tr;
  readonly quick = ['Explain this', 'Find problems', 'Summarise', 'Commands to fix it'];

  /** What to send: the selection, the clipboard, the whole terminal, or the tabs in the send group. */
  readonly source = signal<Src>('selection');
  readonly keep = signal(0); // whole terminal / tabs: only the last N lines (0 = all)
  private raw: Record<Src, string> = { selection: '', clipboard: '', terminal: '', tabs: '' };
  private group: { id: string; title: string; deviceType: string }[] = [];
  readonly sources = signal<{ k: Src; label: string; note: string; ok: boolean }[]>([]);

  readonly providerName = computed(() => PROVIDER[this.st()?.provider ?? ''] ?? 'the AI service');
  readonly hideHosts = computed(() => !!this.ui.settings()?.ai?.hideHosts);

  /** The answer as paragraphs and code blocks (one command per line). */
  readonly parts = computed(() => {
    const out: { code: boolean; text?: string; lines?: string[] }[] = [];
    const t = this.ans()?.text ?? '';
    const re = /```[a-zA-Z0-9_+-]*\n([\s\S]*?)```/g;
    let last = 0, m: RegExpExecArray | null;
    while ((m = re.exec(t))) {
      const before = t.slice(last, m.index).trim();
      if (before) out.push({ code: false, text: before.replace(/\*\*(.+?)\*\*/g, '$1').replace(/`([^`]+)`/g, '$1') });
      out.push({ code: true, lines: m[1].split('\n').map(l => l.trimEnd()).filter(l => l.trim()) });
      last = m.index + m[0].length;
    }
    const rest = t.slice(last).trim();
    if (rest) out.push({ code: false, text: rest.replace(/\*\*(.+?)\*\*/g, '$1').replace(/`([^`]+)`/g, '$1') });
    return out;
  });

  ngOnInit() {
    const r = this.ui.aiReq();
    if (r) {
      this.kind.set(r.kind);
      if (r.kind === 'suggest') this.text = r.text;
      else void this.prepare(r.text, r.tabId);
    }
    void this.backend.aiState().then(s => this.st.set(s)).catch(() => this.st.set({ provider: '', model: '', hasKey: false, ready: false }));
  }

  /** Reads what can be sent and picks: the selection, else the send group, else the whole terminal, else the clipboard. */
  private async prepare(selection: string, tabId?: string) {
    const term = (id: string) => this.tabs.get(id)?.kind === 'terminal';
    this.raw.selection = (selection || (tabId && term(tabId) ? this.tabs.selectionText(tabId) : '')).trimEnd();
    this.raw.terminal = tabId && term(tabId) ? this.tabs.bufferText(tabId) : '';
    this.group = [...this.tabs.selected()].filter(term).map(id => { const t = this.tabs.get(id)!; return { id, title: t.title, deviceType: t.session.deviceType ?? '' }; });
    this.build();
    const first: Src = this.raw.selection ? 'selection' : this.group.length >= 2 ? 'tabs' : this.raw.terminal ? 'terminal' : 'clipboard';
    this.pick(first);
    try { this.raw.clipboard = (await this.backend.clipboardGet() ?? '').trimEnd(); } catch { this.raw.clipboard = ''; }
    this.build();
    if (first === 'clipboard' && this.raw.clipboard) this.pick('clipboard');
  }

  private build() {
    const n = (k: Src) => nLines(this.raw[k]);
    const g = this.group;
    this.sources.set([
      { k: 'selection', label: tr('Selected text'), note: n('selection') ? tr('{0} lines selected', n('selection')) : tr('nothing selected'), ok: !!this.raw.selection },
      { k: 'clipboard', label: tr('Copied text'), note: n('clipboard') ? tr('{0} lines on the clipboard', n('clipboard')) : tr('the clipboard is empty'), ok: !!this.raw.clipboard },
      { k: 'terminal', label: tr('Whole terminal'), note: this.raw.terminal ? tr('{0} lines with the scrollback', n('terminal').toLocaleString()) : tr('no terminal tab'), ok: !!this.raw.terminal },
      { k: 'tabs', label: g.length >= 2 ? tr('Selected tabs ({0})', g.length) : tr('Selected tabs'), note: g.length >= 2 ? g.map(x => x.title).join(', ') : tr('none - add tabs to the send group'), ok: g.length >= 2 },
    ]);
  }

  pick(k: Src) { this.source.set(k); this.text = this.textOf(k); }
  setKeep(n: number) { this.keep.set(Number(n) || 0); this.text = this.textOf(this.source()); }

  private textOf(k: Src) {
    if (k === 'tabs') return this.group.map(g => `===== ${g.title}${g.deviceType ? ' (' + g.deviceType + ')' : ''} =====\n` + tail(this.tabs.bufferText(g.id), this.keep())).join('\n\n');
    return k === 'terminal' ? tail(this.raw.terminal, this.keep()) : this.raw[k];
  }

  lines() { return nLines(this.text); }
  tokens() { return Math.ceil(this.text.length / 4); }
  /** Worth a warning: the whole terminal or several tabs, or a lot of text. */
  heavy() { return (this.source() === 'terminal' || this.source() === 'tabs') && this.tokens() > 1500 || this.tokens() > 8000; }

  close() { if (!this.busy()) this.ui.aiReq.set(null); }
  setUp() { this.ui.aiReq.set(null); this.ui.openOptions('ai'); }

  async ask() {
    const r = this.ui.aiReq();
    const explain = this.kind() === 'explain';
    if (!r || (!this.text.trim() && !(explain && this.question.trim()))) return;
    const types = new Set(this.group.map(g => g.deviceType));
    const deviceType = explain && this.source() === 'tabs' ? (types.size === 1 ? [...types][0] : '') : r.deviceType;
    this.busy.set(true); this.error.set(''); this.ans.set(null);
    try { this.ans.set(await this.backend.aiAsk({ kind: this.kind(), text: this.text, question: explain ? this.question : '', deviceType, protocol: r.protocol })); }
    catch (e) { this.error.set(errText(e)); }
    finally { this.busy.set(false); }
  }

  paste(c: string) {
    const id = this.ui.aiReq()?.tabId;
    if (!id) return;
    void this.tabs.paste(id, c);
    this.ui.aiReq.set(null); // back to the terminal: check it, then press Enter
    this.tabs.focus(id);
    this.ui.toast(tr('Pasted - check it, then press Enter in the terminal'), 'info');
  }
  async copy(c: string) { await this.backend.clipboardSet(c); this.ui.toast(tr('Copied'), 'success'); }

  /** Adds the command to the Command Library, for the tab's device type. */
  async save(c: string) {
    const s = this.ui.settings(), r = this.ui.aiReq();
    if (!s || !r) return;
    const dt = r.deviceType;
    const snippets = [...(s.snippets ?? []), { name: c.length > 48 ? c.slice(0, 47) + '…' : c, folder: dt ? dt + ' / AI' : 'AI', text: c, description: tr('Suggested by the AI assistant'), deviceTypes: dt ? [dt] : [] }];
    try { await this.ui.saveSettings({ ...s, snippets }); this.ui.toast(tr('Saved to the Command Library'), 'success'); }
    catch (e) { this.ui.error(e); }
  }
}
