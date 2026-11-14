import { Component, ElementRef, HostListener, OnInit, computed, effect, inject, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Backend } from '../core/backend';
import { Ui } from '../core/ui.service';
import { Tabs } from '../core/tabs.service';
import { Icon } from './icon';
import { AGENT_PERMS, AGENT_TOOLS, AgentPerm, AIChat, ChatMsg, EFFORTS, Img, Step, modelChoices, nice } from '../core/ai-chat.service';
import { AIStatus } from '../core/models';
import { tr } from '../core/i18n';

const PROVIDER: Record<string, string> = { anthropic: 'Claude', openai: 'OpenAI-compatible', ollama: 'Ollama', claudecode: 'Claude Code' };

type Span = { t: string; b?: boolean; c?: boolean };
type Block = { k: 'p' | 'li'; s: Span[] } | { k: 'code'; lang: string; lines: string[] };

/** Markdown enough for answers: paragraphs, lists, **bold**, `code` and code blocks. */
function blocks(text: string): Block[] {
  const out: Block[] = [];
  const re = /```([a-zA-Z0-9_+-]*)\n([\s\S]*?)```/g;
  let last = 0, m: RegExpExecArray | null;
  const prose = (t: string) => {
    for (const para of t.split(/\n{2,}/)) {
      const lines = para.split('\n').filter(l => l.trim());
      let buf: string[] = [];
      const flush = () => { if (buf.length) out.push({ k: 'p', s: spans(buf.join(' ')) }); buf = []; };
      for (const l of lines) {
        const li = /^\s*(?:[-*•]|\d+[.)])\s+(.*)$/.exec(l);
        if (li) { flush(); out.push({ k: 'li', s: spans(li[1]) }); } else buf.push(l.replace(/^#+\s*/, ''));
      }
      flush();
    }
  };
  while ((m = re.exec(text))) {
    prose(text.slice(last, m.index));
    out.push({ k: 'code', lang: m[1], lines: m[2].replace(/\n$/, '').split('\n') });
    last = m.index + m[0].length;
  }
  prose(text.slice(last));
  return out;
}
function spans(t: string): Span[] {
  return t.split(/(\*\*[^*]+\*\*|`[^`]+`)/).filter(Boolean).map(x => x.startsWith('**') ? { t: x.slice(2, -2), b: true } : x.startsWith('`') && x.length > 1 ? { t: x.slice(1, -1), c: true } : { t: x });
}

/** The AI assistant as a chat panel: docked right or left, or floating. */
@Component({
  selector: 'app-ai-panel',
  imports: [FormsModule, Icon],
  host: { '[class]': "'aip ' + ai.dock()", '[style.width.px]': 'ai.width()', '[style.left.px]': "ai.dock() === 'float' ? fx() : null", '[style.top.px]': "ai.dock() === 'float' ? fy() : null", role: 'complementary', '[attr.aria-label]': "'AI Assistant'" },
  template: `
  <span class="grip" (mousedown)="resize($event)" title="Drag to resize"></span>
  <header class="hd" (mousedown)="move($event)">
    <span class="sp"><app-icon name="sparkles" [size]="16" /></span><span class="t">AI Assistant</span>
    <span class="modw"><button type="button" class="chipm" [class.on]="mmenu()" (click)="openModels()" [title]="provider() + ' - ' + tr('click to choose the model')" aria-haspopup="menu" [attr.aria-expanded]="mmenu()">{{ service() }}<app-icon name="chevron-down" [size]="11" /></button>
      @if (mmenu()) {
        <div class="menu models" role="menu" (click)="$event.stopPropagation()">
          <div class="mh">{{ tr('AI model') }}<small>{{ provider() }}</small></div>
          @if (isCC()) {
            <div class="grp">{{ tr('Effort') }}</div>
            <div class="effs" role="radiogroup" [attr.aria-label]="tr('Effort')">@for (e of efforts; track e.k) {
              <button type="button" role="radio" class="eff" [class.on]="(ui.settings()?.ai?.effort || '') === e.k" [attr.aria-checked]="(ui.settings()?.ai?.effort || '') === e.k" (click)="setEffort(e.k)" [title]="e.k ? tr(e.name) : tr('Default of the model')">{{ tr(e.name) }}</button> }</div>
          }
          <div class="grp">{{ tr('Model') }}</div>
          <button type="button" role="menuitemradio" [attr.aria-checked]="!st()?.model" (click)="setModel('')"><span class="ck">@if (!st()?.model) { <app-icon name="check" [size]="12" /> }</span>{{ tr('Default') }}<small>{{ lastModel() ? nice(lastModel()) : tr('the newest') }}</small></button>
          @if (models() === null) { <div class="mf">{{ tr('Loading the models…') }}</div> }
          @for (o of choices(); track o.value; let i = $index) {
            @if (o.group && o.group !== choices()[i - 1]?.group) { <div class="grp">{{ o.group }}</div> }
            <button type="button" role="menuitemradio" [attr.aria-checked]="st()?.model === o.value" (click)="setModel(o.value)"><span class="ck">@if (st()?.model === o.value) { <app-icon name="check" [size]="12" /> }</span>{{ o.label }}@if (o.hint) { <small class="mono">{{ o.hint }}</small> }</button>
          }
          @if (modelErr()) { <div class="mf">{{ modelErr() }}</div> }
          <button type="button" class="foot" (click)="mmenu.set(false); ui.openOptions('ai')"><app-icon name="options" [size]="12" />{{ tr('AI settings…') }}</button>
        </div>
      }</span>
    <span class="grow"></span>
    <button type="button" class="ib" [class.on]="ai.view() === 'history'" title="Chats" aria-label="Chats" (click)="ai.view.set(ai.view() === 'history' ? 'chat' : 'history')"><app-icon name="history" [size]="15" /></button>
    <span class="sep"></span>
    <button type="button" class="ib" [class.on]="ai.dock() === 'left'" title="Dock left" aria-label="Dock left" (click)="ai.setDock('left')"><app-icon name="dock-left" [size]="15" /></button>
    <button type="button" class="ib" [class.on]="ai.dock() === 'right'" title="Dock right" aria-label="Dock right" (click)="ai.setDock('right')"><app-icon name="dock-right" [size]="15" /></button>
    <button type="button" class="ib" [class.on]="ai.dock() === 'float'" title="Float" aria-label="Float" (click)="ai.setDock('float')"><app-icon name="float" [size]="15" /></button>
    <button type="button" class="ib" title="Close" aria-label="Close" (click)="ai.show(false)"><app-icon name="x" [size]="15" /></button>
  </header>

  @if (st() && !st()!.ready) {
    <div class="empty"><span class="big"><app-icon name="sparkles" [size]="26" /></span><h3>Set up the AI assistant</h3>
      <p>Choose the AI service: Claude Code with your Claude login, Claude with an API key, an OpenAI-compatible service, or Ollama on your own network.</p>
      <button type="button" class="primary" (click)="ui.openOptions('ai')">Open AI Settings…</button></div>
  } @else if (ai.view() === 'history') {
    <div class="conv hist">
      <button type="button" class="newc wide" (click)="ai.newChat()" [disabled]="ai.busy()"><app-icon name="plus" [size]="13" />{{ tr('New Chat') }}</button>
      @for (c of ai.chats(); track c.id) {
        <div class="hrow" [class.on]="c.id === ai.currentId()" (click)="ai.openChat(c.id)" role="button" tabindex="0" (keydown.enter)="ai.openChat(c.id)">
          <div class="grow"><div class="ht">{{ c.title }}</div><div class="hint"><app-icon name="shell" [size]="11" /> {{ c.device || tr('no tab') }} · {{ ago(c.updated) }}</div></div>
          <button type="button" class="ib sm" title="Delete" aria-label="Delete chat" (click)="ai.deleteChat(c.id); $event.stopPropagation()"><app-icon name="trash" [size]="13" /></button></div>
      } @empty { <p class="hint pad">{{ tr('Your chats appear here. They stay on this computer; the device output sent with them is not kept.') }}</p> }
    </div>
  } @else {
    <div class="sub">
      <button type="button" class="ctitle" (click)="ai.view.set('history')" [title]="tr('All chats')"><app-icon name="history" [size]="12" /><span>{{ ai.current()?.title || tr('New chat') }}</span></button>
      @if (ai.used().in + ai.used().out) { <span class="usage" [title]="tr('This chat so far, as the AI service counted: {0} tokens sent, {1} received', ai.used().in.toLocaleString(), ai.used().out.toLocaleString())">⇅ {{ k(ai.used().in + ai.used().out) }} {{ tr('tokens') }}</span> }
      <button type="button" class="newc" (click)="ai.newChat()" [disabled]="ai.busy() || !ai.current()"><app-icon name="plus" [size]="12" />{{ tr('New Chat') }}</button>
    </div>
    <div class="conv" #conv>
      @if (!msgs().length) {
        <div class="empty"><span class="big"><app-icon name="sparkles" [size]="26" /></span><h3>{{ tab() ? tr('Ask about this terminal') : tr('Ask the AI assistant') }}</h3>
          <p>@if (tab(); as t) { {{ tr('Using') }} <b>{{ t.title }}</b>{{ t.session.deviceType ? ' (' + t.session.deviceType + ')' : '' }}. }{{ tr('Select output to ask about just that part, paste a screenshot, or switch to Agent to let the AI run commands.') }}</p>
          <div class="sugg">@for (q of suggestions; track q) { <button type="button" (click)="text = tr(q); go()">{{ q }}</button> }</div></div>
      }
      @for (m of msgs(); track m.id) {
        @if (m.role === 'user') {
          <div class="msg"><div class="who"><span class="av u" aria-hidden="true"><svg viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="9.2" r="4.4" /><path d="M2.8 24c0-5.4 4.1-9 9.2-9s9.2 3.6 9.2 9z" /></svg></span>{{ tr('You') }} <span class="when">{{ time(m.time) }}</span>
            <span class="grow"></span><button type="button" class="mc" (click)="copy(m.text)" [title]="tr('Copy what you asked')" [attr.aria-label]="tr('Copy what you asked')"><app-icon name="copy" [size]="12" /></button></div>
            <div class="ub">
              @for (i of m.images ?? []; track $index) { @if (i.url) { <img class="img" [src]="i.url" [alt]="i.name" /> } @else { <span class="cx"><app-icon name="image" [size]="12" />{{ i.name }}</span> } }
              @if (m.ctx?.length) { <div class="ctx">@for (c of m.ctx!; track $index) { <span class="cx"><app-icon name="shell" [size]="12" /><b>{{ c.label }}</b> · {{ c.detail }}</span> }</div> }
              <div class="ut">{{ m.text }}</div></div></div>
        } @else {
          <div class="msg"><div class="who"><span class="av a"><app-icon name="sparkles" [size]="12" /></span>AI Assistant
            @if (m.model) { <span class="when" [title]="m.model">{{ nice(m.model) }}{{ m.secs ? ' · ' + m.secs + ' s' : '' }}</span> }
            @if (m.usage) { <span class="when tok" [title]="tr('Tokens as the AI service counted them: {0} sent (the chat so far and the instructions), {1} in the answer', m.usage.in.toLocaleString(), m.usage.out.toLocaleString())">↑ {{ k(m.usage.in) }} ↓ {{ k(m.usage.out) }}</span> }
            @if (m.text) { <span class="grow"></span><button type="button" class="mc" (click)="copy(m.text)" [title]="tr('Copy the answer')" [attr.aria-label]="tr('Copy the answer')"><app-icon name="copy" [size]="12" /></button> }</div>
            @if (m.error) { <div class="ab err">{{ m.error }}</div>
              @if (isSignIn(m.error)) { <div class="sib"><button type="button" class="b p" (click)="runSignIn()" [title]="tr('Opens a Local Shell tab and runs it there; sign in, then ask again')"><app-icon name="shell" [size]="12" />{{ tr('Run it in a Termwright tab') }}</button><button type="button" class="b" (click)="copySignIn()"><app-icon name="copy" [size]="12" />{{ tr('Copy the command') }}</button></div> } }
            @if (m.text) {
              <div class="ab">
                @for (b of render(m); track $index) {
                  @switch (b.k) {
                    @case ('code') {
                      <div class="cb"><div class="cbh">{{ $any(b).lang || tab()?.session?.deviceType || '' }}<span class="grow"></span>
                        @if (tab()) { <button type="button" class="b p" (click)="paste($any(b).lines)" [title]="tr('Types it in the open tab without Enter')"><app-icon name="paste" [size]="12" />Paste</button> }
                        <button type="button" class="b" (click)="copy($any(b).lines.join('\\n'))"><app-icon name="copy" [size]="12" />Copy</button>
                        <button type="button" class="b" (click)="saveCmd($any(b).lines)" [title]="tr('Adds it to the Command Library')"><app-icon name="star" [size]="12" />Save</button></div>
                        <pre>{{ $any(b).lines.join('\\n') }}</pre></div>
                    }
                    @case ('li') { <div class="li">•&nbsp;<span>@for (x of $any(b).s; track $index) { @if (x.b) { <b>{{ x.t }}</b> } @else if (x.c) { <code class="i">{{ x.t }}</code> } @else { {{ x.t }} } }</span></div> }
                    @default { <p>@for (x of $any(b).s; track $index) { @if (x.b) { <b>{{ x.t }}</b> } @else if (x.c) { <code class="i">{{ x.t }}</code> } @else { {{ x.t }} } }</p> }
                  }
                }
              </div>
            }
            @if (m.steps?.length) { <div class="used"><app-icon name="wrench" [size]="12" />{{ tr('Tools used') }}: {{ toolsUsed(m) }}</div> }
            @if (devices(m); as ds) { @if (ds.length) {
              <div class="devs" [attr.aria-label]="tr('Devices')">@for (d of ds; track d.tab) {
                <span class="dv" [class]="'dv ' + d.state" [title]="tr('{0}: {1} commands run, {2} with an error', d.tab, d.ok + d.err, d.err)"><i></i>{{ d.tab }}
                  @if (d.state === 'ask') { <small>{{ tr('waiting') }}</small> } @else if (d.state === 'running') { <small>{{ tr('working') }}</small> } @else if (!d.runs) { <small>{{ d.err ? tr('not connected') : tr('connected') }}</small> } @else { <small>✓{{ d.ok }}@if (d.err) { · ✗{{ d.err }} }</small> }</span> }</div>
            } }
            @for (s of m.steps ?? []; track $index) {
              @if (s.state === 'ask') {
                <div class="appr" [class.danger]="s.danger"><div class="q"><app-icon name="alert" [size]="15" /><div>
                  <b>{{ s.kind === 'open' ? tr('Connect to {0}?', s.tab) : s.kind === 'lab' ? tr('Change the emulated lab?') : tr('Run on {0}?', s.tab) }}</b>@if (s.prod) { <span class="prodb">PROD</span> }
                  @if (s.prod) { <div class="hint">{{ tr('Production session: Termwright asks before every command and connection, whatever the agent may do.') }}</div> }
                  <div class="hint">{{ s.danger ? tr('This may change the device or interrupt traffic.') : s.kind === 'open' ? tr('Opens the session in a new tab.') : s.kind === 'lab' ? 'lab ' + s.command : tr('Check the command - it is typed in the tab as it is.') }}</div>
                  @if (s.ask) { <div class="hint lask">{{ s.ask }}</div> }</div></div>
                  @if (s.kind === 'run') { <input class="mono" [(ngModel)]="edits[$index + '-' + m.id]" [placeholder]="s.command" [attr.aria-label]="tr('Command')" (focus)="seed(m.id, $index, s)" /> }
                  <div class="btns"><button type="button" class="primary" [class.danger]="s.danger" (click)="ai.decide(s, 'run', edits[$index + '-' + m.id])">{{ s.kind === 'open' ? tr('Connect') : s.kind === 'lab' ? tr('Do It') : tr('Run') }}</button>
                    @if (s.kind === 'lab' && !s.danger) { <button type="button" (click)="ai.decide(s, 'chat')" [title]="tr('Do this and the next lab changes of this chat without asking (not the ones that may interrupt traffic)')">{{ tr('Allow for This Chat') }}</button> }
                    <button type="button" (click)="ai.decide(s, 'skip')">Skip</button><button type="button" (click)="ai.decide(s, 'stop')">Stop</button></div></div>
              } @else {
                <details class="step" [class]="'step ' + s.state + (s.failed ? ' failed' : '')"><summary class="sh">
                  <span class="si">@switch (s.state) { @case ('done') { <app-icon [name]="s.failed ? 'alert' : 'check'" [size]="13" /> } @case ('running') { <span class="spin"></span> } @default { <app-icon name="x" [size]="13" /> } }</span>
                  <span class="grow">@switch (s.kind) {
                    @case ('open') { {{ tr('Connect to') }} <b>{{ s.tab }}</b> }
                    @case ('find') { <app-icon name="search" [size]="11" /> {{ tr('Find sessions') }} <b>{{ s.tab }}</b> }
                    @case ('log') { <app-icon name="log" [size]="11" /> {{ tr('Logger') }} {{ s.command }} · <b>{{ s.tab }}</b> }
                    @case ('lab') { <app-icon name="cube" [size]="11" /> {{ tr('Emulated lab') }} <code>{{ s.command }}</code> }
                    @case ('record') { <app-icon name="record" [size]="11" /> {{ tr('Recorder') }} {{ s.command }} · <b>{{ s.tab }}</b> }
                    @default { <code>{{ s.command }}</code> {{ tr('on') }} <b>{{ s.tab }}</b> } }</span>
                  <span class="hint">{{ s.note || (s.auto ? tr('auto') : s.state === 'done' ? tr('allowed') : '') }}</span></summary>
                  @if (s.output) { <div class="so"><button type="button" class="mc" (click)="copy(s.output)" [title]="tr('Copy the output')" [attr.aria-label]="tr('Copy the output')"><app-icon name="copy" [size]="12" /></button><pre>{{ s.output }}</pre></div> }</details>
              }
            }
          </div>
        }
      }
      @if (ai.busy()) { <div class="working"><span class="typing"><i></i><i></i><i></i></span>{{ waiting() ? tr('Waiting for you') : tr('Working…') }}</div> }
    </div>

    <div class="comp" (dragover)="$event.preventDefault()" (drop)="drop($event)">
      <div class="box" [class.busy]="ai.busy()">
        <div class="ctx">
          @for (c of ai.ctxs(); track c.id) { <span class="cx"><app-icon [name]="c.kind === 'clipboard' ? 'paste' : c.kind === 'file' ? 'file' : 'shell'" [size]="12" /><b>{{ c.label }}</b> · {{ c.detail }}
            <button type="button" class="x" [attr.aria-label]="tr('Remove')" (click)="ai.removeCtx(c.id)"><app-icon name="x" [size]="10" /></button></span> }
          <span class="addw"><button type="button" class="cx add" (click)="menu.set(!menu())" aria-haspopup="menu" [attr.aria-expanded]="menu()"><app-icon name="plus" [size]="11" />{{ tr('Add context') }}</button>
            @if (menu()) {
              <div class="menu" role="menu" (click)="$event.stopPropagation()">
                <button type="button" role="menuitem" (click)="pick('sel')"><app-icon name="shell" [size]="13" />{{ tr('Selected text') }}</button>
                <button type="button" role="menuitem" (click)="pick('clip')"><app-icon name="paste" [size]="13" />{{ tr('Copied text') }}</button>
                <button type="button" role="menuitem" (click)="pick('term')"><app-icon name="shell" [size]="13" />{{ tr('Whole terminal') }}</button>
                @for (t of otherTabs(); track t.id) { <button type="button" role="menuitem" (click)="pick('tab', t.id)"><app-icon name="tabs" [size]="13" />{{ t.title }}<small>{{ t.session.deviceType }}</small></button> }
                <button type="button" role="menuitem" (click)="pick('file')"><app-icon name="image" [size]="13" />{{ tr('Picture or file…') }}<small>⌘V</small></button>
              </div>
            }</span>
        </div>
        @if (ai.images().length) { <div class="atts">@for (i of ai.images(); track $index) { <span class="att"><img [src]="i.url" [alt]="i.name" /><button type="button" class="rm" [attr.aria-label]="tr('Remove')" (click)="ai.removeImage(i)"><app-icon name="x" [size]="10" /></button></span> }</div> }
        <textarea #ta [(ngModel)]="text" (ngModelChange)="grow(); at($event)" (keydown)="key($event)" (paste)="pasteIn($event)" (tw-paste-picture)="pastePicture($any($event).detail)" data-paste-pictures rows="3" spellcheck="false"
          [placeholder]="tr('Ask about the output, or describe what you want to do… (paste a picture with ⌘V, @ adds a tab)')" [attr.aria-label]="tr('Message')"></textarea>
        <div class="bar">
          <select class="sel" [ngModel]="ai.mode()" (ngModelChange)="ai.mode.set($event)" [attr.aria-label]="tr('Mode')" [title]="tr('Ask: answers only. Agent: the AI may run commands in your tabs.')"><option value="ask">Ask</option><option value="agent">Agent</option></select>
          @if (ai.mode() === 'agent') {
            <span class="permw"><button type="button" class="sel perm" [class]="'sel perm ' + permTone()" (click)="pmenu.set(!pmenu())" aria-haspopup="menu" [attr.aria-expanded]="pmenu()" [attr.aria-label]="tr('Agent permissions')" [title]="tr('What the agent may do without asking')"><i class="pd"></i><span>{{ tr(permName()) }}</span><app-icon name="chevron-down" [size]="11" /></button>
              @if (pmenu()) {
                <div class="menu perms" role="menu" (click)="$event.stopPropagation()">
                  <div class="mh">{{ tr('What the agent may do') }}</div>
                  @for (o of perms; track o.k) {
                    <button type="button" role="menuitemradio" [class]="'prow ' + o.tone" [attr.aria-checked]="ai.perm() === o.k" (click)="setPerm(o.k)"><i class="pd"></i><span class="pt"><b>{{ tr(o.name) }}</b><small>{{ tr(o.what) }}</small></span><span class="ck">@if (ai.perm() === o.k) { <app-icon name="check" [size]="13" /> }</span></button>
                  }
                  <div class="mf"><app-icon name="shield" [size]="11" /> {{ tr('Production sessions always ask before every command and connection - except in Read only, where nothing that changes anything runs.') }}</div>
                </div>
              }</span>
          }
          @if (ai.mode() === 'agent') {
            <span class="toolw"><button type="button" class="ib tl" [class.on]="tmenu()" [title]="tr('Tools the agent may use')" [attr.aria-label]="tr('Tools')" (click)="tmenu.set(!tmenu())"><app-icon name="wrench" [size]="14" /><span class="cnt">{{ toolCount() }}</span></button>
              @if (tmenu()) {
                <div class="menu tools" role="dialog" [attr.aria-label]="tr('Agent tools')" (click)="$event.stopPropagation()">
                  <div class="mh">{{ tr('Tools the agent may use') }}</div>
                  @for (t of agentTools; track t.k) {
                    <label class="trow"><input type="checkbox" [checked]="ai.tools()[t.k]" (change)="ai.setTool(t.k, $any($event.target).checked)" /><span><b>{{ tr(t.name) }}</b><small>{{ tr(t.what) }}</small></span></label>
                  }
                  <div class="mf">{{ tr('Each use shows in the chat. Logger and Recorder only write files on this computer, so they never ask.') }}</div>
                </div>
              }</span>
          }
          <button type="button" class="ib" [title]="tr('Attach a picture or a text file')" [attr.aria-label]="tr('Attach')" (click)="file.click()"><app-icon name="clip" [size]="14" /></button>
          <input #file type="file" hidden multiple accept="image/*,.txt,.log,.cfg,.conf,.csv,.json,.xml,.yaml,.yml" (change)="files($any($event.target).files); $any($event.target).value = ''" />
          <span class="grow"></span>
          @if (canSend()) { <span class="hint" [title]="tr('About how many tokens this question sends: the chat so far, what goes with it and your question')">≈ {{ k(ai.estimate(text)) }} {{ tr('tokens') }}</span> }
          @if (ai.busy()) { <button type="button" class="send stop" [title]="tr('Stop (Esc)')" [attr.aria-label]="tr('Stop')" (click)="ai.stop()"><span class="sq"></span></button> }
          @else { <button type="button" class="send" [title]="tr('Send (Enter) - Shift+Enter for a new line')" [attr.aria-label]="tr('Send')" (click)="go()" [disabled]="!canSend()"><app-icon name="up" [size]="15" /></button> }
        </div>
      </div>
      @if (ai.heavy()) {
        <div class="warn"><app-icon name="alert" [size]="13" /><span>{{ tr('About {0} tokens - that uses much more of your AI allowance. Keep the last', ai.tokens().toLocaleString()) }}
          <select [ngModel]="ai.keep()" (ngModelChange)="ai.keep.set(+$event)" [attr.aria-label]="tr('Lines to keep')"><option [ngValue]="0">{{ tr('all lines') }}</option><option [ngValue]="200">200</option><option [ngValue]="500">500</option><option [ngValue]="1000">1,000</option></select></span></div>
      }
    </div>
  }`,
  styles: [`
    :host { flex: none; display: flex; flex-direction: column; min-height: 0; min-width: 320px; max-width: 70vw; background: var(--panel); border-left: 1px solid var(--border); position: relative; font-size: calc(13px * var(--ui-scale, 1)); color: var(--text); }
    :host(.left) { border-left: 0; border-right: 1px solid var(--border); }
    :host(.float) { position: fixed; z-index: 60; height: min(680px, calc(100vh - 120px)); border: 1px solid var(--border); border-radius: 14px; box-shadow: 0 24px 60px rgba(0, 0, 0, .45); overflow: hidden; }
    .grip { position: absolute; top: 0; bottom: 0; left: -3px; width: 7px; cursor: col-resize; z-index: 3; }
    :host(.left) .grip { left: auto; right: -3px; } :host(.float) .grip { left: auto; right: 0; }
    .hd { display: flex; align-items: center; gap: 4px; padding: 7px 8px 7px 14px; border-bottom: 1px solid var(--border); background: var(--chrome); user-select: none; }
    :host(.float) .hd { cursor: move; }
    .t { font-weight: 600; white-space: nowrap; margin-left: 4px; } .sp { color: var(--accent); display: inline-flex; }
    .chipm { margin-left: 6px; padding: 2px 8px; border-radius: 999px; border: 1px solid var(--line); font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
    .grow { flex: 1; min-width: 0; } .sep { width: 1px; height: 16px; background: var(--line); margin: 0 3px; }
    .ib { width: 28px; height: 28px; padding: 0; border: 0; background: none; border-radius: 7px; display: inline-flex; align-items: center; justify-content: center; color: var(--muted); flex: none; }
    .ib:hover { color: var(--text); background: var(--hover); } .ib.on { color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); } .ib.sm { width: 24px; height: 24px; }
    .conv { flex: 1; overflow: auto; padding: 14px 14px 8px; display: flex; flex-direction: column; gap: 16px; scroll-behavior: smooth; }
    .msg { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
    .who { display: flex; align-items: center; gap: 7px; font-size: calc(12px * var(--ui-scale, 1)); font-weight: 600; .grow { flex: 1; } }
    .mc { width: 22px; height: 22px; padding: 0; border: 0; border-radius: 5px; background: transparent; color: var(--muted); display: inline-flex; align-items: center; justify-content: center; cursor: pointer; opacity: 0; transition: opacity .12s;
      &:hover { background: var(--surface-2); color: var(--text); } &:focus-visible { opacity: 1; } }
    .msg:hover .who .mc, .so:hover .mc { opacity: 1; }
    .ub, .ab, .ut { user-select: text; -webkit-user-select: text; }
    .so { position: relative; .mc { position: absolute; top: 4px; right: 6px; background: var(--surface); } }
    .av { width: 22px; height: 22px; border-radius: 50%; display: inline-flex; align-items: center; justify-content: center; font-size: 10.5px; font-weight: 700; flex: none; }
    .av.u { background: color-mix(in srgb, var(--muted) 42%, var(--surface)); color: color-mix(in srgb, var(--text) 88%, transparent); overflow: hidden; svg { width: 100%; height: 100%; display: block; } } .av.a { background: color-mix(in srgb, var(--accent) 18%, transparent); color: var(--accent); }
    .when, .hint { color: var(--muted); font-weight: 400; font-size: calc(11px * var(--ui-scale, 1)); }
    .hint { white-space: nowrap; flex: none; } .send { flex: none; }
    .appr .lask { white-space: pre-wrap; color: var(--text); margin-top: 3px; }
    .ub { margin-left: 29px; padding: 9px 12px; border-radius: 10px; background: var(--surface-2); border: 1px solid var(--line); line-height: 1.5; min-width: 0; }
    .ut { white-space: pre-wrap; overflow-wrap: anywhere; }
    .ctx { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 6px; }
    .cx { display: inline-flex; align-items: center; gap: 5px; padding: 2px 8px; border-radius: 6px; border: 1px solid var(--line); background: var(--bg); font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--muted); max-width: 100%; height: auto; }
    .cx b { color: var(--text); font-weight: 500; } .cx .x { border: 0; background: none; padding: 0; height: auto; color: var(--muted); display: inline-flex; }
    .cx.add { border-style: dashed; color: var(--accent); cursor: pointer; }
    .ab { margin-left: 29px; line-height: 1.55; min-width: 0; overflow-wrap: anywhere; } .ab p { margin: 0 0 8px; } .ab .li { display: flex; margin: 2px 0 2px 4px; }
    .ab.err { color: var(--danger); white-space: pre-wrap; }
    .sib { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 6px; }
    code.i { font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); padding: 0 4px; border-radius: 4px; background: var(--surface-2); }
    .cb { border: 1px solid var(--line); border-radius: 9px; overflow: hidden; margin: 6px 0 10px; background: var(--bg); }
    .cbh { display: flex; align-items: center; gap: 4px; padding: 4px 6px 4px 10px; border-bottom: 1px solid var(--line); background: var(--surface); font-size: calc(11px * var(--ui-scale, 1)); color: var(--muted); }
    .cbh .b, .sib .b { display: inline-flex; align-items: center; gap: 4px; padding: 2px 8px; height: 24px; border-radius: 6px; font-size: calc(11.5px * var(--ui-scale, 1)); }
    .cbh .b.p, .sib .b.p { background: var(--accent); border-color: var(--accent); color: #fff; }
    pre { margin: 0; padding: 8px 10px; font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); line-height: 1.5; white-space: pre; overflow: auto; max-height: 340px; user-select: text; }
    .img { max-width: 240px; max-height: 160px; border-radius: 8px; border: 1px solid var(--line); display: block; margin-bottom: 6px; }
    .step { margin-left: 29px; border: 1px solid var(--line); border-radius: 9px; background: var(--bg); font-size: calc(12px * var(--ui-scale, 1)); overflow: hidden; }
    .step .sh { display: flex; align-items: center; gap: 8px; padding: 7px 10px; cursor: pointer; list-style: none; } .step .sh::-webkit-details-marker { display: none; }
    .step code { font-family: var(--mono); font-size: calc(11.5px * var(--ui-scale, 1)); }
    .step pre { border-top: 1px solid var(--line); max-height: 220px; font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--muted); }
    .si { display: inline-flex; flex: none; } .step.done .si { color: var(--ok); } .step.error .si, .step.refused .si { color: var(--danger); } .step.skipped .si { color: var(--muted); }
    .spin { width: 12px; height: 12px; border-radius: 50%; border: 2px solid var(--line); border-top-color: var(--accent); animation: spin .8s linear infinite; }
    @keyframes spin { to { transform: rotate(360deg); } }
    .appr { margin-left: 29px; border: 1px solid color-mix(in srgb, var(--warn) 55%, var(--line)); border-radius: 10px; background: color-mix(in srgb, var(--warn) 8%, var(--surface)); padding: 10px 12px; display: flex; flex-direction: column; gap: 8px; }
    .appr.danger { border-color: color-mix(in srgb, var(--danger) 60%, var(--line)); background: color-mix(in srgb, var(--danger) 7%, var(--surface)); }
    .appr .q { display: flex; gap: 8px; align-items: flex-start; app-icon { color: var(--warn); margin-top: 2px; } } .appr.danger .q app-icon { color: var(--danger); }
    .appr input { width: 100%; font-family: var(--mono); }
    .btns { display: flex; gap: 6px; flex-wrap: wrap; } button.danger { background: var(--danger); border-color: var(--danger); color: #fff; }
    .working { display: flex; align-items: center; gap: 8px; margin-left: 29px; color: var(--muted); font-size: calc(12px * var(--ui-scale, 1)); }
    .typing { display: inline-flex; gap: 4px; } .typing i { width: 6px; height: 6px; border-radius: 50%; background: var(--muted); animation: blink 1.2s infinite ease-in-out; }
    .typing i:nth-child(2) { animation-delay: .15s; } .typing i:nth-child(3) { animation-delay: .3s; }
    @keyframes blink { 0%, 80%, 100% { opacity: .25; } 40% { opacity: 1; } }
    .comp { border-top: 1px solid var(--border); padding: 10px 12px 12px; background: var(--chrome); }
    .box { border: 1px solid var(--line); border-radius: 12px; background: var(--input-bg, var(--bg)); transition: border-color .15s, box-shadow .15s; }
    .box:focus-within { border-color: var(--accent); box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent); }
    .box .ctx { padding: 8px 10px 0; margin: 0; }
    .addw { position: relative; display: inline-flex; }
    .menu { position: absolute; left: 0; bottom: calc(100% + 6px); width: 240px; max-height: 300px; overflow: auto; background: var(--panel); border: 1px solid var(--border); border-radius: 10px; box-shadow: var(--shadow); padding: 5px; z-index: 5; display: flex; flex-direction: column; }
    .menu button { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border: 0; background: none; height: auto; border-radius: 6px; font-size: calc(12.5px * var(--ui-scale, 1)); text-align: left; }
    .menu button:hover, .menu button:focus-visible { background: color-mix(in srgb, var(--accent) 16%, transparent); } .menu small { margin-left: auto; color: var(--muted); }
    .atts { display: flex; gap: 8px; padding: 8px 10px 0; flex-wrap: wrap; }
    .att { position: relative; display: inline-block; } .att img { height: 54px; border-radius: 6px; border: 1px solid var(--line); display: block; }
    .att .rm { position: absolute; top: -6px; right: -6px; width: 18px; height: 18px; padding: 0; border-radius: 50%; background: var(--surface-2); border: 1px solid var(--line); display: inline-flex; align-items: center; justify-content: center; }
    textarea { display: block; width: 100%; min-height: 72px; max-height: 260px; resize: none; border: 0; outline: 0; background: transparent; padding: 8px 12px; line-height: 1.5; font: inherit; color: var(--text); box-shadow: none !important; }
    .bar { display: flex; align-items: center; gap: 6px; padding: 4px 8px 8px 10px; }
    .sel { width: auto; height: 26px; padding: 0 6px; font-size: calc(12px * var(--ui-scale, 1)); border-radius: 6px; }
    :host { --orange: #f5862e; --tone-ask: var(--warn); } :host-context(:root.light) { --orange: #d2600a; --tone-ask: #a87808; }
    .permw { position: relative; display: inline-flex; flex: 0 1 190px; min-width: 64px; }
    button.sel.perm { display: inline-flex; align-items: center; gap: 6px; width: 100%; min-width: 0; padding: 0 7px; border: 1px solid var(--line); background: var(--input-bg, var(--bg)); color: var(--text);
      span { flex: 1; min-width: 0; text-align: left; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; } }
    .pd { width: 8px; height: 8px; border-radius: 50%; flex: none; background: var(--muted); }
    .auto { --tone: var(--orange); } .ask { --tone: var(--tone-ask); } .ro { --tone: var(--text); }
    button.sel.perm.auto, button.sel.perm.ask { color: var(--tone); border-color: color-mix(in srgb, var(--tone) 45%, var(--line)); }
    button.sel.perm.auto { background: color-mix(in srgb, var(--orange) 10%, var(--input-bg, var(--bg))); }
    .auto .pd, .ask .pd { background: var(--tone); } .ro .pd { background: transparent; border: 1.5px solid var(--text); }
    .menu.perms { width: 300px; gap: 2px; max-height: none; }
    .menu .prow { align-items: flex-start; padding: 7px 8px; .pd { margin-top: 5px; } .pt { display: flex; flex-direction: column; gap: 1px; flex: 1; } b { font-weight: 600; color: var(--tone); } small { margin: 0; color: var(--muted); line-height: 1.35; } .ck { color: var(--accent); } }
    .ck { width: 14px; flex: none; display: inline-flex; }
    .modw { position: relative; display: inline-flex; min-width: 0; flex: 0 1 auto; } .modw .chipm { max-width: 100%; }
    button.chipm { display: inline-flex; align-items: center; gap: 4px; height: auto; background: none; cursor: pointer; } button.chipm:hover, button.chipm.on { color: var(--text); border-color: var(--accent); }
    .menu.models .grp { padding: 8px 8px 3px; font-size: calc(10.5px * var(--ui-scale, 1)); font-weight: 600; letter-spacing: .06em; text-transform: uppercase; color: var(--faint); }
    .effs { display: flex; flex-wrap: wrap; gap: 4px; padding: 2px 6px 6px; }
    .menu .eff { padding: 3px 9px; border: 1px solid var(--line); border-radius: 999px; font-size: calc(11.5px * var(--ui-scale, 1)); height: auto; }
    .menu .eff.on { border-color: var(--accent); color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); }
    .menu.models { top: calc(100% + 6px); bottom: auto; left: 0; width: 290px; max-height: min(520px, calc(100vh - 140px)); z-index: 8; .mh { display: flex; justify-content: space-between; small { margin: 0; font-weight: 400; } } .mono { font-family: var(--mono); font-size: calc(12px * var(--ui-scale, 1)); } .foot { border-top: 1px solid var(--line); border-radius: 0 0 6px 6px; margin-top: 4px; color: var(--muted); } }
    .prodb { margin-left: 6px; padding: 0 5px; border-radius: 4px; background: var(--danger); color: #fff; font-size: 10px; font-weight: 700; letter-spacing: .03em; vertical-align: 1px; }
    .devs { margin-left: 29px; display: flex; flex-wrap: wrap; gap: 6px; }
    .dv { display: inline-flex; align-items: center; gap: 6px; padding: 2px 9px; border-radius: 999px; border: 1px solid var(--line); background: var(--surface); font-size: calc(11.5px * var(--ui-scale, 1)); font-weight: 500;
      i { width: 7px; height: 7px; border-radius: 50%; background: var(--ok); } small { color: var(--muted); font-weight: 400; } }
    .dv.err i { background: var(--warn); } .dv.ask i { background: var(--warn); } .dv.running i { background: var(--accent); animation: blink 1.2s infinite; }
    .step.failed .si { color: var(--warn); }
    .send { width: 30px; height: 30px; padding: 0; border-radius: 8px; background: var(--accent); border-color: var(--accent); color: #fff; display: inline-flex; align-items: center; justify-content: center; }
    .send:disabled { opacity: .45; } .send.stop { background: var(--danger); border-color: var(--danger); } .sq { width: 10px; height: 10px; border-radius: 2px; background: #fff; }
    .warn { display: flex; gap: 6px; align-items: center; font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--warn); padding: 7px 2px 0; select { width: auto; height: auto; padding: 0 4px; font-size: inherit; } }
    .empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; padding: 24px; text-align: center; }
    .big { width: 52px; height: 52px; border-radius: 14px; display: inline-flex; align-items: center; justify-content: center; color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); }
    .empty h3 { margin: 4px 0 0; font-size: calc(16px * var(--ui-scale, 1)); } .empty p { margin: 0; color: var(--muted); line-height: 1.5; max-width: 340px; }
    .sugg { display: flex; flex-direction: column; gap: 6px; width: 100%; max-width: 340px; margin-top: 8px; }
    .sugg button { text-align: left; padding: 8px 12px; height: auto; border: 1px solid var(--line); border-radius: 9px; background: var(--surface); }
    .sugg button:hover { border-color: var(--accent); }
    .hist { gap: 2px; }
    .sub { display: flex; align-items: center; gap: 8px; padding: 6px 10px 6px 12px; border-bottom: 1px solid var(--line); background: var(--panel); min-height: 34px; }
    .ctitle { display: inline-flex; align-items: center; gap: 6px; border: 0; background: none; padding: 2px 4px; height: auto; color: var(--muted); min-width: 0; flex: 1; justify-content: flex-start;
      span { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; } &:hover { color: var(--text); } }
    .usage { font-size: calc(11px * var(--ui-scale, 1)); color: var(--muted); white-space: nowrap; padding: 1px 7px; border: 1px solid var(--line); border-radius: 999px; }
    .newc { display: inline-flex; align-items: center; gap: 4px; padding: 2px 9px; height: 24px; font-size: calc(12px * var(--ui-scale, 1)); white-space: nowrap; border-radius: 6px; }
    .newc.wide { justify-content: center; margin-bottom: 8px; height: 30px; }
    .tok { font-variant-numeric: tabular-nums; }
    .used { margin-left: 29px; display: flex; align-items: center; gap: 6px; font-size: calc(11.5px * var(--ui-scale, 1)); color: var(--muted); }
    .toolw { position: relative; display: inline-flex; }
    .ib.tl { width: auto; padding: 0 6px; gap: 3px; } .cnt { font-size: 10.5px; font-weight: 600; }
    .menu.tools { width: 280px; left: auto; right: -80px; gap: 2px; }
    .mh { font-weight: 600; padding: 6px 8px 4px; font-size: calc(12px * var(--ui-scale, 1)); } .mf { padding: 6px 8px 4px; font-size: calc(11px * var(--ui-scale, 1)); color: var(--muted); border-top: 1px solid var(--line); margin-top: 4px; }
    .trow { display: flex; gap: 8px; align-items: flex-start; padding: 6px 8px; border-radius: 6px; cursor: pointer; input { margin-top: 2px; } span { display: flex; flex-direction: column; } b { font-weight: 500; } small { color: var(--muted); }
      &:hover { background: var(--hover); } } .hrow { display: flex; align-items: center; gap: 6px; padding: 8px 10px; border-radius: 8px; cursor: pointer; }
    .hrow:hover { background: var(--hover); } .hrow.on { background: color-mix(in srgb, var(--accent) 12%, transparent); }
    .ht { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; } .pad { padding: 10px; }
    @media (prefers-reduced-motion: reduce) { .conv { scroll-behavior: auto; } .typing i, .spin { animation: none; } }
  `],
})
export class AIPanel implements OnInit {
  protected ai = inject(AIChat);
  protected ui = inject(Ui);
  private backend = inject(Backend);
  private tabs = inject(Tabs);
  private host = inject(ElementRef<HTMLElement>);
  protected readonly tr = tr;
  readonly conv = viewChild<ElementRef<HTMLElement>>('conv');
  readonly ta = viewChild<ElementRef<HTMLTextAreaElement>>('ta');
  readonly file = viewChild.required<ElementRef<HTMLInputElement>>('file');
  readonly st = signal<AIStatus | null>(null);
  readonly menu = signal(false);
  readonly tmenu = signal(false);
  readonly pmenu = signal(false);
  readonly mmenu = signal(false);
  readonly models = signal<string[] | null>(null);
  readonly modelErr = signal('');
  protected readonly perms = AGENT_PERMS;
  readonly permTone = computed(() => AGENT_PERMS.find(p => p.k === this.ai.perm())?.tone ?? 'ask');
  readonly permName = computed(() => AGENT_PERMS.find(p => p.k === this.ai.perm())?.name ?? 'Changes ask first');
  readonly provider = computed(() => PROVIDER[this.st()?.provider ?? ''] ?? '');
  /** The model that answered last in this chat - what "Default" is. */
  readonly lastModel = computed(() => [...(this.ai.current()?.msgs ?? [])].reverse().find(m => m.role === 'assistant' && m.model && m.model !== 'Claude Code')?.model ?? '');
  protected readonly agentTools = AGENT_TOOLS;
  readonly toolCount = computed(() => AGENT_TOOLS.filter(t => this.ai.tools()[t.k]).length);
  readonly fx = signal(Math.max(20, innerWidth - 500));
  readonly fy = signal(90);
  text = '';
  edits: Record<string, string> = {};
  readonly suggestions = ['Explain the last output', 'Find problems in this session', 'Summarise what happened in this session', 'Give me the command to show the LLDP neighbours'];
  readonly initials = 'Me';

  readonly msgs = computed(() => (this.ai.current()?.msgs ?? []).filter(m => !m.hidden));
  readonly tab = computed(() => { const t = this.tabs.active(); return t && t.kind === 'terminal' ? t : null; });
  readonly otherTabs = computed(() => this.tabs.tabs().filter(t => t.kind === 'terminal' && t.id !== this.tab()?.id));
  readonly service = computed(() => { const s = this.st(); if (!s?.provider) return tr('Off'); const m = s.model || this.lastModel(); const e = EFFORTS.find(x => x.k && x.k === this.ui.settings()?.ai?.effort);
    return (m ? nice(m) : PROVIDER[s.provider] ?? s.provider) + (e && s.provider === 'claudecode' ? ' · ' + tr(e.name) : ''); });
  protected readonly nice = nice;
  readonly waiting = computed(() => (this.ai.current()?.msgs ?? []).some(m => (m.steps ?? []).some(s => s.state === 'ask')));
  private renders = new WeakMap<ChatMsg, { text: string; b: Block[] }>();

  constructor() {
    // follow the conversation to its end
    effect(() => { this.ai.current(); this.ai.busy(); queueMicrotask(() => { const c = this.conv()?.nativeElement; if (c) c.scrollTop = c.scrollHeight; }); });
    effect(() => { this.ui.settings()?.ai; void this.backend.aiState().then(s => this.st.set(s)).catch(() => this.st.set({ provider: '', model: '', hasKey: false, ready: false })); });
  }
  ngOnInit() { setTimeout(() => this.ta()?.nativeElement.focus(), 50); }

  render(m: ChatMsg) { const c = this.renders.get(m); if (c && c.text === m.text) return c.b; const b = blocks(m.text); this.renders.set(m, { text: m.text, b }); return b; }
  time(iso: string) { const d = new Date(iso); return isNaN(+d) ? '' : d.toTimeString().slice(0, 5); }
  ago(iso: string) {
    const s = (Date.now() - +new Date(iso)) / 1000;
    if (s < 60) return tr('just now'); if (s < 3600) return tr('{0} min ago', Math.floor(s / 60)); if (s < 86400) return tr('{0} h ago', Math.floor(s / 3600));
    return tr('{0} days ago', Math.floor(s / 86400));
  }
  k(n: number) { return n >= 1000 ? (n / 1000).toFixed(n >= 100000 ? 0 : 1) + 'k' : String(n); }
  toolsUsed(m: ChatMsg) {
    const n = (k: string) => (m.steps ?? []).filter(s => s.kind === k).length;
    return [['run', 'Run commands'], ['find', 'Find sessions'], ['open', 'Connect to sessions'], ['log', 'Logger'], ['record', 'Recorder']]
      .map(([k, l]) => n(k) ? tr(l) + ' ×' + n(k) : '').filter(Boolean).join(' · ');
  }
  /** Per device: commands that ran, those the device refused, and what it is doing now. */
  devices(m: ChatMsg) {
    const by = new Map<string, { tab: string; ok: number; err: number; runs: number; state: string }>();
    for (const s of m.steps ?? []) {
      if (s.kind !== 'run' && s.kind !== 'open') continue;
      const d = by.get(s.tab) ?? { tab: s.tab, ok: 0, err: 0, runs: 0, state: 'done' };
      if (s.kind === 'run') d.runs++;
      if (s.kind === 'run' && s.state === 'done') { if (s.failed) d.err++; else d.ok++; }
      if (s.state === 'error') d.err++;
      if (s.state === 'ask' || s.state === 'running') d.state = s.state;
      else if (d.state === 'done' && d.err) d.state = 'err';
      by.set(s.tab, d);
    }
    return [...by.values()];
  }
  openModels() {
    this.mmenu.set(!this.mmenu());
    if (!this.mmenu()) return;
    this.models.set(null); this.modelErr.set('');
    const a = this.ui.settings()?.ai;
    if (!a?.provider) { this.models.set([]); return; }
    this.backend.aiModels(a).then(l => this.models.set(l ?? [])).catch(e => { this.models.set([]); this.modelErr.set(String(e?.message ?? e)); });
  }
  readonly isCC = computed(() => this.st()?.provider === 'claudecode');
  protected readonly efforts = EFFORTS;
  readonly choices = computed(() => modelChoices(this.st()?.provider ?? '', this.models() ?? []));
  setEffort(e: string) {
    const s = this.ui.settings(); if (!s) return;
    void this.ui.saveSettings({ ...s, ai: { ...(s.ai ?? {}), effort: e } });
  }
  setModel(m: string) {
    this.mmenu.set(false);
    const s = this.ui.settings(); if (!s) return;
    void this.ui.saveSettings({ ...s, ai: { ...(s.ai ?? {}), model: m } });
  }
  canSend() { return !!(this.text.trim() || this.ai.ctxs().length || this.ai.images().length); }

  go() { if (!this.canSend() || this.ai.busy()) return; const t = this.text; this.text = ''; this.grow(); this.menu.set(false); void this.ai.send(t); }
  key(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); this.go(); }
    else if (e.key === 'Escape') { if (this.menu()) { this.menu.set(false); e.stopPropagation(); } else if (this.ai.busy()) { this.ai.stop(); e.stopPropagation(); } }
  }
  /** "@" opens the context menu (another tab). */
  at(v: string) { if (v.endsWith('@')) { this.text = v.slice(0, -1); this.menu.set(true); } }
  grow() { const t = this.ta()?.nativeElement; if (!t) return; t.style.height = 'auto'; t.style.height = Math.min(260, Math.max(72, t.scrollHeight)) + 'px'; }

  pick(k: string, id?: string) {
    this.menu.set(false);
    if (k === 'sel') this.ai.addSelection(); else if (k === 'clip') void this.ai.addClipboard(); else if (k === 'term') this.ai.addTerminal();
    else if (k === 'tab') this.ai.addTerminal(id); else if (k === 'file') this.file().nativeElement.click();
    this.ta()?.nativeElement.focus();
  }

  pasteIn(e: ClipboardEvent) {
    const d = e.clipboardData;
    let fs = [...(d?.files ?? [])].filter(f => f.type.startsWith('image/'));
    if (!fs.length) fs = [...(d?.items ?? [])].filter(i => i.kind === 'file' && i.type.startsWith('image/')).map(i => i.getAsFile()).filter((f): f is File => !!f);
    if (fs.length) { e.preventDefault(); this.files(fs); }
  }
  /** A picture from the system clipboard, through the app's Paste (⌘V / Ctrl+V in its Edit menu). */
  pastePicture(p: { data: string; type: string }) {
    const n = (this.ai.images().length || 0) + 1;
    this.ai.addImage({ name: n > 1 ? `picture-${n}.png` : 'picture.png', mediaType: p.type, data: p.data, url: `data:${p.type};base64,${p.data}` } as Img);
  }
  drop(e: DragEvent) { e.preventDefault(); if (e.dataTransfer?.files?.length) this.files(e.dataTransfer.files); }
  files(list: FileList | File[]) {
    for (const f of [...list]) {
      if (f.type.startsWith('image/')) {
        if (f.size > 5 * 1024 * 1024) { this.ui.toast(tr('{0} is larger than 5 MB', f.name), 'error'); continue; }
        const r = new FileReader();
        r.onload = () => { const url = String(r.result); this.ai.addImage({ name: f.name || 'picture.png', mediaType: f.type, data: url.slice(url.indexOf(',') + 1), url } as Img); };
        r.readAsDataURL(f);
      } else if (f.size <= 1024 * 1024) {
        const r = new FileReader(); r.onload = () => this.ai.addFile(f.name, String(r.result)); r.readAsText(f);
      } else this.ui.toast(tr('{0} is larger than 1 MB', f.name), 'error');
    }
  }

  seed(id: number, i: number, s: Step) { const k = i + '-' + id; if (this.edits[k] === undefined) this.edits[k] = s.command; }
  async setPerm(v: AgentPerm) {
    this.pmenu.set(false);
    if (v === 'auto' && this.ai.perm() !== 'auto' && !(await this.ui.confirm(tr('Fully autonomous agent'),
      tr('The AI will run commands and change the configuration of the devices it connects to without asking you, using their saved logins. Production sessions still ask before every command, and reboot, reload and erase always ask. You can press Stop at any time.'), tr('Allow'), true))) return;
    const s = this.ui.settings(); if (s) void this.ui.saveSettings({ ...s, ai: { ...(s.ai ?? {}), agentPerm: v } });
  }

  paste(lines: string[]) {
    const t = this.tab(); if (!t) return;
    void this.tabs.paste(t.id, lines.filter(l => l.trim()).join('\n'));
    this.tabs.focus(t.id);
    this.ui.toast(tr('Pasted - check it, then press Enter in the terminal'), 'info');
  }
  isSignIn(e: string) { return /^Claude Code is not signed in/.test(e); }
  async copySignIn() { try { await this.copy(await this.backend.claudeCodeSignIn()); } catch (e) { this.ui.error(e); } }
  /** Signs Claude Code in where the person already is: a Local Shell tab of Termwright, with the command typed. */
  async runSignIn() {
    try {
      const cmd = await this.backend.claudeCodeSignIn();
      const before = new Set(this.tabs.tabs().map(t => t.id));
      await this.tabs.openLocalShell();
      for (let i = 0; i < 40; i++) {
        const t = this.tabs.tabs().find(x => !before.has(x.id) && x.kind === 'terminal');
        if (t?.state === 'connected') { await new Promise(r => setTimeout(r, 400)); await this.backend.sendInput(t.id, cmd + '\r'); return; }
        await new Promise(r => setTimeout(r, 250));
      }
      this.ui.toast(tr('The Local Shell did not start: run {0} in a terminal', cmd), 'error');
    } catch (e) { this.ui.error(e); }
  }
  async copy(text: string) { await this.backend.clipboardSet(text); this.ui.toast(tr('Copied'), 'success'); }
  async saveCmd(lines: string[]) {
    const s = this.ui.settings(); if (!s) return;
    const dt = this.tab()?.session?.deviceType ?? '';
    const add = lines.filter(l => l.trim()).map(c => ({ name: c.length > 48 ? c.slice(0, 47) + '…' : c, folder: dt ? dt + ' / AI' : 'AI', text: c, description: tr('Suggested by the AI assistant'), deviceTypes: dt ? [dt] : [] }));
    try { await this.ui.saveSettings({ ...s, snippets: [...(s.snippets ?? []), ...add] }); this.ui.toast(tr('Saved to the Command Library'), 'success'); }
    catch (e) { this.ui.error(e); }
  }

  // ---- size and place ----
  resize(e: MouseEvent) {
    e.preventDefault();
    const el = this.host.nativeElement as HTMLElement, r = el.getBoundingClientRect(), d = this.ai.dock();
    const mv = (ev: MouseEvent) => this.ai.setWidth(d === 'left' ? ev.clientX - r.left : d === 'float' ? ev.clientX - r.left : r.right - ev.clientX);
    const up = () => { removeEventListener('mousemove', mv); removeEventListener('mouseup', up); document.body.style.cursor = ''; };
    document.body.style.cursor = 'col-resize';
    addEventListener('mousemove', mv); addEventListener('mouseup', up);
  }
  move(e: MouseEvent) {
    if (this.ai.dock() !== 'float' || (e.target as HTMLElement).closest('button, select')) return;
    e.preventDefault();
    const sx = e.clientX - this.fx(), sy = e.clientY - this.fy();
    const mv = (ev: MouseEvent) => { this.fx.set(Math.max(0, Math.min(innerWidth - 200, ev.clientX - sx))); this.fy.set(Math.max(0, Math.min(innerHeight - 80, ev.clientY - sy))); };
    const up = () => { removeEventListener('mousemove', mv); removeEventListener('mouseup', up); };
    addEventListener('mousemove', mv); addEventListener('mouseup', up);
  }
  @HostListener('document:keydown.escape') esc() { this.menu.set(false); this.tmenu.set(false); this.pmenu.set(false); this.mmenu.set(false); }
  @HostListener('document:click', ['$event']) out(e: MouseEvent) {
    const t = e.target as HTMLElement;
    if (this.menu() && !t.closest('.addw')) this.menu.set(false);
    if (this.tmenu() && !t.closest('.toolw')) this.tmenu.set(false);
    if (this.pmenu() && !t.closest('.permw')) this.pmenu.set(false);
    if (this.mmenu() && !t.closest('.modw')) this.mmenu.set(false);
  }
}
