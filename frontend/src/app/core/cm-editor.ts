// The light text editor of the file viewer (CodeMirror 6). Loaded only when a file is opened (import()),
// so it costs nothing until then. One file at a time: no tabs, projects or code intelligence - colours,
// folding, find, wrap and plain editing.
import { Compartment, EditorState, type Extension } from '@codemirror/state';
import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection, highlightSpecialChars } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { HighlightStyle, StreamLanguage, forceParsing, syntaxHighlighting, foldGutter, foldKeymap, bracketMatching, indentOnInput, type StreamParser } from '@codemirror/language';
import { highlightSelectionMatches, openSearchPanel, search, searchKeymap } from '@codemirror/search';
import { tags as t } from '@lezer/highlight';
import { json } from '@codemirror/lang-json';
import { yaml } from '@codemirror/lang-yaml';
import { xml } from '@codemirror/lang-xml';
import { markdown } from '@codemirror/lang-markdown';
import { shell } from '@codemirror/legacy-modes/mode/shell';
import { go } from '@codemirror/legacy-modes/mode/go';
import { python } from '@codemirror/legacy-modes/mode/python';
import { javascript, typescript } from '@codemirror/legacy-modes/mode/javascript';
import { tcl } from '@codemirror/legacy-modes/mode/tcl';
import { properties } from '@codemirror/legacy-modes/mode/properties';
import { toml } from '@codemirror/legacy-modes/mode/toml';
import type { FileKind } from './file-kind';
export type { FileKind } from './file-kind';

const YANG_KW = new Set(('action anydata anyxml argument augment base belongs-to bit case choice config contact container default description deviate deviation ' +
  'enum error-app-tag error-message extension feature fraction-digits grouping identity if-feature import include input key leaf leaf-list length list mandatory ' +
  'max-elements min-elements modifier module must namespace notification ordered-by organization output path pattern position prefix presence range reference ' +
  'refine require-instance revision revision-date rpc status submodule type typedef unique units uses value when yang-version yin-element').split(' '));

