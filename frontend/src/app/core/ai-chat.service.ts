import { Injectable, computed, effect, inject, signal, untracked } from '@angular/core';
import { Backend, errText } from './backend';
import { Ui } from './ui.service';
import { Tabs, TabView } from './tabs.service';
import { SessionNode } from './models';
import { envInfo, envOf } from './env';
import { tr } from './i18n';

/**
 * The AI panel: chats (kept on this computer), what goes with a question
 * (selection, copied text, a whole terminal, other tabs, pictures, files), and
 * the Agent mode - the AI asks Termwright to run commands in tabs; read-only
 * ones run by themselves (never on production), the rest wait for the user.
 */

export type CtxKind = 'selection' | 'clipboard' | 'terminal' | 'tab' | 'file';
export interface Ctx { id: number; kind: CtxKind; tabId?: string; label: string; detail: string; text: string; }
export interface Img { name: string; mediaType: string; data: string; url: string; }
export type StepState = 'ask' | 'running' | 'done' | 'skipped' | 'error' | 'refused';
export interface Step { kind: 'run' | 'open'; tab: string; command: string; output?: string; state: StepState; auto?: boolean; danger?: boolean; note?: string; }
export interface ChatMsg {
  id: number; role: 'user' | 'assistant'; text: string; time: string;
  ctx?: { label: string; detail: string }[]; images?: Img[]; wire?: string; hidden?: boolean;
  model?: string; secs?: number; steps?: Step[]; error?: string; usage?: { in: number; out: number } | null;
}
export interface Chat { id: string; title: string; device: string; updated: string; msgs: ChatMsg[]; }
export type Dock = 'right' | 'left' | 'float';

const KEY_CHATS = 'tw.ai.chats', KEY_DOCK = 'tw.ai.dock', KEY_W = 'tw.ai.width', KEY_TOOLS = 'tw.ai.tools';

/** What the agent may do. */
export const AGENT_TOOLS = [
  { k: 'run', name: 'Run commands in tabs', what: 'Types a command in an open tab and reads the output' },
  { k: 'open', name: 'Connect to saved sessions', what: 'Opens a saved session in a new tab' },
] as const;
const MAX_CHATS = 40, MAX_OUT = 12000, MAX_STEPS = 10;

const NET_RO = ['show', 'sh', 'display', 'dis', 'ping', 'traceroute', 'tracert', 'trace'];
const LINUX_RO = ['ls', 'cat', 'head', 'tail', 'grep', 'ip a', 'ip addr', 'ip r', 'ip route', 'ip link', 'ip neigh', 'ifconfig', 'ping', 'traceroute', 'tracepath',
  'uname', 'uptime', 'df', 'du', 'free', 'ps', 'top -b -n 1', 'ss', 'netstat', 'journalctl', 'systemctl status', 'dmesg', 'hostname', 'whoami', 'id', 'date', 'lsblk', 'lscpu', 'ethtool', 'nslookup', 'dig', 'host', 'pwd', 'env', 'which'];

/** The commands the agent may run without asking, for a device type (own list first). */
export function readOnlyList(deviceType: string, own: string | undefined): string[] {
  const l = (own ?? '').split('\n').map(x => x.trim()).filter(Boolean);
  if (l.length) return l;
  return /linux|unix|ubuntu|debian|centos|rhel|server|bsd|mac/i.test(deviceType) ? LINUX_RO : NET_RO;
}

const SAFE_PIPE = /^(include|exclude|begin|section|count|grep|egrep|head|tail|wc|sort|uniq|no-more|match|except|find|i|e|b)\b/i;

