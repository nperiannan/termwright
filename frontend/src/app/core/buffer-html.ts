import type { IBufferCell, Terminal } from '@xterm/xterm';

/** A keyword of the tab's highlighting set (as compiled by the terminal view). */
export interface HtmlKeyword { re: RegExp; color: string; fg: string; }

const BASE16: [string, string][] = [
  ['black', '#000000'], ['red', '#cd3131'], ['green', '#0dbc79'], ['yellow', '#e5e510'], ['blue', '#2472c8'], ['magenta', '#bc3fbc'],
  ['cyan', '#11a8cd'], ['white', '#e5e5e5'], ['brightBlack', '#666666'], ['brightRed', '#f14c4c'], ['brightGreen', '#23d18b'],
  ['brightYellow', '#f5f543'], ['brightBlue', '#3b8eea'], ['brightMagenta', '#d670d6'], ['brightCyan', '#29b8db'], ['brightWhite', '#e5e5e5'],
];

/** The 256-color palette of a terminal theme. */
function palette(theme: Record<string, unknown>): string[] {
  const p = BASE16.map(([k, d]) => (typeof theme[k] === 'string' ? (theme[k] as string) : d));
  const lv = [0, 95, 135, 175, 215, 255];
  const hex = (n: number) => n.toString(16).padStart(2, '0');
  for (let r = 0; r < 6; r++) for (let g = 0; g < 6; g++) for (let b = 0; b < 6; b++) p.push('#' + hex(lv[r]) + hex(lv[g]) + hex(lv[b]));
  for (let i = 0; i < 24; i++) { const v = 8 + i * 10; p.push('#' + hex(v) + hex(v) + hex(v)); }
  return p;
}

const esc = (s: string) => s.replace(/[&<>]/g, c => (c === '&' ? '&amp;' : c === '<' ? '&lt;' : '&gt;'));

/** The terminal's whole buffer (scrollback and screen) as HTML lines inside a
 *  <pre>: colors, bold, italic, underline and the keyword highlighting as
 *  shown in the tab. Trailing blank lines are left out. */
export function bufferHtml(term: Terminal, keywords: HtmlKeyword[] = []): { html: string; lines: number } {
  const theme = (term.options.theme ?? {}) as Record<string, unknown>;
  const pal = palette(theme);
  const defFg = typeof theme['foreground'] === 'string' ? (theme['foreground'] as string) : '#d4d4d4';
  const defBg = typeof theme['background'] === 'string' ? (theme['background'] as string) : '#1e1f22';
  const color = (isDefault: boolean, isPal: boolean, isRGB: boolean, v: number): string | null =>
    isDefault ? null : isPal ? (pal[v] ?? null) : isRGB ? '#' + v.toString(16).padStart(6, '0') : null;

  const buf = term.buffer.active;
  let last = -1;
  for (let y = buf.length - 1; y >= 0; y--) if (buf.getLine(y)?.translateToString(true)) { last = y; break; }

  const out: string[] = [];
  let cell: IBufferCell | undefined;
  for (let y = 0; y <= last; y++) {
    const line = buf.getLine(y);
    if (!line) { out.push(''); continue; }
    // the cells of the row (wide characters take one entry)
    const cells: { ch: string; style: string }[] = [];
    for (let x = 0; x < line.length; x++) {
      cell = line.getCell(x, cell);
      if (!cell || cell.getWidth() === 0) continue;
      let fg = color(cell.isFgDefault(), cell.isFgPalette(), cell.isFgRGB(), cell.getFgColor());
      let bg = color(cell.isBgDefault(), cell.isBgPalette(), cell.isBgRGB(), cell.getBgColor());
      // bold text in the 8 base colors is shown in the bright ones, as in the terminal
      if (cell.isBold() && cell.isFgPalette() && cell.getFgColor() < 8) fg = pal[cell.getFgColor() + 8];
      if (cell.isInverse()) { const f = fg ?? defFg; fg = bg ?? defBg; bg = f; }
      const st: string[] = [];
      if (fg) st.push('color:' + fg);
      if (bg) st.push('background:' + bg);
      if (cell.isBold()) st.push('font-weight:bold');
      if (cell.isItalic()) st.push('font-style:italic');
      if (cell.isDim()) st.push('opacity:.6');
      const deco = [cell.isUnderline() ? 'underline' : '', cell.isStrikethrough() ? 'line-through' : ''].filter(Boolean).join(' ');
      if (deco) st.push('text-decoration:' + deco);
      if (cell.isInvisible()) st.push('visibility:hidden');
      cells.push({ ch: cell.getChars() || ' ', style: st.join(';') });
    }
    // keyword highlighting: matches in the row's text, mapped back to cells
    const text = cells.map(c => c.ch).join('');
    const start: number[] = [];
    let pos = 0;
    for (const c of cells) { start.push(pos); pos += c.ch.length; }
    const cellAt = (i: number) => { let lo = 0, hi = start.length - 1; while (lo < hi) { const m = (lo + hi + 1) >> 1; if (start[m] <= i) lo = m; else hi = m - 1; } return lo; };
    for (const k of keywords) {
      k.re.lastIndex = 0;
      let m: RegExpExecArray | null;
      let guard = 0;
      while ((m = k.re.exec(text)) && guard++ < 200) {
        if (!m[0].length) { k.re.lastIndex++; continue; }
        const a = cellAt(m.index), b = cellAt(m.index + m[0].length - 1);
        for (let i = a; i <= b; i++) cells[i].style = (cells[i].style ? cells[i].style + ';' : '') + `background:${k.color};color:${k.fg}`;
      }
    }
    // trailing blanks without a background are dropped
    let end = cells.length;
    while (end > 0 && cells[end - 1].ch === ' ' && !/background/.test(cells[end - 1].style)) end--;
    let html = '';
    for (let i = 0; i < end;) {
      let j = i, s = '';
      while (j < end && cells[j].style === cells[i].style) s += cells[j++].ch;
      html += cells[i].style ? `<span style="${cells[i].style}">${esc(s)}</span>` : esc(s);
      i = j;
    }
    out.push(html);
  }
  return { html: `<pre style="color:${defFg};background:${defBg}">${out.join('\n')}</pre>`, lines: last + 1 };
}
