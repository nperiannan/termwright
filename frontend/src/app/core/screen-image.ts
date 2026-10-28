import type { IBufferCell, Terminal } from '@xterm/xterm';
import { HtmlKeyword } from './buffer-html';

const BASE16: [string, string][] = [
  ['black', '#000000'], ['red', '#cd3131'], ['green', '#0dbc79'], ['yellow', '#e5e510'], ['blue', '#2472c8'], ['magenta', '#bc3fbc'],
  ['cyan', '#11a8cd'], ['white', '#e5e5e5'], ['brightBlack', '#666666'], ['brightRed', '#f14c4c'], ['brightGreen', '#23d18b'],
  ['brightYellow', '#f5f543'], ['brightBlue', '#3b8eea'], ['brightMagenta', '#d670d6'], ['brightCyan', '#29b8db'], ['brightWhite', '#e5e5e5'],
];

function palette(theme: Record<string, unknown>): string[] {
  const p = BASE16.map(([k, d]) => (typeof theme[k] === 'string' ? (theme[k] as string) : d));
  const lv = [0, 95, 135, 175, 215, 255];
  const hex = (n: number) => n.toString(16).padStart(2, '0');
  for (let r = 0; r < 6; r++) for (let g = 0; g < 6; g++) for (let b = 0; b < 6; b++) p.push('#' + hex(lv[r]) + hex(lv[g]) + hex(lv[b]));
  for (let i = 0; i < 24; i++) { const v = 8 + i * 10; p.push('#' + hex(v) + hex(v) + hex(v)); }
  return p;
}

interface Cell { ch: string; w: number; fg: string; bg: string | null; bold: boolean; italic: boolean; under: boolean; dim: boolean; }

/** The terminal's visible screen as a PNG (base64, no data: prefix): the
 *  colours, bold, italic, underline and keyword highlighting as shown. */
export function screenPng(term: Terminal, keywords: HtmlKeyword[] = [], scale = 2): string {
  const theme = (term.options.theme ?? {}) as Record<string, unknown>;
  const pal = palette(theme);
  const defFg = typeof theme['foreground'] === 'string' ? (theme['foreground'] as string) : '#d4d4d4';
  const defBg = typeof theme['background'] === 'string' ? (theme['background'] as string) : '#1e1f22';
  const color = (isDefault: boolean, isPal: boolean, isRGB: boolean, v: number): string | null =>
    isDefault ? null : isPal ? (pal[v] ?? null) : isRGB ? '#' + v.toString(16).padStart(6, '0') : null;

  const fontSize = term.options.fontSize ?? 14;
  const family = term.options.fontFamily ?? 'monospace';
  const buf = term.buffer.active;
  const rows: Cell[][] = [];
  let cell: IBufferCell | undefined;
  for (let y = buf.viewportY; y < buf.viewportY + term.rows; y++) {
    const line = buf.getLine(y);
    const cells: Cell[] = [];
    for (let x = 0; line && x < term.cols; x++) {
      cell = line.getCell(x, cell);
      if (!cell || cell.getWidth() === 0) continue;
      let fg = color(cell.isFgDefault(), cell.isFgPalette(), cell.isFgRGB(), cell.getFgColor());
      let bg = color(cell.isBgDefault(), cell.isBgPalette(), cell.isBgRGB(), cell.getBgColor());
      if (cell.isBold() && cell.isFgPalette() && cell.getFgColor() < 8) fg = pal[cell.getFgColor() + 8];
      if (cell.isInverse()) { const f = fg ?? defFg; fg = bg ?? defBg; bg = f; }
      cells.push({ ch: cell.isInvisible() ? ' ' : cell.getChars() || ' ', w: cell.getWidth(), fg: fg ?? defFg, bg,
        bold: !!cell.isBold(), italic: !!cell.isItalic(), under: !!cell.isUnderline(), dim: !!cell.isDim() });
    }
    // keyword highlighting on the row's text
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
        for (let i = cellAt(m.index); i <= cellAt(m.index + m[0].length - 1); i++) { cells[i].fg = k.fg; if (k.color) cells[i].bg = k.color; }
      }
    }
    rows.push(cells);
  }

  const canvas = document.createElement('canvas');
  const ctx = canvas.getContext('2d')!;
  ctx.font = `${fontSize}px ${family}`;
  const cw = ctx.measureText('M').width;
  const lh = Math.ceil(fontSize * (term.options.lineHeight ?? 1) * 1.2);
  const pad = 14;
  canvas.width = Math.ceil((term.cols * cw + pad * 2) * scale);
  canvas.height = Math.ceil((rows.length * lh + pad * 2) * scale);
  ctx.scale(scale, scale);
  ctx.fillStyle = defBg;
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.textBaseline = 'middle';
  rows.forEach((cells, r) => {
    let x = pad;
    const y = pad + r * lh;
    for (const c of cells) {
      const w = cw * c.w;
      if (c.bg) { ctx.fillStyle = c.bg; ctx.fillRect(x, y, w + 0.5, lh); }
      if (c.ch !== ' ') {
        ctx.font = `${c.italic ? 'italic ' : ''}${c.bold ? 'bold ' : ''}${fontSize}px ${family}`;
        ctx.globalAlpha = c.dim ? 0.6 : 1;
        ctx.fillStyle = c.fg;
        ctx.fillText(c.ch, x, y + lh / 2);
        ctx.globalAlpha = 1;
      }
      if (c.under) { ctx.fillStyle = c.fg; ctx.fillRect(x, y + lh - 2, w, 1); }
      x += w;
    }
  });
  return canvas.toDataURL('image/png').replace(/^data:image\/png;base64,/, '');
}