/** Read-only by the list: no chaining or redirection, pipes only to filters. */
export function isReadOnly(cmd: string, list: string[]): boolean {
  const c = cmd.trim();
  if (!c || /[;`]|&&|\|\||\$\(|>|</.test(c)) return false;
  const [head, ...pipes] = c.split('|');
  if (pipes.some(p => !SAFE_PIPE.test(p.trim()))) return false;
  const h = head.trim().toLowerCase();
  return list.some(x => { const w = x.toLowerCase(); return h === w || h.startsWith(w + ' '); });
}

export const RISKY = /^\s*(conf(ig(ure)?)?\b|unconfigure|delete|del\b|erase|clear\b|reboot|reload|restart|shutdown|no\s|write\b|copy\b|save\b|disable\b|enable\s+ports?\b|format|rm\s|kill|halt|poweroff|set\s|unset\s|download|upload|install|upgrade|reset|sudo\b)/i;

let nid = 0;
const now = () => new Date().toISOString();

@Injectable({ providedIn: 'root' })
export class AIChat {
  private backend = inject(Backend);
  private ui = inject(Ui);
  private tabs = inject(Tabs);

  readonly open = signal(false);
  readonly dock = signal<Dock>((store(KEY_DOCK) as Dock) || 'right');
  readonly width = signal(Number(store(KEY_W)) || 440);
  readonly chats = signal<Chat[]>(loadChats());
  readonly currentId = signal<string>('');
  readonly view = signal<'chat' | 'history'>('chat');
  readonly busy = signal(false);
  readonly mode = signal<'ask' | 'agent'>('ask');
  readonly ctxs = signal<Ctx[]>([]);
  readonly images = signal<Img[]>([]);
  readonly keep = signal(0); // whole terminal: the last N lines (0 = all)
  readonly error = signal('');
  readonly tools = signal<Record<string, boolean>>((() => { try { return { run: true, open: true, ...JSON.parse(store(KEY_TOOLS) || '{}') }; } catch { return { run: true, open: true }; } })());
  setTool(k: string, on: boolean) { this.tools.update(t => ({ ...t, [k]: on })); save(KEY_TOOLS, JSON.stringify(this.tools())); }
  /** The tokens this chat used so far, as the AI service counted them. */
  readonly used = computed(() => (this.current()?.msgs ?? []).reduce((a, m) => ({ in: a.in + (m.usage?.in ?? 0), out: a.out + (m.usage?.out ?? 0) }), { in: 0, out: 0 }));
  /** About how many tokens the next question sends: the chat so far, what goes with it, and the question. */
  estimate(typed: string) {
    const hist = (this.current()?.msgs ?? []).reduce((n, m) => n + (m.wire ?? m.text).length, 0);
    return Math.ceil((hist + this.ctxs().reduce((n, c) => n + this.ctxTextLen(c), 0) + typed.length) / 4) + this.images().length * 1500;
  }
  private stopping = false;
  /** Approvals waiting for the user, by step. */
  private waits = new Map<Step, (r: { go: 'run' | 'skip' | 'stop'; command?: string }) => void>();

  readonly current = computed(() => this.chats().find(c => c.id === this.currentId()) ?? null);

  constructor() {
    // right-click → Explain with AI / Ask AI for a Command open the panel
    effect(() => {
      const r = this.ui.aiReq();
      if (!r) return;
      untracked(() => {
        this.ui.aiReq.set(null);
        if (!this.busy() && (this.current()?.msgs.length ?? 0) > 0 && r.text) this.currentId.set(''); // a new selection: a new chat
        this.start({ text: r.text, tabId: r.tabId, suggest: r.kind === 'suggest' });
      });
    });
  }
  readonly perm = computed(() => this.ui.settings()?.ai?.agentPerm ?? '');

  // ---- panel ----
  show(on = true) { this.open.set(on); }
  setDock(d: Dock) { this.dock.set(d); save(KEY_DOCK, d); }
  setWidth(w: number) { const v = Math.max(320, Math.min(900, Math.round(w))); this.width.set(v); save(KEY_W, String(v)); }

  /** Opens the panel for the active tab: the selection, else the whole terminal. */
  start(opts: { text?: string; tabId?: string; suggest?: boolean } = {}) {
    this.show(true);
    this.view.set('chat');
    const t = (opts.tabId ? this.tabs.get(opts.tabId) : this.tabs.active()) ?? null;
    const term = t && t.kind === 'terminal' ? t : null;
    const l: Ctx[] = [];
    const sel = (opts.text || (term ? this.tabs.selectionText(term.id) : '')).trimEnd();
    if (sel && term) l.push(this.mkCtx('selection', term, sel));
    else if (term && !opts.suggest) { const all = this.tabs.bufferText(term.id); if (all.trim()) l.push(this.mkCtx('terminal', term, all)); }
    if (!this.busy()) this.ctxs.set(l);
  }

  newChat() { if (this.busy()) return; this.currentId.set(''); this.view.set('chat'); this.error.set(''); this.start(); }
  openChat(id: string) { if (this.busy()) return; this.currentId.set(id); this.view.set('chat'); }
  deleteChat(id: string) { this.chats.update(l => l.filter(c => c.id !== id)); if (this.currentId() === id) this.currentId.set(''); this.persist(); }

  // ---- context ----
  private mkCtx(kind: CtxKind, t: TabView | null, text: string, label?: string): Ctx {
    const lines = text ? text.split('\n').length : 0;
    const what = kind === 'selection' ? tr('selection') : kind === 'clipboard' ? tr('copied text') : kind === 'file' ? tr('file') : tr('whole terminal');
    return { id: ++nid, kind, tabId: t?.id, label: label ?? t?.title ?? tr('Clipboard'), detail: `${what} · ${tr('{0} lines', lines.toLocaleString())}`, text };
  }
  addSelection() { const t = this.termTab(); if (!t) return; const s = this.tabs.selectionText(t.id).trimEnd(); if (s) this.addCtx(this.mkCtx('selection', t, s)); else this.ui.toast(tr('Select output in the terminal first'), 'info'); }
  addTerminal(id?: string) { const t = id ? this.tabs.get(id) : this.termTab(); if (!t) return; this.addCtx(this.mkCtx(id ? 'tab' : 'terminal', t, this.tabs.bufferText(t.id))); }
  async addClipboard() {
    const c = ((await this.backend.clipboardGet().catch(() => '')) ?? '').trimEnd();
    if (c) this.addCtx(this.mkCtx('clipboard', null, c, tr('Clipboard'))); else this.ui.toast(tr('The clipboard is empty'), 'info');
  }
  addFile(name: string, text: string) { this.addCtx({ ...this.mkCtx('file', null, text, name) }); }
  addCtx(c: Ctx) { this.ctxs.update(l => [...l.filter(x => !(x.kind === c.kind && x.tabId && x.tabId === c.tabId)), c]); }
  removeCtx(id: number) { this.ctxs.update(l => l.filter(x => x.id !== id)); }
  addImage(i: Img) { if (this.images().length >= 4) { this.ui.toast(tr('Up to 4 pictures in a message'), 'info'); return; } this.images.update(l => [...l, i]); }
  removeImage(i: Img) { this.images.update(l => l.filter(x => x !== i)); }

  private termTab() { const t = this.tabs.active(); return t && t.kind === 'terminal' ? t : null; }

  /** The text that goes with the question, the whole terminal cut to `keep` lines. */
  private ctxText(c: Ctx) {
    if ((c.kind === 'terminal' || c.kind === 'tab') && this.keep() > 0) { const l = c.text.split('\n'); return l.slice(-this.keep()).join('\n'); }
    return c.text;
  }
  readonly tokens = computed(() => Math.ceil(this.ctxs().reduce((n, c) => n + this.ctxTextLen(c), 0) / 4));
  private ctxTextLen(c: Ctx) { return this.ctxText(c).length; }
  readonly heavy = computed(() => this.tokens() > 4000 || this.ctxs().some(c => (c.kind === 'terminal' || c.kind === 'tab') && this.ctxTextLen(c) > 6000));

  // ---- asking ----
  async send(question: string) {
    const q = question.trim();
    if (this.busy() || (!q && !this.ctxs().length && !this.images().length)) return;
    this.error.set('');
    let chat = this.current();
    if (!chat) {
      const t = this.termTab();
      chat = { id: 'c' + Date.now().toString(36), title: (q || tr('Explain the output')).slice(0, 80), device: t?.title ?? '', updated: now(), msgs: [] };
      this.chats.update(l => [chat!, ...l].slice(0, MAX_CHATS));
      this.currentId.set(chat.id);
    }
    const ctxs = this.ctxs(), imgs = this.images();
    const parts = ctxs.map(c => `Output from ${c.label} (${c.kind === 'selection' ? 'selected' : c.kind === 'clipboard' ? 'copied text' : c.kind === 'file' ? 'a file' : 'the whole terminal'}):\n\`\`\`\n${this.ctxText(c)}\n\`\`\``);
    const ask = q || (imgs.length ? tr('What does this show? Is something wrong?') : tr('Explain this output: what it means, whether something is wrong, and what to check next.'));
    this.push(chat.id, { id: ++nid, role: 'user', text: q || ask, time: now(), ctx: ctxs.map(c => ({ label: c.label, detail: c.detail })), images: imgs, wire: [ask, ...parts].join('\n\n') });
    this.ctxs.set([]); this.images.set([]);
    await this.run(chat.id);
  }

  stop() { this.stopping = true; for (const [, r] of this.waits) r({ go: 'stop' }); }

  /** Answers an approval card. */
  decide(s: Step, go: 'run' | 'skip' | 'stop', command?: string) { this.waits.get(s)?.({ go, command }); }

  private async run(chatId: string) {
    this.busy.set(true); this.stopping = false;
    const agent = this.mode() === 'agent';
    try {
      for (let step = 0; step < MAX_STEPS; step++) {
        const chat = this.chats().find(c => c.id === chatId);
        if (!chat || this.stopping) break;
        const t = this.termTab() ?? this.tabs.tabs().find(x => x.kind === 'terminal') ?? null;
        const started = Date.now();
        const ans = await this.backend.aiChat({
          messages: chat.msgs.filter(m => !m.error).map(m => ({ role: m.role, text: m.wire ?? m.text, images: (m.images ?? []).map(i => ({ name: i.name, mediaType: i.mediaType, data: i.data })) })),
          deviceType: t?.session?.deviceType ?? '', protocol: t?.session?.protocol ?? '', agent,
          tabs: agent ? this.tabs.tabs().filter(x => x.kind === 'terminal').map(x => `${x.title} (${x.session.deviceType || 'unknown'}, ${x.state})`) : [],
          tools: Object.entries(this.tools()).filter(([, on]) => on).map(([k]) => k).concat(['none']),
        });
        const { text, actions } = parseActions(ans.text);
        const msg: ChatMsg = { id: ++nid, role: 'assistant', text, time: now(), wire: ans.text, model: ans.model, secs: Math.round((Date.now() - started) / 1000), steps: [], usage: ans.usage ?? null };
        this.push(chatId, msg);
        if (!agent || !actions.length || this.stopping) break;
        const results: string[] = [];
        for (const a of actions) {
          if (this.stopping) break;
          if (!this.tools()[a.kind]) { results.push(`${a.kind} ${a.tab}: not done - the user turned this tool off.`); continue; }
          const s: Step = { kind: a.kind, tab: a.tab, command: a.command, state: 'running' };
          msg.steps!.push(s); this.touch(chatId);
          results.push(await this.act(s, chatId));
          this.touch(chatId);
        }
        if (this.stopping) { results.push('The user stopped the agent.'); }
        this.push(chatId, { id: ++nid, role: 'user', text: '', hidden: true, time: now(), wire: results.join('\n\n') });
        if (this.stopping) break;
      }
    } catch (e) {
      this.push(chatId, { id: ++nid, role: 'assistant', text: '', time: now(), error: errText(e) });
    } finally {
      this.busy.set(false); this.stopping = false; this.waits.clear(); this.persist();
    }
  }

  /** One agent step: open a session or run a command, asking when the rules say so. */
  private async act(s: Step, chatId: string): Promise<string> {
    const perm = this.perm();
    if (s.kind === 'open') {
      const node = findSession(this.ui.tree(), s.tab);
      if (!node) { s.state = 'error'; s.note = tr('No saved session is called {0}', s.tab); return `open ${s.tab}: no saved session has that name.`; }
      s.tab = node.name;
      const prod = !!envInfo(envOf(node.path, node.environment, this.ui.settings()?.folderDefaults))?.protect;
      if (perm === 'every' || prod) { const r = await this.ask(s, chatId); if (r.go !== 'run') return this.refusal(s, r.go); }
      s.state = 'running'; s.auto = perm !== 'every' && !prod; this.touch(chatId);
      await this.tabs.openSession(node.path);
      const t = await waitFor(() => this.tabs.tabs().find(x => x.session?.path === node.path && x.state === 'connected'), 30000);
      if (!t) { s.state = 'error'; s.note = tr('Did not connect within 30 seconds'); return `open ${node.name}: did not connect within 30 seconds.`; }
      s.state = 'done'; s.output = tr('Connected'); return `open ${node.name}: connected; the tab is called ${t.title}.`;
    }
    const t = findTab(this.tabs.tabs(), s.tab);
    if (!t) { s.state = 'error'; s.note = tr('No open tab is called {0}', s.tab); return `run ${s.tab}: no open tab has that name - open it first.`; }
    s.tab = t.title;
    if (t.state !== 'connected') { s.state = 'error'; s.note = tr('The tab is not connected'); return `run ${t.title}: the tab is not connected.`; }
    const dt = (this.ui.settings()?.deviceTypes ?? []).find(d => d.name.toLowerCase() === (t.session.deviceType ?? '').toLowerCase());
    const ro = isReadOnly(s.command, readOnlyList(t.session.deviceType ?? '', dt?.aiReadOnly));
    const prod = !!envInfo(envOf(t.session.path, t.session.environment, this.ui.settings()?.folderDefaults))?.protect;
    s.danger = !ro && RISKY.test(s.command);
    if (perm === 'readonly' && !ro) { s.state = 'refused'; s.note = tr('Read-only mode: not run'); return `run ${t.title}: ${s.command} - not run: the user allows only read-only commands. Tell the user the command instead.`; }
    if (!ro || prod || perm === 'every') {
      const r = await this.ask(s, chatId);
      if (r.go !== 'run') return this.refusal(s, r.go);
      if (r.command && r.command.trim() !== s.command) s.command = r.command.trim();
    } else s.auto = true;
    s.state = 'running'; this.touch(chatId);
    const out = await this.runIn(t.id, s.command);
    s.output = out; s.state = 'done';
    const paging = /(--more--|press any key|<--- more --->|q to quit)\s*$/i.test(out.trim());
    return `run ${t.title}: ${s.command}\n\`\`\`\n${out.length > MAX_OUT ? '…\n' + out.slice(-MAX_OUT) : out}\n\`\`\`${paging ? '\n(The device is paging: turn paging off and run it again.)' : ''}`;
  }

  private ask(s: Step, chatId: string) {
    s.state = 'ask'; this.touch(chatId);
    return new Promise<{ go: 'run' | 'skip' | 'stop'; command?: string }>(res => this.waits.set(s, r => { this.waits.delete(s); res(r); }));
  }

  private refusal(s: Step, go: 'skip' | 'stop' | 'run') {
    if (go === 'stop') { this.stopping = true; s.state = 'skipped'; s.note = tr('Stopped'); return `${s.kind} ${s.tab}: ${s.command} - the user stopped the agent.`; }
    s.state = 'skipped'; s.note = tr('Skipped'); return `${s.kind} ${s.tab}: ${s.command} - the user chose not to run it.`;
  }

  /** Types a command in a tab and returns the output once it settles. */
  private async runIn(id: string, cmd: string) {
    const before = this.tabs.bufferText(id).split('\n').length;
    await this.backend.sendInput(id, cmd + '\r');
    let last = '', changed = Date.now();
    const start = Date.now();
    while (Date.now() - start < 30000 && !this.stopping) {
      await sleep(400);
      const cur = this.tabs.bufferText(id);
      if (cur !== last) { last = cur; changed = Date.now(); continue; }
      if (Date.now() - changed > 1500 && Date.now() - start > 1000) break;
    }
    const lines = last.split('\n');
    const n = Math.min(lines.length, Math.max(1, lines.length - before + 1));
    return lines.slice(-n).join('\n');
  }

  // ---- storage ----
  private push(chatId: string, m: ChatMsg) {
    this.chats.update(l => l.map(c => c.id === chatId ? { ...c, updated: now(), msgs: [...c.msgs, m] } : c));
    this.persist();
  }
  private touch(chatId: string) { this.chats.update(l => l.map(c => c.id === chatId ? { ...c, msgs: [...c.msgs] } : c)); }
  /** Chats are kept on this computer; the device output that went with them is not. */
  persist() {
    const keep = this.chats().slice(0, MAX_CHATS).map(c => ({ ...c, msgs: c.msgs.map(m => ({ ...m, wire: m.role === 'assistant' ? m.wire : undefined, images: (m.images ?? []).map(i => ({ ...i, data: '', url: '' })) })) }));
    save(KEY_CHATS, JSON.stringify(keep));
  }
}