/** YANG modules: statements, strings, comments. */
const yangParser: StreamParser<{ c: boolean }> = {
  startState: () => ({ c: false }),
  token(s, st) {
    if (st.c) { if (s.match(/.*?\*\//)) st.c = false; else s.skipToEnd(); return 'comment'; }
    if (s.match('//')) { s.skipToEnd(); return 'comment'; }
    if (s.match('/*')) { st.c = true; return 'comment'; }
    if (s.match(/^"(?:[^"\\]|\\.)*"?/) || s.match(/^'[^']*'?/)) return 'string';
    if (s.match(/^[{};]/)) return 'bracket';
    if (s.match(/^[\w.:-]+/)) { const w = s.current(); return YANG_KW.has(w) ? 'keyword' : /^\d/.test(w) ? 'number' : w.includes(':') ? 'typeName' : null; }
    s.next(); return null;
  },
};

const IP = /^(?:\d{1,3}\.){3}\d{1,3}(?:\/\d{1,2})?/;
const MAC = /^(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}/i;

/** Logs: severities, timestamps, addresses. */
const logParser: StreamParser<unknown> = {
  token(s) {
    if (s.match(/^\b(?:EMERG|ALERT|CRIT(?:ICAL)?|FATAL|ERROR|ERR|FAIL(?:ED|URE)?)\b/i)) return 'invalid';
    if (s.match(/^\b(?:WARN(?:ING)?)\b/i)) return 'string';
    if (s.match(/^\b(?:NOTICE|INFO)\b/i)) return 'typeName';
    if (s.match(/^\b(?:DEBUG|TRACE)\b/i)) return 'meta';
    if (s.match(MAC) || s.match(IP)) return 'number';
    if (s.match(/^\d{4}-\d\d-\d\d[T ]\d\d:\d\d:\d\d(?:[.,]\d+)?(?:Z|[+-]\d\d:?\d\d)?/) || s.match(/^(?:[A-Z][a-z]{2} +\d{1,2} )?\d\d:\d\d:\d\d(?:\.\d+)?/)) return 'meta';
    if (s.match(/^"(?:[^"\\]|\\.)*"/)) return 'string';
    if (s.match(/^\w+/)) return null;
    s.next(); return null;
  },
};

const CFG_KW = /^(?:configure|config|create|delete|enable|disable|unconfigure|interface|vlan|router|ip|ipv6|no|shutdown|exit|end|hostname|description|switchport|mode|access|trunk|tagged|untagged|add|ports?|show|set|save|ntp|snmp(?:-server)?|logging|radius|tacacs|aaa|username|line|route|static|ospf|bgp|isis|vrf|vr|lacp|sharing|stp|mstp|spanning-tree|lldp|cdp|acl|access-list|permit|deny|policy|qos|mtu|speed|duplex)\b/i;

/** Device configurations (EXOS, VOSS, IOS-like): comments, keywords, addresses. */
const configParser: StreamParser<unknown> = {
  token(s) {
    if (s.sol() && s.match(/^\s*[!#]/)) { s.skipToEnd(); return 'comment'; }
    if (s.match(/^"(?:[^"\\]|\\.)*"?/)) return 'string';
    if (s.match(MAC) || s.match(IP)) return 'number';
    if (s.match(CFG_KW)) return 'keyword';
    if (s.match(/^\d+(?::\d+|\/\d+)*/)) return 'number';
    if (s.match(/^[\w.-]+/)) return null;
    s.next(); return null;
  },
};

/** VS Code tells names apart: control words purple, function and method calls yellow, types teal. CodeMirror's
 * old modes call every name a "variable" (and Go's calls the dot in a.b a number) - this sorts them out. */
function vsLike<S>(p: StreamParser<S>, o: { control: string; types?: string }): StreamParser<S> {
  const control = new Set(o.control.split(' ')), types = new Set((o.types ?? '').split(' '));
  const name = /^(variable|def|property|variableName|variable-2|variable-3)/;
  return {
    ...p,
    token(s, st) {
      const start = s.pos, style = p.token(s, st), w = s.current();
      if (w === '.') return 'punctuation';
      if (!style) return style;
      if (style === 'keyword' && control.has(w)) return 'controlKeyword';
      if ((style === 'keyword' || style === 'builtin' || style === 'variable') && types.has(w)) return 'typeName';
      const call = s.match(/^\s*\(/, false);
      if ((style === 'atom' || style === 'builtin') && call) return 'variableName.function';
      if (style === 'builtin') return 'typeName';
      if (name.test(style)) {
        if (call) return 'variableName.function';
        if (/^[A-Z]/.test(w) && (s.string.charAt(start - 1) === '.' || s.match(/^\s*\{/, false))) return 'typeName';
      }
      return style;
    },
  };
}
const GO = vsLike(go, { control: 'if else for range return switch case default break continue go defer select goto fallthrough',
  types: 'bool byte complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any comparable' });
const PY = vsLike(python, { control: 'if elif else for while return break continue pass try except finally raise with yield await import from as in is not and or' });
const JS = vsLike(javascript, { control: 'if else for while do return break continue switch case default try catch finally throw await yield import export from' });
const TS = vsLike(typescript, { control: 'if else for while do return break continue switch case default try catch finally throw await yield import export from' });
const CODE: Record<string, StreamParser<unknown>> = { go: GO, py: PY, js: JS, mjs: JS, ts: TS, tcl, exp: tcl, toml } as Record<string, StreamParser<unknown>>;

function language(kind: FileKind, name: string): Extension {
  const ext = name.toLowerCase().split('.').pop() ?? '';
  switch (kind) {
    case 'json': return json();
    case 'yaml': return yaml();
    case 'xml': return xml();
    case 'md': return markdown({ codeLanguages: info => { // fenced code in its language's colours, as in VS Code
      const l = info.toLowerCase();
      if (l === 'json') return json().language;
      if (l === 'yaml' || l === 'yml') return yaml().language;
      if (l === 'xml' || l === 'html') return xml().language;
      const m: Record<string, StreamParser<unknown>> = { go: GO, golang: GO, python: PY, py: PY, sh: shell, bash: shell, shell, zsh: shell, js: JS, javascript: JS, ts: TS, typescript: TS, tcl, toml, ini: properties } as Record<string, StreamParser<unknown>>;
      if (m[l]) return StreamLanguage.define(m[l]);
      if (l === 'yang') return StreamLanguage.define(yangParser);
      if (l === 'log') return StreamLanguage.define(logParser);
      return null;
    } });
    case 'yang': return StreamLanguage.define(yangParser);
    case 'log': return StreamLanguage.define(logParser);
    case 'config': return StreamLanguage.define(configParser);
    case 'ini': return StreamLanguage.define(ext === 'toml' ? toml : properties);
    case 'shell': return StreamLanguage.define(shell);
    case 'code': return CODE[ext] ? StreamLanguage.define(CODE[ext]) : [];
    default: return [];
  }
}

// VS Code's Dark+ / Light+ token colours, as CSS variables of the viewer (set for the light and dark theme)
const v = (n: string) => `var(--vs-${n})`;
const style = HighlightStyle.define([
  { tag: [t.keyword, t.definitionKeyword, t.moduleKeyword, t.bool, t.null, t.atom, t.self], color: v('kw') },
  { tag: [t.controlKeyword, t.operatorKeyword], color: v('ctrl') },
  { tag: [t.string, t.special(t.string), t.attributeValue, t.character], color: v('str') },
  { tag: [t.number, t.integer, t.float], color: v('num') },
  { tag: [t.regexp, t.escape], color: v('re') },
  { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: v('comment') },
  { tag: [t.propertyName, t.definition(t.propertyName)], color: v('prop') },
  { tag: [t.attributeName], color: v('attr') },
  { tag: [t.tagName, t.angleBracket], color: v('tag') },
  { tag: [t.typeName, t.className, t.namespace], color: v('type') },
  { tag: [t.function(t.variableName), t.function(t.propertyName)], color: v('fn') },
  { tag: [t.variableName, t.definition(t.variableName)], color: v('var') },
  { tag: [t.heading, t.heading1, t.heading2, t.heading3, t.heading4], color: v('heading'), fontWeight: 'bold' },
  { tag: t.strong, fontWeight: 'bold' },
  { tag: t.emphasis, fontStyle: 'italic' },
  { tag: t.strikethrough, textDecoration: 'line-through' },
  { tag: t.monospace, color: v('code') },
  { tag: [t.link, t.url], color: v('link'), textDecoration: 'underline' },
  { tag: t.quote, color: v('quote') },
  { tag: [t.processingInstruction, t.contentSeparator, t.punctuation, t.separator, t.bracket], color: v('punct') },
  { tag: t.meta, color: v('meta') },
  { tag: t.invalid, color: v('err'), fontWeight: '600' },
]);

const theme = EditorView.theme({
  '&': { height: '100%', fontSize: 'var(--vs-size)', color: v('fg'), backgroundColor: v('bg') },
  '.cm-scroller': { fontFamily: v('font'), lineHeight: 'var(--vs-lh)' },
  '.cm-content': { caretColor: v('caret'), padding: '6px 0' },
  '.cm-gutters': { backgroundColor: v('bg'), color: v('lineno'), border: 'none', paddingRight: '6px' },
  '.cm-activeLineGutter': { backgroundColor: 'transparent', color: v('lineno-on') },
  '.cm-activeLine': { backgroundColor: v('line') },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': { backgroundColor: v('sel') + ' !important' },
  '.cm-searchMatch': { backgroundColor: v('match') },
  '.cm-searchMatch-selected': { backgroundColor: v('match-on') },
  '.cm-selectionMatch': { backgroundColor: v('sel-other') },
  '.cm-matchingBracket': { outline: '1px solid ' + v('lineno'), backgroundColor: 'transparent' },
  '.cm-panels': { backgroundColor: 'var(--panel)', color: 'var(--text)' },
  '.cm-panels.cm-panels-top': { borderBottom: '1px solid var(--border)' },
  '.cm-panel input, .cm-panel button, .cm-panel label': { fontSize: '12px' },
  // the find bar, as VS Code's (CodeMirror's own is light: white buttons in the dark theme)
  '.cm-panel.cm-search': { padding: '6px 10px', backgroundColor: v('head'), color: v('fg') },
  '.cm-textfield': { backgroundColor: v('bg'), color: v('fg'), border: '1px solid ' + v('bd'), borderRadius: '4px', padding: '3px 7px', outline: 'none' },
  '.cm-textfield:focus': { borderColor: v('status') },
  '.cm-button': { backgroundImage: 'none', backgroundColor: v('btn'), color: v('fg'), border: '1px solid ' + v('bd'), borderRadius: '4px', padding: '2px 10px', cursor: 'pointer' },
  '.cm-button:hover': { backgroundColor: v('btn-hover') },
  '.cm-button:active': { backgroundImage: 'none', backgroundColor: v('btn-hover') },
  '.cm-panel.cm-search label': { color: v('fg') },
  '.cm-panel.cm-search input[type=checkbox]': { accentColor: v('status') },
  '.cm-panel.cm-search [name=close]': { color: v('fg'), backgroundColor: 'transparent', border: 'none', fontSize: '16px', cursor: 'pointer' },
  '.cm-foldGutter span': { color: v('lineno') },
  '.cm-foldPlaceholder': { backgroundColor: v('line'), border: 'none', color: v('lineno') },
});

export interface TextEditor {
  view: EditorView;
  text(): string;
  setText(s: string, keepScroll?: boolean): void;
  setReadOnly(ro: boolean): void;
  setWrap(on: boolean): void;
  find(): void;
  toEnd(): void;
  /** Moves to a line (1-based) and selects it. */
  goLine(n: number): void;
  destroy(): void;
}

/** Makes the editor in host. numbers: line numbers and folding (configs, JSON, YAML, code); not for logs and text. */
export function createEditor(host: HTMLElement, o: { text: string; kind: FileKind; name: string; readOnly: boolean; wrap: boolean; numbers: boolean; onChange?: () => void; onCursor?: (line: number, col: number, sel: number) => void }): TextEditor {
  const ro = new Compartment(), wrap = new Compartment();
  const view = new EditorView({
    parent: host,
    state: EditorState.create({
      doc: o.text,
      extensions: [
        ...(o.numbers ? [lineNumbers(), foldGutter(), highlightActiveLineGutter()] : []),
        highlightSpecialChars(), history(), drawSelection(), highlightActiveLine(), bracketMatching(), indentOnInput(),
        search({ top: true }), highlightSelectionMatches(),
        keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap, ...foldKeymap, indentWithTab]),
        language(o.kind, o.name), syntaxHighlighting(style), theme,
        ro.of([EditorState.readOnly.of(o.readOnly), EditorView.editable.of(!o.readOnly)]),
        wrap.of(o.wrap ? EditorView.lineWrapping : []),
        EditorView.updateListener.of(u => {
          if (u.docChanged && o.onChange) o.onChange();
          if ((u.selectionSet || u.docChanged) && o.onCursor) { const m = u.state.selection.main, l = u.state.doc.lineAt(m.head); o.onCursor(l.number, m.head - l.from + 1, m.to - m.from); }
        }),
      ],
    }),
  });
  // colour the whole file now, not only around what is on screen: a jump through a long file (Go, logs…)
  // otherwise left stretches without colours; what one go does not finish is done in the next ones
  const parseAll = () => {
    let n = 0;
    const step = () => { if (!view.dom.isConnected || forceParsing(view, view.state.doc.length, 150) || ++n > 40) return; setTimeout(step, 30); };
    setTimeout(step, 0);
  };
  parseAll();
  return {
    view,
    text: () => view.state.doc.toString(),
    setText(s, keepScroll) {
      const top = view.scrollDOM.scrollTop;
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: s } });
      if (keepScroll) view.scrollDOM.scrollTop = top;
      parseAll();
    },
    setReadOnly: r => view.dispatch({ effects: ro.reconfigure([EditorState.readOnly.of(r), EditorView.editable.of(!r)]) }),
    setWrap: w => view.dispatch({ effects: wrap.reconfigure(w ? EditorView.lineWrapping : []) }),
    find: () => { openSearchPanel(view); },
    toEnd: () => { view.dispatch({ selection: { anchor: view.state.doc.length }, scrollIntoView: true }); },
    goLine(n) {
      const l = view.state.doc.line(Math.max(1, Math.min(n, view.state.doc.lines)));
      view.dispatch({ selection: { anchor: l.from, head: l.to }, scrollIntoView: true }); view.focus();
    },
    destroy: () => view.destroy(),
  };
}
