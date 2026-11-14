import { Injectable, computed, effect, inject, signal, untracked } from '@angular/core';
import { Backend, errText } from './backend';
import { Ui } from './ui.service';
import { Tabs, TabView } from './tabs.service';
import { LabView, SessionNode } from './models';
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
export type StepKind = 'run' | 'open' | 'find' | 'log' | 'record' | 'lab';
export interface Step { kind: StepKind; tab: string; command: string; output?: string; state: StepState; auto?: boolean; danger?: boolean; note?: string; failed?: boolean; prod?: boolean; ask?: string; }
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
  { k: 'open', name: 'Connect to saved sessions', what: 'Finds saved sessions by folder, name or address and connects with their saved login' },
  { k: 'log', name: 'Logger', what: 'Logs a tab to a file in the log folder - also in Read only' },
  { k: 'record', name: 'Recorder', what: 'Records a tab with a marker for each command - also in Read only' },
  { k: 'lab', name: 'Emulated lab', what: 'Sees the emulated lab (switches, cables, LAGs, KVM hosts, events), adds and starts switches, cables, configures them on their consoles, runs traffic tests, captures packets and checks links' },
] as const;
const MAX_CHATS = 40, MAX_OUT = 12000, MAX_STEPS = 25;
/** lab lines with words; the connector's lab tools take JSON (lab traffic {...}). */
const LAB_VERBS = ['start', 'stop', 'shutdown', 'cable', 'uncable', 'pull', 'plug', 'lag', 'nolag', 'readlags', 'setlag', 'unsetlag', 'checkcabling', 'uplink'];
const LAB_TOOLS = ['images', 'add_switches', 'cable', 'power', 'wait', 'console', 'check_cabling', 'move', 'traffic', 'capture', 'link_check', 'qemu_command', 'address', 'computer', 'cpu', 'debug', 'events'];

/** What the agent may do on its own, from "Read only" (white) to fully autonomous (orange). */
export type AgentPerm = '' | 'every' | 'readonly' | 'auto';
export const AGENT_PERMS: { k: AgentPerm; name: string; what: string; tone: 'auto' | 'ask' | 'ro' }[] = [
  { k: 'auto', name: 'Fully autonomous', what: 'Runs commands and changes without asking. Production sessions and reboot / erase still ask.', tone: 'auto' },
  { k: '', name: 'Changes ask first', what: 'Read-only commands run by themselves; changes wait for you.', tone: 'ask' },
  { k: 'every', name: 'Ask before every command', what: 'Every command and connection waits for you.', tone: 'ask' },
  { k: 'readonly', name: 'Read only', what: 'Read-only commands run without asking, production sessions too; changes are given to you as text.', tone: 'ro' },
];

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

/** Asks even in fully autonomous mode: the device restarts, or loses its configuration or files. */
export const DESTRUCTIVE = /^\s*(reboot|reload|halt|poweroff|shutdown\s*(-h|-r|now)|erase|format|write\s+erase|unconfigure\s+switch|delete\s+(flash|config|startup|nvram|bootflash|\S*\.cfg|\S*\.xsf)|clear\s+config|factory|rm\s+-rf|mkfs|dd\s)/i;
/** A device's answer that says the command failed. */
const FAILED = /(^|\n)\s*(%\s*(invalid|incomplete|ambiguous|unknown|error)|error:|invalid input|syntax error|command not found|unrecognized command|\^\s*$)/i;

export const RISKY = /^\s*(conf(ig(ure)?)?\b|unconfigure|delete|del\b|erase|clear\b|reboot|reload|restart|shutdown|no\s|write\b|copy\b|save\b|disable\b|enable\s+ports?\b|format|rm\s|kill|halt|poweroff|set\s|unset\s|download|upload|install|upgrade|reset|sudo\b)/i;

