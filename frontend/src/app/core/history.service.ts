import { Injectable, inject, signal } from '@angular/core';
import { Backend } from './backend';
import { HistoryEntry } from './models';
import { Ui } from './ui.service';

/** Prompts at the start of a terminal line: "core-sw1:1#", "X440G2.1 # ", "user@host:~$ ", "[root@srv ~]# ", "PS C:\\> ". */
const PROMPT = [
  /^\*?\s?[\w.@~/:()\[\]-]+(\s[\w.()-]+)?\s?[#>$%]\s?/,
  /^\[[^\]\n]{1,80}\][#$%>]\s?/,
  /^PS [^>\n]{1,200}>\s?/,
];
/** Commands that may carry a secret are never remembered. */
export const SECRET = /pass(word|wd|phrase)|secret|communit(y|ies)|key-string|pre-?shared|\bpsk\b|\btoken\b|api[-_]?key/i;

/** The command typed on a terminal line, without the prompt; null when it cannot be told apart or may carry a secret. */
export function commandOfLine(line: string): string | null {
  const l = line.replace(/\s+$/, '');
  if (!l) return null;
  for (const re of PROMPT) {
    const m = re.exec(l);
    if (m && m[0].length < l.length) {
      const cmd = l.slice(m[0].length).trim();
      return cmd && cmd.length <= 1000 && !SECRET.test(cmd) ? cmd : null;
    }
  }
  return null;
}

/** Remembered commands for history search and autocomplete. */
@Injectable({ providedIn: 'root' })
export class History {
  private backend = inject(Backend);
  private ui = inject(Ui);
  /** Newest first. */
  readonly entries = signal<HistoryEntry[]>([]);
  private loaded = false;

  enabled() { return this.ui.settings()?.recordHistory !== false; }

  async load() {
    if (this.loaded) return;
    this.loaded = true;
    this.entries.set(await this.backend.historyList().catch(() => []) ?? []);
  }

  add(cmd: string, from: { session?: string; title?: string; deviceType?: string }) {
    cmd = cmd.trim();
    if (!cmd || !this.enabled() || SECRET.test(cmd)) return;
    const e: HistoryEntry = { cmd, session: from.session || undefined, title: from.title, deviceType: from.deviceType || undefined, time: new Date().toISOString(), count: 1 };
    this.entries.update(l => {
      const old = l.find(x => x.cmd === cmd && x.session === e.session && (e.session || x.title === e.title));
      if (old) e.count = old.count + 1;
      return [e, ...l.filter(x => x !== old)].slice(0, 5000);
    });
    void this.backend.historyAdd(e).catch(() => {});
  }

  async clear() {
    await this.backend.historyClear();
    this.entries.set([]);
  }

  /** Commands containing every word of q, those starting with it first; one per command text. */
  search(q: string, deviceType = '', limit = 200): HistoryEntry[] {
    const words = q.toLowerCase().split(/\s+/).filter(Boolean);
    const seen = new Set<string>();
    const starts: HistoryEntry[] = [], rest: HistoryEntry[] = [];
    const ql = q.trim().toLowerCase();
    for (const e of this.entries()) {
      if (deviceType && (e.deviceType ?? '').toLowerCase() !== deviceType.toLowerCase()) continue;
      const c = e.cmd.toLowerCase();
      if (!words.every(w => c.includes(w)) || seen.has(e.cmd)) continue;
      seen.add(e.cmd);
      (ql && c.startsWith(ql) ? starts : rest).push(e);
      if (starts.length + rest.length >= limit * 2) break;
    }
    return [...starts, ...rest].slice(0, limit);
  }
}
