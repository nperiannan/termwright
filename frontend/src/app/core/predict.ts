import { TestExpect } from './models';

/** Words of configuration commands that are actions or keywords, not values. */
const KEYWORDS = new Set(('create configure config conf set add delete remove no undo unconfigure enable disable show sh display dis ' +
  'interface int ip ipv4 ipv6 ipaddress address addr mask name description desc mode access trunk allowed native tag tagged untagged ' +
  'ports port vlan vlans vr vrf router route static protocol to from on off all default switchport members member state admin ' +
  'shutdown speed duplex auto full half mtu lag sharing group area network neighbor bgp ospf isis rip stp stpd domain priority ' +
  'cost weight level community user account password secret key snmp syslog server host logging ntp dns sntp').split(' '));
/** Keywords followed by a name ("vlan sales", "name uplink"). */
const NAMING = new Set('vlan vr vrf name description interface lag group policy profile domain stpd community user host vlans'.split(' '));
const ACTIONS = new Set('create configure config conf set add delete remove no undo unconfigure enable disable show sh display dis'.split(' '));

export interface ConfigValue { key: string; value: string; }

/** The values a configuration command sets, with the keyword before each:
 *  "create vlan v200 tag 200" → vlan=v200, tag=200; "configure v200 ipaddress 200.1.1.1/24" → v200, ipaddress=200.1.1.1/24. */
export function configValues(cmd: string): ConfigValue[] {
  const w = cmd.trim().split(/\s+/), out: ConfigValue[] = [];
  for (let i = 0; i < w.length; i++) {
    const t = w[i], lower = t.toLowerCase(), prev = i > 0 ? w[i - 1].toLowerCase() : '';
    if (KEYWORDS.has(lower) || t.length < 2) continue;
    const valueLike = /\d/.test(t) || /[/.:@]/.test(t) || (NAMING.has(prev) && /^[\w.-]+$/.test(t)) || (i > 0 && ACTIONS.has(prev) && /^[A-Za-z][\w.-]*\d|^[A-Z]/.test(t));
    if (valueLike && /^[\w.:/@-]+$/.test(t)) out.push({ key: KEYWORDS.has(prev) && !ACTIONS.has(prev) ? prev : '', value: t });
  }
  return out;
}

const words = (s: string) => new Set(s.toLowerCase().split(/[^a-z0-9]+/).filter(x => x.length >= 3 && !ACTIONS.has(x)));
const esc = (s: string) => s.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&');

/** The value as a whole word in out: its line and position, or null. */
function find(out: string, v: string): { line: string; at: number } | null {
  const re = new RegExp('(^|[^\\w.:/-])' + esc(v) + '(?=$|[^\\w:/-]|\\.(?!\\d))', 'm');
  for (const line of out.split('\n')) {
    const m = re.exec(line);
    if (m) return { line, at: m.index + m[1].length };
  }
  return null;
}

/** The label before a value on its line ("Primary IP:          200.1.1.1/24" → "Primary IP:"), when the line is
 *  "label   value" shaped. */
function labelOf(line: string, at: number): string {
  const before = line.slice(0, at).replace(/\s+$/, '');
  const label = before.split(/\s{2,}/).pop()?.trim() ?? '';
  return label.length >= 2 && label.length <= 40 && /[A-Za-z]/.test(label) && /:$|\s{2,}$/.test(line.slice(0, at)) ? label : '';
}

/**
 * Checks a read step most likely needs, from the configuration commands recorded before it on the same device:
 * the values they set must show up in its output (in the cleanup: what was removed must be gone). Only checks
 * that hold for the output seen while recording are returned, so the test passes when nothing changed.
 * A value gets the most readable check that fits: "Primary IP = 200.1.1.1/24" (a field with its label), else
 * "contains Tag 200" (with the word before it), else "contains v200".
 */
export function predictChecks(configs: string[], show: string, out: string, cleanup: boolean, have: TestExpect[] = []): TestExpect[] {
  if (!out.trim()) return [];
  const sw = words(show);
  const seen = new Set(have.map(e => e.contains ?? e.notContains ?? e.equals ?? ''));
  const res: TestExpect[] = [];
  // a command is related when it shares a topic word with the show command, names something the show command
  // names, or configures an object a related command made ("configure v200 ipaddress …" after "create vlan v200")
  const objs = new Set<string>(), rel = new Set<string>();
  for (let pass = 0; pass < 2; pass++) {
    for (const cmd of configs) {
      const vals = configValues(cmd);
      const direct = [...words(cmd)].some(x => sw.has(x) && (NAMING.has(x) || !KEYWORDS.has(x))) || vals.some(v => show.toLowerCase().includes(v.value.toLowerCase()));
      if (direct || vals.some(v => objs.has(v.value.toLowerCase()))) { rel.add(cmd); vals.forEach(v => objs.add(v.value.toLowerCase())); }
    }
  }
  for (const cmd of configs) {
    const vals = configValues(cmd);
    if (!vals.length || !rel.has(cmd)) continue;
    for (const { key, value } of vals) {
      if (seen.has(value) || res.length >= 6) continue;
      const f = find(out, value);
      if (cleanup) {
        if (!f && /[A-Za-z]/.test(value)) { res.push({ notContains: value }); seen.add(value); }
        continue;
      }
      if (!f) continue;
      const label = labelOf(f.line, f.at);
      if (label && f.line.slice(f.at).trim().split(/\s+/)[0] === value) {
        const pat = esc(label).replace(/\s+/g, '\\s+') + '\\s*(\\S+)';
        if (new RegExp(pat).exec(out)?.[1] === value) { res.push({ field: pat, equals: value }); seen.add(value); continue; }
      }
      const prevWord = f.line.slice(0, f.at).trim().split(/\s+/).pop() ?? '';
      if (/^\d{1,3}$/.test(value)) {
        // a short number alone matches too much: keep the word before it ("Tag 200"), or the keyword
        if (prevWord && /[A-Za-z]/.test(prevWord)) res.push({ contains: prevWord + ' ' + value });
        else if (key) res.push({ matches: '(?i)' + esc(key) + '\\W{0,3}' + value + '\\b' });
        else continue;
      } else res.push({ contains: value });
      seen.add(value);
    }
  }
  return res;
}

/** A field pattern as people read it: "Admin\s+State:\s*(\S+)" → "Admin State". */
export function fieldName(pattern: string): string {
  const s = pattern.replace(/\(\?[a-z]+\)/g, '').replace(/\\s[+*]/g, ' ').replace(/\((?:[^()\\]|\\.)*\)\s*$/, '').replace(/\\(.)/g, '$1').replace(/[:\s]+$/, '').trim();
  return s && s.length <= 40 && !/[()[\]|*+?^$]/.test(s) ? s : pattern;
}
