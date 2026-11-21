/** A small syntax highlighter for the code views (Go, Python, TCL, YAML): HTML with <span class="hl-…">. */
export type Lang = 'go' | 'py' | 'tcl' | 'yaml';

const KW: Record<Lang, string[]> = {
  go: ['package', 'import', 'func', 'return', 'if', 'else', 'for', 'range', 'var', 'const', 'type', 'struct', 'map', 'nil', 'true', 'false', 'defer', 'go', 'switch', 'case', 'default', 'break', 'continue', 'string', 'int', 'bool', 'float64', 'any', 'error'],
  py: ['import', 'from', 'def', 'class', 'return', 'if', 'elif', 'else', 'for', 'in', 'while', 'with', 'as', 'try', 'except', 'finally', 'raise', 'pass', 'None', 'True', 'False', 'and', 'or', 'not', 'is', 'lambda', 'self'],
  tcl: ['proc', 'set', 'if', 'else', 'elseif', 'foreach', 'for', 'while', 'return', 'source', 'package', 'require', 'namespace', 'variable', 'expr', 'puts', 'list', 'dict', 'catch', 'uplevel'],
  yaml: ['true', 'false', 'null'],
};

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

export function highlight(src: string, lang: Lang): string {
  const kw = new Set(KW[lang]);
  const comment = lang === 'go' ? '\\/\\/[^\\n]*' : '#[^\\n]*';
  const str = lang === 'go' ? '"(?:[^"\\\\\\n]|\\\\.)*"|`[^`]*`' : lang === 'tcl' ? '"(?:[^"\\\\\\n]|\\\\.)*"' : '"(?:[^"\\\\\\n]|\\\\.)*"|\'(?:[^\'\\\\\\n]|\\\\.)*\'';
  const key = lang === 'yaml' ? '(?<=^|\\n)[ \\t]*(?:- )?[\\w.$-]+(?=:)' : '(?!x)x';
  const re = new RegExp(`(${comment})|(${str})|(${key})|(\\b\\d+(?:\\.\\d+)?\\b)|(\\{\\{[^}]+\\}\\})|([A-Za-z_][\\w:]*)`, 'g');
  let out = '', last = 0, m: RegExpExecArray | null;
  while ((m = re.exec(src))) {
    out += esc(src.slice(last, m.index));
    const t = m[0];
    if (m[1]) out += `<span class="hl-c">${esc(t)}</span>`;
    else if (m[2]) out += `<span class="hl-s">${esc(t).replace(/\{\{[^}]+\}\}/g, v => `<span class="hl-v">${v}</span>`)}</span>`;
    else if (m[3]) { const i = t.search(/[\w.$-]+$/); out += esc(t.slice(0, i)) + `<span class="hl-k">${esc(t.slice(i))}</span>`; }
    else if (m[4]) out += `<span class="hl-n">${t}</span>`;
    else if (m[5]) out += `<span class="hl-v">${esc(t)}</span>`;
    else if (kw.has(t)) out += `<span class="hl-w">${t}</span>`;
    else if (lang === 'tcl' && /^tw::/.test(t) || (lang !== 'yaml' && /^[A-Z]\w*$/.test(t))) out += `<span class="hl-f">${esc(t)}</span>`;
    else out += esc(t);
    last = m.index + t.length;
    if (t.length === 0) re.lastIndex++;
  }
  return out + esc(src.slice(last));
}
