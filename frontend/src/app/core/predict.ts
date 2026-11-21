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
  // "(?m)^4\s+\S+\s+(\S+)": a value in the line that starts with 4
  const m = pattern.match(/^(?:\(\?m\))?\^(.+?)((?:\\s\+\\S\+)*)\\s\+\(\\S\+\)$/);
  if (m) { const n = (m[2].match(/\\S\+/g) ?? []).length + 2; return `line "${m[1].replace(/\\(.)/g, '$1')}", value ${n}`; }
  const s = pattern.replace(/\(\?[a-z]+\)/g, '').replace(/\\s[+*]/g, ' ').replace(/\((?:[^()\\]|\\.)*\)\s*$/, '').replace(/\\(.)/g, '$1').replace(/[:\s]+$/, '').trim();
  return s && s.length <= 40 && !/[()[\]|*+?^$]/.test(s) ? s : pattern;
}

/** The scalar values of a JSON document by path ("name", "data.vlan.id", "ports[0]"). */
function flatten(x: unknown, path = '', out: [string, string][] = [], depth = 0): [string, string][] {
  if (depth > 6 || out.length > 200) return out;
  if (x === null || typeof x !== 'object') {
    if (path && (typeof x === 'string' || typeof x === 'number' || typeof x === 'boolean')) out.push([path, String(x)]);
    return out;
  }
  if (Array.isArray(x)) x.slice(0, 50).forEach((v, i) => flatten(v, `${path}[${i}]`, out, depth + 1));
  else for (const [k, v] of Object.entries(x as Record<string, unknown>)) flatten(v, path ? `${path}.${k}` : k, out, depth + 1);
  return out;
}

const lastKey = (p: string) => p.replace(/\[\d+\]/g, '').split('.').pop() ?? p;
const pathBase = (u: string) => u.replace(/^[a-z]+:\/\/[^/]+/i, '').split('?')[0].replace(/\/+$/, '');

/**
 * Checks for an API response from the requests that changed things before it: the values a POST, PUT or
 * PATCH sent must come back - in its own answer, or in a later GET of the same resource or its list. A
 * value is checked by the JSON path where it was found, and only when it is there in this response.
 */
export function predictApiChecks(writes: { url: string; body?: string }[], url: string, response: string, have: TestExpect[] = []): TestExpect[] {
  let doc: unknown;
  try { doc = JSON.parse(response); } catch { return []; }
  const got = flatten(doc), base = pathBase(url), res: TestExpect[] = [];
  const seen = new Set(have.map(e => e.json));
  for (const w of writes) {
    const wb = pathBase(w.url);
    if (!(base === wb || base.startsWith(wb + '/') || wb.startsWith(base + '/') || wb === base.replace(/\/[^/]+$/, ''))) continue;
    let sent: unknown;
    try { sent = JSON.parse(w.body ?? ''); } catch { continue; }
    for (const [p, v] of flatten(sent)) {
      if (v === '' || v.length > 200 || res.length >= 6) continue;
      const k = lastKey(p);
      const hit = got.find(([gp, gv]) => gv === v && lastKey(gp) === k) ?? got.find(([gp, gv]) => gv === v && gp.endsWith(p));
      if (!hit || seen.has(hit[0])) continue;
      seen.add(hit[0]);
      // in a list the position can change: check that the value is in the answer instead
      if (/\[\d+\]/.test(hit[0])) { if (v.length >= 3 && /[A-Za-z]/.test(v) && !seen.has('#' + v)) { res.push({ contains: v }); seen.add('#' + v); } }
      else res.push({ json: hit[0], equals: v });
    }
  }
  return res;
}
