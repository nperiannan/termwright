import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend, errText } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Tabs } from '../core/tabs.service';
import { Icon } from '../components/icon';
import { Autofocus } from '../core/autofocus';
import { AIAnswer, AIStatus } from '../core/models';
import { tr } from '../core/i18n';

const PROVIDER: Record<string, string> = { anthropic: 'Claude', openai: 'OpenAI-compatible', ollama: 'Ollama' };

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
      <div class="modal-head"><span class="mh-ic"><app-icon name="pulse" [size]="17" /></span>
        <span class="mh-t">AI Assistant<span class="mh-s">{{ st()?.ready ? providerName() + ' · ' + (st()!.model || 'newest model') : 'Off' }}{{ r.deviceType ? ' · ' + r.deviceType : '' }}</span></span></div>
      <div class="modal-body">
        @if (st() && !st()!.ready) {
          <div class="off"><b>The AI assistant is off.</b> It explains device output and errors and suggests commands for your device types, with the AI service you choose: Claude, an OpenAI-compatible service (also Azure OpenAI or a company gateway), or Ollama on your computer or a lab server. Nothing is sent until you ask, and passwords, keys and SNMP communities are removed first.
            <div><button type="button" class="primary" (click)="setUp()">Set Up in Settings…</button></div></div>
        } @else {
          <div class="seg">
            <button type="button" [class.on]="kind() === 'explain'" (click)="kind.set('explain')">Explain</button>
            <button type="button" [class.on]="kind() === 'suggest'" (click)="kind.set('suggest')">Suggest a Command</button>
          </div>
          @if (kind() === 'explain') {
            <div class="lbl">Output or error to explain <span class="muted">- edit it to send less</span></div>
            <textarea class="mono" rows="7" [(ngModel)]="text" name="t" spellcheck="false" placeholder="Select output in the terminal, then right-click → Explain with AI"></textarea>
          } @else {
            <div class="lbl">What do you want to do?</div>
            <input [(ngModel)]="text" name="q" appAutofocus (keydown.enter)="ask()" placeholder="e.g. show the LLDP neighbours, or find which port a MAC address is on" />
          }
          <div class="row"><button type="button" class="primary" (click)="ask()" [disabled]="busy() || !text.trim()">{{ busy() ? 'Asking…' : kind() === 'explain' ? 'Explain' : 'Suggest' }}</button>
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
    if (r) { this.kind.set(r.kind); this.text = r.text; }
    void this.backend.aiState().then(s => this.st.set(s)).catch(() => this.st.set({ provider: '', model: '', hasKey: false, ready: false }));
  }

  close() { if (!this.busy()) this.ui.aiReq.set(null); }
  setUp() { this.ui.aiReq.set(null); this.ui.openOptions('ai'); }

  async ask() {
    const r = this.ui.aiReq();
    if (!r || !this.text.trim()) return;
    this.busy.set(true); this.error.set(''); this.ans.set(null);
    try { this.ans.set(await this.backend.aiAsk({ kind: this.kind(), text: this.text, deviceType: r.deviceType, protocol: r.protocol })); }
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