/** "claude-sonnet-5-5" → "Sonnet 5.5", "sonnet" → "Sonnet"; other names as they are. */
export function nice(m: string): string {
  const cap = (w: string) => w.charAt(0).toUpperCase() + w.slice(1);
  const c = /^(?:claude-)?(opus|sonnet|haiku|fable)(?:-(\d+)(?:-(\d+))?)?(?:-\d{8})?$/i.exec(m.trim());
  if (c) return cap(c[1].toLowerCase()) + (c[2] ? ' ' + c[2] + (c[3] ? '.' + c[3] : '') : '');
  return m;
}

/** Claude Code's effort levels ("" = the model's default). */
export const EFFORTS: { k: string; name: string }[] = [
  { k: '', name: 'Default' }, { k: 'low', name: 'Low' }, { k: 'medium', name: 'Medium' }, { k: 'high', name: 'High' }, { k: 'xhigh', name: 'Extra high' }, { k: 'max', name: 'Max' },
];

/** The model list for a service: the newest of each family (Claude Code's short names) first, then the versions. */
export function modelChoices(provider: string, models: string[]): { value: string; label: string; hint: string; group: string }[] {
  const ids = models.filter(m => /^claude-/.test(m));
  return models.map(m => {
    if (/^(opus|sonnet|haiku|fable)$/.test(m)) {
      const newest = ids.find(id => id.startsWith('claude-' + m + '-')) ?? '';
      return { value: m, label: (newest ? nice(newest) : nice(m)) + ' · ' + tr('newest'), hint: m, group: provider === 'claudecode' ? tr('Newest') : '' };
    }
    return { value: m, label: nice(m), hint: nice(m).toLowerCase() !== m.toLowerCase() ? m : '', group: provider === 'claudecode' ? tr('Versions') : '' };
  });
}

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
  /** Kept in the data folder (ai-chats.json), so backups include them. */
  readonly chats = signal<Chat[]>([]);
  private loaded = false;
  readonly currentId = signal<string>('');
  readonly view = signal<'chat' | 'history'>('chat');
  readonly busy = signal(false);
  readonly mode = signal<'ask' | 'agent'>('ask');
  readonly ctxs = signal<Ctx[]>([]);
  readonly images = signal<Img[]>([]);
  readonly keep = signal(0); // whole terminal: the last N lines (0 = all)
  readonly error = signal('');
  /** The agent tools that are on - a setting (AI → agentToolsOff), so it is backed up, exported and synced. */
  readonly tools = computed<Record<string, boolean>>(() => {
    const off = new Set(this.ui.settings()?.ai?.agentToolsOff ?? []);
    return Object.fromEntries(AGENT_TOOLS.map(t => [t.k, !off.has(t.k)]));
  });
  setTool(k: string, on: boolean) {
    const s = this.ui.settings(); if (!s) return;
    const off = new Set(s.ai?.agentToolsOff ?? []);
    if (on) off.delete(k); else off.add(k);
    void this.ui.saveSettings({ ...s, ai: { ...(s.ai ?? {}), agentToolsOff: [...off] } });
  }
  /** The tokens this chat used so far, as the AI service counted them. */
  readonly used = computed(() => (this.current()?.msgs ?? []).reduce((a, m) => ({ in: a.in + (m.usage?.in ?? 0), out: a.out + (m.usage?.out ?? 0) }), { in: 0, out: 0 }));
  /** About how many tokens the next question sends: the chat so far, what goes with it, and the question. */
  estimate(typed: string) {
    const hist = (this.current()?.msgs ?? []).reduce((n, m) => n + (m.wire ?? m.text).length, 0);
    return Math.ceil((hist + this.ctxs().reduce((n, c) => n + this.ctxTextLen(c), 0) + typed.length) / 4) + this.images().length * 1500;
  }
  private stopping = false;
  /** Approvals waiting for the user, by step. */
  private waits = new Map<Step, (r: { go: 'run' | 'skip' | 'stop' | 'chat'; command?: string }) => void>();
  /** Chats where the user allowed every lab change (not the dangerous ones) without asking. */

  readonly current = computed(() => this.chats().find(c => c.id === this.currentId()) ?? null);

  constructor() {
    void this.loadChats();
    // the agent tools choice was kept in the browser before 0.33: move it into the settings once
    effect(() => {
      const st = this.ui.settings(); if (!st) return;
      untracked(() => {
        const old = store(KEY_TOOLS); if (!old) return;
        try {
          const t = JSON.parse(old) as Record<string, boolean>;
          const off = Object.entries(t).filter(([, on]) => on === false).map(([k]) => k);
          if (off.length && !(st.ai?.agentToolsOff ?? []).length) void this.ui.saveSettings({ ...st, ai: { ...(st.ai ?? {}), agentToolsOff: off } });
        } catch { /* ignore */ }
        drop(KEY_TOOLS);
      });
    });
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
  readonly perm = computed<AgentPerm>(() => this.ui.settings()?.ai?.agentPerm ?? '');

  // ---- panel ----
  show(on = true) { this.open.set(on); }
  setDock(d: Dock) { this.dock.set(d); save(KEY_DOCK, d); this.persist(); }
  setWidth(w: number) { const v = Math.max(320, Math.min(900, Math.round(w))); this.width.set(v); save(KEY_W, String(v)); this.persist(); }

  /** Reads the chats from the data folder; chats kept in the browser by an earlier version move there once. */
  private async loadChats() {
    try {
      const raw = await this.backend.aiChatsLoad();
      if (raw) {
        const v = JSON.parse(raw) as { chats?: Chat[]; dock?: Dock; width?: number };
        if (Array.isArray(v.chats)) this.chats.set([...this.chats(), ...v.chats.filter(c => !this.chats().some(x => x.id === c.id))]);
        if (v.dock) this.dock.set(v.dock);
        if (v.width) this.width.set(v.width);
      } else {
        const old = browserChats();
        if (old.length) { this.chats.set(old); this.loaded = true; this.persist(); }
      }
      drop(KEY_CHATS);
    } catch { this.chats.set(browserChats()); }
    this.loaded = true;
  }

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
  decide(s: Step, go: 'run' | 'skip' | 'stop' | 'chat', command?: string) { this.waits.get(s)?.({ go, command }); }

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
          folders: agent ? folders(this.ui.tree()) : [], perm: this.perm(),
        });
        const { text, actions } = parseActions(ans.text);
        const msg: ChatMsg = { id: ++nid, role: 'assistant', text, time: now(), wire: ans.text, model: ans.model, secs: Math.round((Date.now() - started) / 1000), steps: [], usage: ans.usage ?? null };
        this.push(chatId, msg);
        if (!agent || !actions.length || this.stopping) break;
        const results: string[] = [];
        for (const a of actions) {
          if (this.stopping) break;
          if (!this.tools()[a.kind === 'find' ? 'open' : a.kind]) { results.push(`${a.kind} ${a.tab}: not done - the user turned this tool off.`); continue; }
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
    if (s.kind === 'find') {
      const hits = findSessions(this.ui.tree(), s.tab);
      s.state = 'done'; s.auto = true;
      s.output = hits.length ? hits.slice(0, 60).map(h => `${h.path}  ${h.hostname ?? ''}  ${h.deviceType ?? ''}`).join('\n') : tr('No saved session matches');
      s.note = tr('{0} found', hits.length);
      return `find ${s.tab}: ` + (hits.length ? `${hits.length} saved session(s) - path | address | device type | environment:\n` + hits.slice(0, 60).map(h => `${h.path} | ${h.hostname ?? ''} | ${h.deviceType || 'unknown'} | ${this.isProd(h.path, h.environment) ? 'production' : 'not production'}`).join('\n') + (hits.length > 60 ? `\n… and ${hits.length - 60} more - narrow the search.` : '') : 'no saved session matches.');
    }
    if (s.kind === 'open') {
      const found = resolveSession(this.ui.tree(), s.tab);
      if (!found.node) { s.state = 'error'; s.note = found.many ? tr('{0} sessions match - be more exact', found.many) : tr('No saved session is called {0}', s.tab); return `open ${s.tab}: ` + (found.many ? `${found.many} saved sessions match - use the folder/name path from find.` : 'no saved session has that name, path or address - use find.'); }
      const node = found.node;
      s.tab = node.name;
      const prod = this.isProd(node.path, node.environment);
      const open = this.tabs.tabs().find(x => x.kind === 'terminal' && x.session?.path === node.path && x.state === 'connected');
      if (open) { s.state = 'done'; s.auto = true; s.output = tr('Already connected'); return `open ${node.name}: already connected; the tab is called ${open.title}.`; }
      s.prod = prod;
      // Read only connects without asking: it then runs only commands that change nothing
      const askOpen = perm !== 'readonly' && (perm === 'every' || prod);
      if (askOpen) { const r = await this.ask(s, chatId); if (r.go !== 'run') return this.refusal(s, r.go); }
      s.state = 'running'; s.auto = !askOpen; this.touch(chatId);
      await this.tabs.openSession(node.path);
      const before = new Set(this.tabs.tabs().map(x => x.id));
      const t = await waitFor(() => this.tabs.tabs().find(x => x.session?.path === node.path && (x.state === 'connected' || (!before.has(x.id) && (x.state === 'error' || x.state === 'disconnected')))), 60000);
      if (!t || t.state !== 'connected') {
        s.state = 'error'; s.note = t?.message ? t.message.slice(0, 160) : tr('Did not connect within a minute');
        return `open ${node.name}: not connected - ${t?.message || 'no answer within a minute (no saved login, or the device does not answer)'}. Tell the user; do not retry the same session.`;
      }
      await sleep(1200); // the banner and the prompt
      s.state = 'done'; s.output = tr('Connected'); return `open ${node.name}: connected; the tab is called ${t.title}.`;
    }
    if (s.kind === 'log' || s.kind === 'record') return this.evidence(s);
    if (s.kind === 'lab') return this.labStep(s, chatId);
    const t = findTab(this.tabs.tabs(), s.tab);
    if (!t) { s.state = 'error'; s.note = tr('No open tab is called {0}', s.tab); return `run ${s.tab}: no open tab has that name - open it first.`; }
    s.tab = t.title;
    if (t.state !== 'connected') { s.state = 'error'; s.note = tr('The tab is not connected'); return `run ${t.title}: the tab is not connected.`; }
    const dt = (this.ui.settings()?.deviceTypes ?? []).find(d => d.name.toLowerCase() === (t.session.deviceType ?? '').toLowerCase());
    const ro = isReadOnly(s.command, readOnlyList(t.session.deviceType ?? '', dt?.aiReadOnly));
    const prod = !!envInfo(envOf(t.session.path, t.session.environment, this.ui.settings()?.folderDefaults))?.protect;
    s.danger = !ro && RISKY.test(s.command);
    const destroy = DESTRUCTIVE.test(s.command);
    s.prod = prod; s.danger = s.danger || destroy;
    if (perm === 'readonly' && !ro) { s.state = 'refused'; s.note = tr('Read-only mode: not run'); return `run ${t.title}: ${s.command} - not run: the user allows only read-only commands. Tell the user the command instead.`; }
    // Read only never asks: what is left changes nothing (production sessions too)
    if (perm === 'readonly' ? false : perm === 'auto' ? (prod || destroy) : (!ro || prod || perm === 'every')) {
      const r = await this.ask(s, chatId);
      if (r.go !== 'run') return this.refusal(s, r.go);
      if (r.command && r.command.trim() !== s.command) s.command = r.command.trim();
    } else s.auto = true;
    s.state = 'running'; this.touch(chatId);
    if (t.castFile) void this.backend.addRecordingMarker(t.id, 'AI: ' + s.command).catch(() => {});
    const out = await this.runIn(t.id, s.command);
    s.output = out; s.state = 'done';
    if (FAILED.test(out.split('\n').slice(1).join('\n'))) { s.failed = true; s.note = tr('The device reported an error'); }
    const paging = /(--more--|press any key|<--- more --->|q to quit)\s*$/i.test(out.trim());
    return `run ${t.title}: ${s.command}\n\`\`\`\n${out.length > MAX_OUT ? '…\n' + out.slice(-MAX_OUT) : out}\n\`\`\`${paging ? '\n(The device is paging: turn paging off and run it again.)' : ''}`;
  }

  private isProd(path: string, env?: string) { return !!envInfo(envOf(path, env, this.ui.settings()?.folderDefaults))?.protect; }

  /** Logger and recorder: local files only, so never asked - also in Read only. */
  private async evidence(s: Step): Promise<string> {
    const t = findTab(this.tabs.tabs(), s.tab);
    if (!t) { s.state = 'error'; s.note = tr('No open tab is called {0}', s.tab); return `${s.kind} ${s.tab}: no open tab has that name.`; }
    s.tab = t.title; s.auto = true;
    const c = s.command.trim().toLowerCase();
    try {
      if (s.kind === 'log') {
        if (c === 'off') { if (t.logFile) await this.backend.stopLog(t.id); s.state = 'done'; s.output = t.logFile || ''; return `log ${t.title}: off` + (t.logFile ? ` - saved to ${t.logFile}` : ' (it was not logging)') + '.'; }
        if (t.logFile) { s.state = 'done'; s.output = t.logFile; return `log ${t.title}: already logging to ${t.logFile}.`; }
        const dir = (this.ui.settings()?.logDir ?? '').replace(/[\\/]+$/, '');
        const p = await this.backend.startLog(t.id, dir ? `${dir}/AI-%S-%Y%M%D-%h%m%s.log` : '', true, false);
        s.state = 'done'; s.output = p; return `log ${t.title}: on - ${p}.`;
      }
      const mark = /^mark\b\s*(.*)$/i.exec(s.command.trim());
      if (mark) {
        if (!t.castFile) { s.state = 'error'; s.note = tr('Not recording'); return `record ${t.title}: mark - not recording; turn record on first.`; }
        await this.backend.addRecordingMarker(t.id, mark[1] || 'Marker'); s.state = 'done'; s.output = mark[1]; return `record ${t.title}: marker added.`;
      }
      if (c === 'off') { const f = t.castFile; if (f) await this.tabs.toggleCast(t.id); s.state = 'done'; s.output = f || ''; return `record ${t.title}: off` + (f ? ` - saved to ${f}` : ' (it was not recording)') + '.'; }
      if (t.castFile) { s.state = 'done'; s.output = t.castFile; return `record ${t.title}: already recording to ${t.castFile}.`; }
      await this.tabs.toggleCast(t.id);
      const f = (await waitFor(() => this.tabs.get(t.id)?.castFile, 3000)) ?? '';
      s.state = f ? 'done' : 'error'; s.output = f; return `record ${t.title}: ` + (f ? `on - ${f}.` : 'could not start.');
    } catch (e) { s.state = 'error'; s.note = errText(e); return `${s.kind} ${t.title}: ${errText(e)}`; }
  }

  /** A lab line: lab show (read), or a change to the emulated lab (asked like a configuration change). */
  private async labStep(s: Step, chatId: string): Promise<string> {
    const line = `lab ${s.command}`;
    const [verb0, ...args] = s.command.trim().split(/\s+/);
    const verb = (verb0 ?? '').toLowerCase();
    s.tab = tr('Emulated lab');
    try {
      const json = s.command.trim().slice((verb0 ?? '').length).trim();
      if (LAB_TOOLS.includes(verb) && (json.startsWith('{') || !LAB_VERBS.includes(verb))) return await this.labTool(s, chatId, verb, json);
      if (verb === 'show' || verb === '') {
        const t = await this.backend.emuAgentText();
        s.state = 'done'; s.auto = true; s.output = t;
        return `${line}:\n${t}`;
      }
      const v: LabView = await this.backend.emuState();
      const devs = v.devices ?? [];
      const dev = (n?: string) => devs.find(d => d.hostname.toLowerCase() === (n ?? '').toLowerCase() || d.id === n);
      const ids = (n?: string) => (n ?? '').toLowerCase() === 'all' ? devs.map(d => d.id) : (dev(n) ? [dev(n)!.id] : null);
      const bad = (why: string) => { s.state = 'error'; s.note = why; return `${line}: not done - ${why}.`; };
      if (!LAB_VERBS.includes(verb)) return bad(`unknown lab command; use ${['show', ...LAB_VERBS, ...LAB_TOOLS].join(', ')}`);
      if (!v.exists || !devs.length) return bad('there is no emulated lab with switches');
      const one = verb === 'checkcabling' ? devs.map(d => d.id) : verb === 'start' || verb === 'stop' ? ids(args[0]) : dev(args[0]) ? [dev(args[0])!.id] : null;
      if (!one) return bad(`no switch is called ${args[0] ?? ''} (lab show lists them)`);
      // reading the switch's LAGs changes nothing; the rest change the lab
      if (verb !== 'readlags' && verb !== 'checkcabling') {
        const perm = this.perm();
        if (perm === 'readonly') { s.state = 'refused'; s.note = tr('Read-only mode: not run'); return `${line}: not done - the user allows only read-only actions. Tell the user what to do instead.`; }
        // lab changes are not asked (automated tests must not wait): each is kept in the lab's Changes history, with Revert
        s.auto = true;
      }
      s.state = 'running'; this.touch(chatId);
      const chg = verb !== 'readlags' && verb !== 'checkcabling' ? await this.backend.emuAgentChangeBegin(line, verb).catch(() => '') : '';
      let out: LabView;
      switch (verb) {
        case 'start': out = await this.backend.emuStart(one); break;
        case 'stop': out = await this.backend.emuStop(one); break;
        case 'shutdown': out = await this.backend.emuShutDown(one); break;
        case 'cable': {
          const b = dev(args[2]);
          if (!b || !args[1] || !args[3]) return bad('give lab cable <switch> <port> <switch> <port>');
          out = await this.backend.emuConnect({ a: one[0], aPort: args[1], b: b.id, bPort: args[3] });
          break;
        }
        case 'uncable': if (!args[1]) return bad('give the port'); out = await this.backend.emuDisconnect(one[0], args[1]); break;
        case 'pull': case 'plug': if (!args[1]) return bad('give the port'); out = await this.backend.emuSetLink(one[0], args[1], verb === 'plug'); break;
        case 'lag': {
          const mode = (args[2] ?? '').toLowerCase();
          const ports = (args.slice(3).join(',')).split(',').map(p => p.trim()).filter(Boolean);
          if (!args[1] || (mode !== 'lacp' && mode !== 'static') || !ports.length) return bad('give lab lag <switch> <name> lacp|static <port,port>');
          out = await this.backend.emuSetLag(one[0], args[1], mode, ports);
          break;
        }
        case 'nolag': if (!args[1]) return bad('give the LAG name'); out = await this.backend.emuSetLag(one[0], args[1], '', []); break;
        case 'uplink': if (!args[1] || !/^(on|off)$/i.test(args[2] ?? '')) return bad('give lab uplink <switch> <port> on|off'); out = await this.backend.emuSetUplink(one[0], args[1], args[2].toLowerCase() === 'on'); break;
        case 'setlag': case 'unsetlag': if (!args[1]) return bad('give the LAG name (lab show lists them)'); out = await this.backend.emuApplyLag(one[0], args.slice(1).join(' '), verb === 'unsetlag'); break;
        case 'checkcabling': {
          out = await this.backend.emuCheckCabling();
          const name = (id: string) => (out.devices ?? []).find(d => d.id === id)?.hostname ?? id;
          const res = (out.links ?? []).map(k => `${name(k.a)} ${k.aPort} <-> ${name(k.b)} ${k.bPort}: ${k.lldp || 'not checked'}`).join('\n');
          s.state = 'done'; s.output = res; return `${line}:\n${res}`;
        }
        default: out = await this.backend.emuReadLags(one);
      }
      if (chg) void this.backend.emuAgentChangeEnd(chg, 'ok');
      void this.ui.refreshTree();
      const names = one.map(id => devs.find(d => d.id === id)?.hostname ?? id);
      const after = (out.devices ?? []).filter(d => one.includes(d.id)).map(d => `${d.hostname}: ${d.stateText || d.state}`).join(', ');
      s.state = 'done'; s.output = after || tr('Done');
      return `${line}: done for ${names.join(', ')}` + (after ? ` - now ${after}` : '') + '. Use lab show to see the lab again.';
    } catch (e) {
      s.state = 'error'; s.note = errText(e).slice(0, 160);
      return `${line}: failed - ${errText(e)}`;
    }
  }

  /** A lab tool with JSON arguments (the connector's: traffic, capture, console, link_check...): reads and changes run (changes go to the lab's Changes history); Read only refuses changes. */
  private async labTool(s: Step, chatId: string, verb: string, json: string): Promise<string> {
    const line = `lab ${s.command}`;
    let args: Record<string, unknown> = {};
    if (json) {
      try { const a = JSON.parse(json); if (a && typeof a === 'object' && !Array.isArray(a)) args = a; else throw new Error(); }
      catch { s.state = 'error'; s.note = tr('The arguments are not JSON'); return `${line}: not done - give the arguments as one JSON object on the same line.`; }
    }
    s.state = 'running'; this.touch(chatId);
    // lab changes are not asked (automated tests must not wait; each is kept in the lab's Changes history with Revert); Read only stops them
    const r = await this.backend.emuAgentTool(verb, args, this.perm() !== 'readonly');
    if (r.ask) { s.ask = r.text; s.danger = !!r.danger; s.state = 'refused'; s.note = tr('Read-only mode: not run'); return `${line}: not done - the user allows only read-only actions. Tell the user what to do instead.`; }
    s.auto = true; void this.ui.refreshTree();
    const out = r.out ?? '';
    s.state = 'done'; s.output = out || tr('Done');
    return `${line}:\n${out.length > MAX_OUT ? '…\n' + out.slice(-MAX_OUT) : out}`;
  }

  private ask(s: Step, chatId: string) {
    s.state = 'ask'; this.touch(chatId);
    return new Promise<{ go: 'run' | 'skip' | 'stop' | 'chat'; command?: string }>(res => this.waits.set(s, r => { this.waits.delete(s); res(r); }));
  }

  private refusal(s: Step, go: 'skip' | 'stop' | 'run' | 'chat') {
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
  private saving?: ReturnType<typeof setTimeout>;
  persist() {
    if (!this.loaded) return;
    clearTimeout(this.saving);
    this.saving = setTimeout(() => {
      const keep = this.chats().slice(0, MAX_CHATS).map(c => ({ ...c, msgs: c.msgs.map(m => ({ ...m, wire: m.role === 'assistant' ? m.wire : undefined, images: (m.images ?? []).map(i => ({ ...i, data: '', url: '' })) })) }));
      void this.backend.aiChatsSave(JSON.stringify({ version: 1, chats: keep, dock: this.dock(), width: this.width() })).catch(() => {});
    }, 300);
  }
}

