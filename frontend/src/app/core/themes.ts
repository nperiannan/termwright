import type { ITheme } from '@xterm/xterm';

/** Terminal color schemes (original palettes; no third-party theme data). */
export const SCHEMES: Record<string, ITheme> = {
  Default: {
    background: '#1b1c1f', foreground: '#d8dadf', cursor: '#f0c674', selectionBackground: '#3f5a85',
    black: '#1b1c1f', red: '#e06c75', green: '#98c379', yellow: '#e5c07b', blue: '#61afef',
    magenta: '#c678dd', cyan: '#56b6c2', white: '#c8ccd4',
    brightBlack: '#5c6370', brightRed: '#ff7a85', brightGreen: '#b5e890', brightYellow: '#ffd68a',
    brightBlue: '#82c4ff', brightMagenta: '#de9bf0', brightCyan: '#7fd4de', brightWhite: '#ffffff',
  },
  Classic: {
    background: '#000000', foreground: '#c0c0c0', cursor: '#c0c0c0', selectionBackground: '#404080',
    black: '#000000', red: '#c00000', green: '#00c000', yellow: '#c0c000', blue: '#0000c0',
    magenta: '#c000c0', cyan: '#00c0c0', white: '#c0c0c0',
    brightBlack: '#808080', brightRed: '#ff0000', brightGreen: '#00ff00', brightYellow: '#ffff00',
    brightBlue: '#5c5cff', brightMagenta: '#ff00ff', brightCyan: '#00ffff', brightWhite: '#ffffff',
  },
  'Green Screen': {
    background: '#0a1408', foreground: '#33ff66', cursor: '#33ff66', selectionBackground: '#1f5c2e',
  },
  Light: {
    background: '#fbfbf8', foreground: '#2a2b2e', cursor: '#2a2b2e', selectionBackground: '#b9d3f5',
    black: '#2a2b2e', red: '#c4242b', green: '#2f8132', yellow: '#946a00', blue: '#2458c4',
    magenta: '#9430a8', cyan: '#127b87', white: '#d5d6d2',
    brightBlack: '#6b6d70', brightRed: '#e0464d', brightGreen: '#3fa043', brightYellow: '#b58600',
    brightBlue: '#3f73e0', brightMagenta: '#b24ec4', brightCyan: '#1b98a6', brightWhite: '#ffffff',
  },
  'Solarized Dark': {
    background: '#002b36', foreground: '#839496', cursor: '#93a1a1', selectionBackground: '#174652',
    black: '#073642', red: '#dc322f', green: '#859900', yellow: '#b58900', blue: '#268bd2', magenta: '#d33682', cyan: '#2aa198', white: '#eee8d5',
    brightBlack: '#586e75', brightRed: '#cb4b16', brightGreen: '#93a1a1', brightYellow: '#657b83', brightBlue: '#839496', brightMagenta: '#6c71c4', brightCyan: '#93a1a1', brightWhite: '#fdf6e3',
  },
  'Solarized Light': {
    background: '#fdf6e3', foreground: '#657b83', cursor: '#586e75', selectionBackground: '#e3dcc6',
    black: '#073642', red: '#dc322f', green: '#859900', yellow: '#b58900', blue: '#268bd2', magenta: '#d33682', cyan: '#2aa198', white: '#eee8d5',
    brightBlack: '#002b36', brightRed: '#cb4b16', brightGreen: '#586e75', brightYellow: '#657b83', brightBlue: '#839496', brightMagenta: '#6c71c4', brightCyan: '#93a1a1', brightWhite: '#fdf6e3',
  },
  Dracula: {
    background: '#282a36', foreground: '#f8f8f2', cursor: '#f8f8f2', selectionBackground: '#44475a',
    black: '#21222c', red: '#ff5555', green: '#50fa7b', yellow: '#f1fa8c', blue: '#bd93f9', magenta: '#ff79c6', cyan: '#8be9fd', white: '#f8f8f2',
    brightBlack: '#6272a4', brightRed: '#ff6e6e', brightGreen: '#69ff94', brightYellow: '#ffffa5', brightBlue: '#d6acff', brightMagenta: '#ff92df', brightCyan: '#a4ffff', brightWhite: '#ffffff',
  },
  Nord: {
    background: '#2e3440', foreground: '#d8dee9', cursor: '#d8dee9', selectionBackground: '#434c5e',
    black: '#3b4252', red: '#bf616a', green: '#a3be8c', yellow: '#ebcb8b', blue: '#81a1c1', magenta: '#b48ead', cyan: '#88c0d0', white: '#e5e9f0',
    brightBlack: '#4c566a', brightRed: '#bf616a', brightGreen: '#a3be8c', brightYellow: '#ebcb8b', brightBlue: '#81a1c1', brightMagenta: '#b48ead', brightCyan: '#8fbcbb', brightWhite: '#eceff4',
  },
  'Gruvbox Dark': {
    background: '#282828', foreground: '#ebdbb2', cursor: '#ebdbb2', selectionBackground: '#504945',
    black: '#282828', red: '#cc241d', green: '#98971a', yellow: '#d79921', blue: '#458588', magenta: '#b16286', cyan: '#689d6a', white: '#a89984',
    brightBlack: '#928374', brightRed: '#fb4934', brightGreen: '#b8bb26', brightYellow: '#fabd2f', brightBlue: '#83a598', brightMagenta: '#d3869b', brightCyan: '#8ec07c', brightWhite: '#ebdbb2',
  },
  'Tomorrow Night': {
    background: '#1d1f21', foreground: '#c5c8c6', cursor: '#c5c8c6', selectionBackground: '#373b41',
    black: '#1d1f21', red: '#cc6666', green: '#b5bd68', yellow: '#f0c674', blue: '#81a2be', magenta: '#b294bb', cyan: '#8abeb7', white: '#c5c8c6',
    brightBlack: '#969896', brightRed: '#d54e53', brightGreen: '#b9ca4a', brightYellow: '#e7c547', brightBlue: '#7aa6da', brightMagenta: '#c397d8', brightCyan: '#70c0b1', brightWhite: '#ffffff',
  },
  'Catppuccin Mocha': {
    background: '#1e1e2e', foreground: '#cdd6f4', cursor: '#f5e0dc', selectionBackground: '#45475a',
    black: '#45475a', red: '#f38ba8', green: '#a6e3a1', yellow: '#f9e2af', blue: '#89b4fa', magenta: '#f5c2e7', cyan: '#94e2d5', white: '#bac2de',
    brightBlack: '#585b70', brightRed: '#f38ba8', brightGreen: '#a6e3a1', brightYellow: '#f9e2af', brightBlue: '#89b4fa', brightMagenta: '#f5c2e7', brightCyan: '#94e2d5', brightWhite: '#a6adc8',
  },
  Tango: {
    background: '#1e1e1e', foreground: '#d3d7cf', cursor: '#d3d7cf', selectionBackground: '#3d4a5c',
    black: '#2e3436', red: '#cc0000', green: '#4e9a06', yellow: '#c4a000', blue: '#3465a4', magenta: '#75507b', cyan: '#06989a', white: '#d3d7cf',
    brightBlack: '#555753', brightRed: '#ef2929', brightGreen: '#8ae234', brightYellow: '#fce94f', brightBlue: '#729fcf', brightMagenta: '#ad7fa8', brightCyan: '#34e2e2', brightWhite: '#eeeeec',
  },
};

