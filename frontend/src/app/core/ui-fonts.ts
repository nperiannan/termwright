/** Interface fonts (Global Options → General). The default is the system font Termwright always used. */
export const UI_FONTS: { v: string; label: string; stack: string }[] = [
  { v: '', label: 'System (default)', stack: "system-ui, -apple-system, 'Segoe UI', Roboto, Ubuntu, sans-serif" },
  { v: 'vscode', label: 'VS Code (the font VS Code uses on each system)', stack: "-apple-system, BlinkMacSystemFont, 'Segoe WPC', 'Segoe UI', 'Ubuntu', 'Droid Sans', sans-serif" },
  { v: 'inter', label: 'Inter (wide and very readable)', stack: "'Inter Variable', 'Inter', system-ui, sans-serif" },
  { v: 'plex', label: 'IBM Plex Sans (technical, clear digits)', stack: "'IBM Plex Sans', system-ui, sans-serif" },
];

/** Interface text sizes in px; 13 is the default. */
export const UI_SIZES: { v: number; label: string }[] = [
  { v: 13, label: 'Default (13)' }, { v: 14, label: 'Medium (14)' }, { v: 15, label: 'Large (15)' },
  { v: 16, label: 'Larger (16)' }, { v: 17, label: 'Largest (17)' },
];

export function uiFontStack(v: string | undefined): string {
  return (UI_FONTS.find(f => f.v === (v ?? '')) ?? UI_FONTS[0]).stack;
}

export function uiFontSize(v: number | undefined): number {
  return v && v >= 13 && v <= 17 ? v : 13;
}