/** The answer's text without its termwright block, and the actions in it. */
export function parseActions(text: string): { text: string; actions: { kind: StepKind; tab: string; command: string }[] } {
  const actions: { kind: StepKind; tab: string; command: string }[] = [];
  const out = text.replace(/```termwright[^\n]*\n([\s\S]*?)```/g, (_, body: string) => {
    for (const raw of body.split('\n')) {
      const l = raw.trim();
      let m = /^run\s+(.+?)\s*:\s*(.+)$/i.exec(l);
      if (m) { actions.push({ kind: 'run', tab: m[1].trim(), command: m[2].trim() }); continue; }
      m = /^(log|record)\s+(.+?)\s*:\s*(.+)$/i.exec(l);
      if (m) { actions.push({ kind: m[1].toLowerCase() as StepKind, tab: m[2].trim(), command: m[3].trim() }); continue; }
      m = /^lab(?:\s+(.*))?$/i.exec(l);
      if (m) { actions.push({ kind: 'lab', tab: 'lab', command: (m[1] ?? 'show').trim() }); continue; }
      m = /^(open|find)\s+(.+)$/i.exec(l);
      if (m) actions.push({ kind: m[1].toLowerCase() as StepKind, tab: m[2].trim(), command: '' });
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

function sessions(root: SessionNode | null): SessionNode[] {
  const out: SessionNode[] = [];
  const walk = (x: SessionNode | null) => { if (!x) return; if (!x.isFolder) out.push(x); (x.children ?? []).forEach(walk); };
  walk(root);
  return out;
}

/** The saved-session folders, for the agent. */
function folders(root: SessionNode | null): string[] {
  const out: string[] = [];
  const walk = (x: SessionNode | null) => { if (!x) return; if (x.isFolder && x.path) out.push(x.path); (x.children ?? []).forEach(walk); };
  walk(root);
  return out;
}

/** Saved sessions whose folder/name path, address or device type contains every word. */
export function findSessions(root: SessionNode | null, q: string): SessionNode[] {
  const words = q.trim().toLowerCase().replace(/^["']|["']$/g, '').split(/\s+/).filter(w => w && w !== '*');
  return sessions(root).filter(s => { const h = `${s.path} ${s.hostname ?? ''} ${s.deviceType ?? ''} ${s.description ?? ''}`.toLowerCase(); return words.every(w => h.includes(w)); });
}

/** A saved session by path, name or address; `many` when a name or address is not unique. */
export function resolveSession(root: SessionNode | null, name: string): { node: SessionNode | null; many: number } {
  const n = name.trim().replace(/^["']|["']$/g, '').toLowerCase();
  const all = sessions(root);
  const by = (f: (s: SessionNode) => boolean) => all.filter(f);
  for (const l of [by(s => s.path.toLowerCase() === n), by(s => s.name.toLowerCase() === n), by(s => (s.hostname ?? '').toLowerCase() === n)]) {
    if (l.length === 1) return { node: l[0], many: 0 };
    if (l.length > 1) return { node: null, many: l.length };
  }
  return { node: null, many: 0 };
}

async function waitFor<T>(f: () => T | undefined | null, ms: number): Promise<T | null> {
  const end = Date.now() + ms;
  while (Date.now() < end) { const v = f(); if (v) return v; await sleep(400); }
  return null;
}
const sleep = (ms: number) => new Promise(r => setTimeout(r, ms));
function store(k: string) { try { return localStorage.getItem(k) ?? ''; } catch { return ''; } }
function save(k: string, v: string) { try { localStorage.setItem(k, v); } catch { /* full or blocked */ } }
function browserChats(): Chat[] { try { const v = JSON.parse(localStorage.getItem(KEY_CHATS) ?? '[]'); return Array.isArray(v) ? v : []; } catch { return []; } }
function drop(k: string) { try { localStorage.removeItem(k); } catch { /* blocked */ } }