export const SCHEME_NAMES = Object.keys(SCHEMES);

/** The editable color slots of a scheme, in display order. */
export const COLOR_KEYS = [
  'background', 'foreground', 'cursor', 'selectionBackground',
  'black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
  'brightBlack', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite',
] as const;

export interface CustomScheme { name: string; colors: Record<string, string>; }

/** A complete copy of a scheme (missing ANSI colors filled from Default). */
export function fullScheme(name: string, customs: CustomScheme[] = []): Record<string, string> {
  const base = SCHEMES['Default'] as Record<string, string>;
  const src = (customs.find(c => c.name === name)?.colors ?? SCHEMES[name] ?? base) as Record<string, string>;
  const out: Record<string, string> = {};
  for (const k of COLOR_KEYS) out[k] = src[k] ?? base[k];
  return out;
}

/** Maps color scheme names found in imported SecureCRT sessions to ours. */
const ALIASES: Record<string, string> = {
  'traditional': 'Classic', 'windows': 'Classic', 'white / black': 'Classic', 'black / white': 'Light',
  'monochrome': 'Classic', 'green / black': 'Green Screen', 'black / green': 'Green Screen',
  'floral white': 'Light', 'white / blue': 'Default', 'solarized darcula': 'Solarized Dark', 'solarized dark': 'Solarized Dark',
  'solarized light': 'Solarized Light', 'default': 'Default',
};

/** SecureCRT's stock scheme names: imported sessions with these follow the global scheme. */
const INHERIT = new Set(['traditional', 'windows', 'monochrome']);

/**
 * The scheme a terminal uses: the session's own scheme unless the global
 * "use for all sessions" option is on; "Auto" follows the UI theme.
 */
export function terminalTheme(sessionScheme: string | undefined,
  s: { terminalScheme?: string; forceTerminalScheme?: boolean; customSchemes?: CustomScheme[] | null } | null | undefined,
  lightUi: boolean): ITheme {
  let global = s?.terminalScheme || 'Auto';
  if (global === 'Auto') global = lightUi ? 'Light' : 'Default';
  const own = (sessionScheme ?? '').trim();
  const name = own && !s?.forceTerminalScheme && !INHERIT.has(own.toLowerCase()) ? own : global;
  return scheme(name, s?.customSchemes ?? []);
}

export function scheme(name: string | undefined, customs: CustomScheme[] = []): ITheme {
  const n = name ?? '';
  const custom = customs.find(c => c.name === n);
  if (custom) return { ...SCHEMES['Default'], ...custom.colors };
  return SCHEMES[n] ?? SCHEMES[ALIASES[n.toLowerCase()] ?? ''] ?? SCHEMES['Default'];
}