/** The answer's text without its termwright block, and the actions in it. */
export function parseActions(text: string): { text: string; actions: { kind: 'run' | 'open'; tab: string; command: string }[] } {
  const actions: { kind: 'run' | 'open'; tab: string; command: string }[] = [];
  const out = text.replace(/```termwright[^\n]*\n([\s\S]*?)```/g, (_, body: string) => {
    for (const raw of body.split('\n')) {
      const l = raw.trim();
      let m = /^run\s+(.+?)\s*:\s*(.+)$/i.exec(l);
      if (m) { actions.push({ kind: 'run', tab: m[1].trim(), command: m[2].trim() }); continue; }
      m = /^open\s+(.+)$/i.exec(l);
      if (m) actions.push({ kind: 'open', tab: m[1].trim(), command: '' });
    }
    return '';
  });
  return { text: out.trim(), actions };
}

function findTab(tabs: TabView[], name: string) {
  const n = name.trim().toLowerCase();
  const terms = tabs.filter(t => t.kind === 'terminal');
  return terms.find(t => t.title.toLowerCase() === n) ?? terms.find(t => t.session?.name?.toLowerCase() === n) ?? terms.find(t => t.session?.path?.toLowerCase() === n) ?? null;
}

function findSession(root: SessionNode | null, name: string): SessionNode | null {
  const n = name.trim().toLowerCase();
  let hit: SessionNode | null = null;
  const walk = (x: SessionNode | null) => {
    if (!x || hit) return;
    if (!x.isFolder && (x.name.toLowerCase() === n || x.path.toLowerCase() === n)) { hit = x; return; }
    (x.children ?? []).forEach(walk);
  };
  walk(root);
  return hit;
}

async function waitFor<T>(f: () => T | undefined | null, ms: number): Promise<T | null> {
  const end = Date.now() + ms;
  while (Date.now() < end) { const v = f(); if (v) return v; await sleep(400); }
  return null;
}
const sleep = (ms: number) => new Promise(r => setTimeout(r, ms));
function store(k: string) { try { return localStorage.getItem(k) ?? ''; } catch { return ''; } }
function save(k: string, v: string) { try { localStorage.setItem(k, v); } catch { /* full or blocked */ } }
function loadChats(): Chat[] { try { const v = JSON.parse(localStorage.getItem(KEY_CHATS) ?? '[]'); return Array.isArray(v) ? v : []; } catch { return []; } }
