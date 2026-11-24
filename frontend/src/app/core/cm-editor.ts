// The light text editor of the file viewer (CodeMirror 6). Loaded only when a file is opened (import()),
// so it costs nothing until then. One file at a time: no tabs, projects or code intelligence - colours,
// folding, find, wrap and plain editing.
import { Compartment, EditorState, type Extension } from '@codemirror/state';
import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection, highlightSpecialChars } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { HighlightStyle, StreamLanguage, syntaxHighlighting, foldGutter, foldKeymap, bracketMatching, indentOnInput, type StreamParser } from '@codemirror/language';
import { highlightSelectionMatches, openSearchPanel, search, searchKeymap } from '@codemirror/search';
import { tags as t } from '@lezer/highlight';
import { json } from '@codemirror/lang-json';
import { yaml } from '@codemirror/lang-yaml';
import { xml } from '@codemirror/lang-xml';
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
    if (s.match(/^\b(?:WARN(?:ING)?)\b/i)) return 'keyword';
    if (s.match(/^\b(?:NOTICE|INFO)\b/i)) return 'typeName';
    if (s.match(/^\b(?:DEBUG|TRACE)\b/i)) return 'comment';
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

const CODE: Record<string, StreamParser<unknown>> = { go, py: python, js: javascript, mjs: javascript, ts: typescript, tcl, exp: tcl, toml } as Record<string, StreamParser<unknown>>;

function language(kind: FileKind, name: string): Extension {
  const ext = name.toLowerCase().split('.').pop() ?? '';
  switch (kind) {
    case 'json': return json();
    case 'yaml': return yaml();
    case 'xml': return xml();
    case 'yang': return StreamLanguage.define(yangParser);
    case 'log': return StreamLanguage.define(logParser);
    case 'config': return StreamLanguage.define(configParser);
    case 'ini': return StreamLanguage.define(ext === 'toml' ? toml : properties);
    case 'shell': return StreamLanguage.define(shell);
    case 'code': return CODE[ext] ? StreamLanguage.define(CODE[ext]) : [];
    default: return [];
  }
}

// colours from the app's own variables, so light and dark themes both work
const style = HighlightStyle.define([
  { tag: [t.keyword, t.definitionKeyword, t.moduleKeyword, t.controlKeyword], color: 'var(--cm-kw, #c678dd)' },
  { tag: [t.string, t.special(t.string), t.attributeValue], color: 'var(--cm-str, #98c379)' },
  { tag: [t.number, t.bool, t.null, t.atom], color: 'var(--cm-num, #d19a66)' },
  { tag: [t.comment, t.lineComment, t.blockComment], color: 'var(--muted)', fontStyle: 'italic' },
  { tag: [t.propertyName, t.attributeName, t.tagName], color: 'var(--cm-prop, #61afef)' },
  { tag: [t.typeName, t.className], color: 'var(--cm-type, #56b6c2)' },
  { tag: t.meta, color: 'var(--cm-time, #7f8ea3)' },
  { tag: t.invalid, color: 'var(--danger, #f85149)', fontWeight: '600' },
]);

const theme = EditorView.theme({
  '&': { height: '100%', fontSize: 'calc(12.5px * var(--ui-scale, 1))', color: 'var(--text)', backgroundColor: 'transparent' },
  '.cm-scroller': { fontFamily: 'var(--mono, ui-monospace, Menlo, Consolas, monospace)', lineHeight: '1.5' },
  '.cm-content': { caretColor: 'var(--accent)' },
  '.cm-gutters': { backgroundColor: 'transparent', color: 'var(--muted)', border: 'none' },
  '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'color-mix(in srgb, var(--accent) 7%, transparent)' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': { backgroundColor: 'color-mix(in srgb, var(--accent) 28%, transparent) !important' },
  '.cm-searchMatch': { backgroundColor: 'color-mix(in srgb, #e3b341 35%, transparent)' },
  '.cm-panels': { backgroundColor: 'var(--panel)', color: 'var(--text)', borderColor: 'var(--border)' },
  '.cm-panel input, .cm-panel button': { fontSize: '12px' },
  '.cm-foldGutter span': { color: 'var(--muted)' },
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
export function createEditor(host: HTMLElement, o: { text: string; kind: FileKind; name: string; readOnly: boolean; wrap: boolean; numbers: boolean; onChange?: () => void }): TextEditor {
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
        EditorView.updateListener.of(u => { if (u.docChanged && o.onChange) o.onChange(); }),
      ],
    }),
  });
  return {
    view,
    text: () => view.state.doc.toString(),
    setText(s, keepScroll) {
      const top = view.scrollDOM.scrollTop;
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: s } });
      if (keepScroll) view.scrollDOM.scrollTop = top;
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
