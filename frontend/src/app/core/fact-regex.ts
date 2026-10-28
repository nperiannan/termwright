/** Builds a regular expression that finds a highlighted value in command
 *  output again: the label in front of it on its line ("ModelName :", "System
 *  MAC:") is kept, things that change from device to device become \S+, and
 *  the value becomes a group shaped like what was highlighted (a MAC or IP
 *  address - not for a version - a word, or the rest of the line). The pattern works in Go (RE2)
 *  and JavaScript, with multi-line mode. */
export function factRegex(line: string, start: number, length: number, name = ''): string {
  const value = line.slice(start, start + length);
  const before = line.slice(0, start);
  const after = line.slice(start + length);
  const v = value.trim();
  if (!v) return '';

  // what comes after the value: a closing bracket or comma that ends it, or the end of the line
  const next = after.match(/^\s*([)\],;"'>])/)?.[1] ?? '';
  const atEnd = !after.trim();

  // the value
  let group: string;
  if (/^[0-9a-f]{2}([:.-]?[0-9a-f]{2}){5}$/i.test(v) || /^[0-9a-f]{4}\.[0-9a-f]{4}\.[0-9a-f]{4}$/i.test(v)) group = '([0-9a-fA-F]{2}(?:[:.-]?[0-9a-fA-F]{2}){5}|[0-9a-fA-F]{4}\\.[0-9a-fA-F]{4}\\.[0-9a-fA-F]{4})';
  else if (/^\d{1,3}(\.\d{1,3}){3}(\/\d{1,2})?$/.test(v) && !/version|release|firmware|software|image/i.test(name)) group = '(\\d{1,3}(?:\\.\\d{1,3}){3}(?:/\\d{1,2})?)';
  else if (next) group = `([^${esc(next)}\\n]+?)`;
  else if (/\s/.test(v)) group = '(.+?)';
  else group = '(\\S+)';

  // the label: the separator nearest the value (":", "=", "is", "Version")
  // with the word or two in front of it; text further left is not needed
  let label = '';
  const seps = [...before.matchAll(/(:|=|\bis\b|\b[Vv]ersion\b)/g)];
  const last = seps[seps.length - 1];
  if (last && last.index !== undefined) {
    const pre = before.slice(0, last.index);
    const rest = before.slice(last.index + last[0].length);
    const n = last[0] === ':' || last[0] === '=' ? 2 : 1;
    const w = pre.match(new RegExp(`([A-Za-z][\\w#./-]*(?:\\s+[A-Za-z][\\w#./-]*){0,${n - 1}})(\\s*)$`));
    label = w ? words(w[1]) + (/^\w/.test(last[0]) ? '\\s+' : '\\s*') + esc(last[0]) : (/^\w/.test(last[0]) ? '\\b' : '') + esc(last[0]);
    label += rest.trim() ? '\\s*' + generalize(rest.replace(/^\s+/, '')) : '\\s*';
  } else if (before.trim()) {
    label = generalize(before.replace(/^\s+/, ''));
  }
  const head = label ? label : '^\\s*';
  const tail = next ? esc(next) : atEnd ? '\\s*$' : '';
  return head + group + tail;
}

/** Label words: kept literally, runs of blanks become \s+ / \s*. */
function words(s: string): string {
  return s.trim().split(/(\s+)/).map(p => (/^\s+$/.test(p) ? '\\s+' : esc(p))).join('').replace(/\\s\+(:|=)/g, '\\s*$1');
}

/** Text between the label and the value that may differ between devices:
 *  words become \S+, blanks \s+, and a bracket or quote right before the
 *  value stays as it is. */
function generalize(s: string): string {
  const m = s.match(/^(.*?)([(\["'<]?)$/s)!;
  const body = m[1], open = m[2];
  const parts = body.split(/(\s+)/).filter(Boolean).map(p => (/^\s+$/.test(p) ? '\\s+' : '\\S+'));
  return parts.join('') + (open ? esc(open) : '');
}

function esc(s: string): string { return s.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&'); }

/** The first group of pattern in text (multi-line), or null when it does not match. */
export function testFactRegex(pattern: string, text: string): string | null {
  try {
    const m = new RegExp(pattern, 'm').exec(text.replace(/\r/g, ''));
    if (!m) return null;
    return (m[1] ?? m[0]).trim();
  } catch { return null; }
}
