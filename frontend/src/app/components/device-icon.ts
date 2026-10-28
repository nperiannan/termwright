import { Component, computed, inject, input } from '@angular/core';
import { Ui } from '../core/ui.service';

/** Kinds of discovered devices. */
export type DeviceKind = 'computer' | 'server' | 'nas' | 'phone' | 'mobile' | 'iot' | 'printer' | 'switch' | 'router' | 'firewall' | 'wireless' | 'unknown';

export const DEVICE_KINDS: Record<DeviceKind, { from: string; to: string; label: string }> = {
  switch: { from: '#2dd4bf', to: '#0d9488', label: 'Network switch' },
  router: { from: '#a78bfa', to: '#7c3aed', label: 'Router' },
  firewall: { from: '#fb7185', to: '#e11d48', label: 'Firewall' },
  wireless: { from: '#f472b6', to: '#db2777', label: 'Wireless access point' },
  server: { from: '#94a3b8', to: '#475569', label: 'Server (Linux/Unix)' },
  computer: { from: '#60a5fa', to: '#2563eb', label: 'Computer' },
  nas: { from: '#fcd34d', to: '#d97706', label: 'NAS / storage' },
  phone: { from: '#4ade80', to: '#16a34a', label: 'IP phone' },
  mobile: { from: '#22d3ee', to: '#0891b2', label: 'Mobile phone / tablet' },
  iot: { from: '#a3e635', to: '#65a30d', label: 'IoT / embedded board' },
  printer: { from: '#fdba74', to: '#ea580c', label: 'Printer' },
  unknown: { from: '#cbd5e1', to: '#64748b', label: 'Unknown device' },
};

/** Device shapes on a 24 grid: a solid body, cut-outs (filled), lines, status lights. */
interface Glyph { body: string; cut?: string; cutLine?: string; line?: string; leds?: [number, number][]; }
const GLYPHS: Record<DeviceKind, Glyph> = {
  switch: { body: 'M3 8.5A2.5 2.5 0 0 1 5.5 6h13A2.5 2.5 0 0 1 21 8.5v7a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 15.5z',
    cut: 'M5.8 12.6h1.8v2H5.8zM8.9 12.6h1.8v2H8.9zM12 12.6h1.8v2H12zM15.1 12.6h1.8v2h-1.8z', cutLine: 'M15 9.2h3', leds: [[6.4, 9.2], [8.4, 9.2]] },
  router: { body: 'M3 13.5A2.5 2.5 0 0 1 5.5 11h13a2.5 2.5 0 0 1 2.5 2.5v3a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 16.5zM6 4.6a1 1 0 1 1 2 0V11H6zM16 4.6a1 1 0 1 1 2 0V11h-2z',
    cutLine: 'M15.5 15h2', leds: [[7, 15], [9.6, 15], [12.2, 15]] },
  firewall: { body: 'M12 2.8 19.5 5.6v5.6c0 4.4-3 7.8-7.5 9.6-4.5-1.8-7.5-5.2-7.5-9.6V5.6z',
    cutLine: 'M6.6 9.6h10.8M7.2 13.4h9.6M12 9.6v3.8M9.4 6.4v3.2M14.6 6.4v3.2M9.6 13.4v3.4M14.4 13.4v3.4' },
  wireless: { body: 'M4 16.2c0-1.5 3.6-2.7 8-2.7s8 1.2 8 2.7-3.6 2.8-8 2.8-8-1.3-8-2.8z',
    line: 'M8.2 10.2a5.4 5.4 0 0 1 7.6 0M5.6 7.6a9 9 0 0 1 12.8 0', leds: [[12, 16.2]] },
  server: { body: 'M4 6.5A2.5 2.5 0 0 1 6.5 4h11A2.5 2.5 0 0 1 20 6.5v2A2.5 2.5 0 0 1 17.5 11h-11A2.5 2.5 0 0 1 4 8.5zM4 15.5A2.5 2.5 0 0 1 6.5 13h11a2.5 2.5 0 0 1 2.5 2.5v2a2.5 2.5 0 0 1-2.5 2.5h-11A2.5 2.5 0 0 1 4 17.5z',
    cutLine: 'M11 7.5h5.6M11 16.5h5.6', leds: [[7.4, 7.5], [7.4, 16.5]] },
  computer: { body: 'M3 6.5A2.5 2.5 0 0 1 5.5 4h13A2.5 2.5 0 0 1 21 6.5v7a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 13.5z',
    cut: 'M5.4 6.4h13.2v7.2H5.4z', line: 'M9 20h6M12 16v4' },
  nas: { body: 'M5 5.5A2.5 2.5 0 0 1 7.5 3h9A2.5 2.5 0 0 1 19 5.5v13a2.5 2.5 0 0 1-2.5 2.5h-9A2.5 2.5 0 0 1 5 18.5z',
    cut: 'M8 6h8v2.6H8zM8 10.4h8V13H8z', leds: [[12, 17]] },
  phone: { body: 'M4 11.5A2.5 2.5 0 0 1 6.5 9h11a2.5 2.5 0 0 1 2.5 2.5v6a2.5 2.5 0 0 1-2.5 2.5h-11A2.5 2.5 0 0 1 4 17.5zM5 7.4C5 5 8 3.6 12 3.6s7 1.4 7 3.8V8h-3.2V6.8c-1-.5-2.3-.7-3.8-.7s-2.8.2-3.8.7V8H5z',
    cut: 'M7.4 12.4h2v1.6h-2zM11 12.4h2v1.6h-2zM14.6 12.4h2v1.6h-2zM7.4 15.6h2v1.6h-2zM11 15.6h2v1.6h-2zM14.6 15.6h2v1.6h-2z' },
  mobile: { body: 'M8 2h8a2.5 2.5 0 0 1 2.5 2.5v15A2.5 2.5 0 0 1 16 22H8a2.5 2.5 0 0 1-2.5-2.5v-15A2.5 2.5 0 0 1 8 2z',
    cut: 'M7.6 4.6h8.8v12.6H7.6z', leds: [[12, 19.5]] },
  iot: { body: 'M7.5 5h9A2.5 2.5 0 0 1 19 7.5v9a2.5 2.5 0 0 1-2.5 2.5h-9A2.5 2.5 0 0 1 5 16.5v-9A2.5 2.5 0 0 1 7.5 5z',
    cut: 'M9.5 9.5h5v5h-5z', line: 'M9 2v3M12 2v3M15 2v3M9 19v3M12 19v3M15 19v3M2 9h3M2 12h3M2 15h3M19 9h3M19 12h3M19 15h3' },
  printer: { body: 'M7 3h10v4.6H7zM3 11.5A2.5 2.5 0 0 1 5.5 9h13a2.5 2.5 0 0 1 2.5 2.5v4a2.5 2.5 0 0 1-2.5 2.5H18v-3H6v3h-.5A2.5 2.5 0 0 1 3 15.5zM7.4 16.4h9.2V21H7.4z',
    leds: [[17.6, 11.8]] },
  unknown: { body: 'M12 3a9 9 0 1 1 0 18 9 9 0 0 1 0-18z',
    cutLine: 'M9.6 9.6a2.5 2.5 0 1 1 3.4 2.3c-.6.3-1 .8-1 1.5v.4', leds: [[12, 16.8]] },
};

/** The kind of device a session's device type names ("VOSS" → switch,
 *  "Linux" → server); '' when the name says nothing about it. */
export function deviceKindOf(deviceType: string | undefined): DeviceKind | '' {
  const t = (deviceType ?? '').toLowerCase();
  if (!t) return '';
  if (/firewall|fortigate|fortios|palo|pan-?os|asa\b|pfsense|opnsense|srx|checkpoint|sonicwall/.test(t)) return 'firewall';
  if (/wireless|wi-?fi|\bap\b|access point|wing|aruba ?os|unifi/.test(t)) return 'wireless';
  if (/router|ios-?xr|junos|mikrotik|routeros|vyos|edgeos|isr|asr/.test(t)) return 'router';
  if (/voss|exos|boss|switch|nx-?os|\bios\b|eos|cumulus|sonic|fabric|extreme|cisco|arista|aruba|procurve|comware|dell ?os|icx|fastiron|slx|netiron/.test(t)) return 'switch';
  if (/linux|ubuntu|debian|centos|rhel|red hat|fedora|alpine|unix|bsd|solaris|aix|server|esxi/.test(t)) return 'server';
  if (/windows|macos|mac os|desktop|laptop/.test(t)) return 'computer';
  if (/nas|synology|qnap|truenas|storage/.test(t)) return 'nas';
  if (/printer/.test(t)) return 'printer';
  if (/phone|voip/.test(t)) return 'phone';
  if (/iot|raspberry|embedded|esp32|arduino/.test(t)) return 'iot';
  return '';
}

/** Device icons: a vivid squircle with the device in white (default), or
 *  just the device in its colour (View → Glyph Icons). */
@Component({
  selector: 'app-device-icon',
  template: `
    <svg viewBox="0 0 24 24" [attr.width]="size()" [attr.height]="size()" aria-hidden="true">
      @if (!glyph()) {
        <defs>
          <linearGradient [attr.id]="gid()" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" [style.stop-color]="spec().from" />
            <stop offset="1" [style.stop-color]="spec().to" />
          </linearGradient>
        </defs>
        <path d="M12 .4C3 .4.4 3 .4 12S3 23.6 12 23.6 23.6 21 23.6 12 21 .4 12 .4z" [attr.fill]="url()" />
        <g transform="translate(3.6 3.6) scale(.7)">
          <path [attr.d]="g().body" fill="#fff" />
          @if (g().cut) { <path [attr.d]="g().cut" [attr.fill]="spec().to" fill-opacity=".85" /> }
          @if (g().cutLine) { <path [attr.d]="g().cutLine" fill="none" [attr.stroke]="spec().to" stroke-width="1.6" stroke-linecap="round" /> }
          @if (g().line) { <path [attr.d]="g().line" fill="none" stroke="#fff" stroke-width="1.9" stroke-linecap="round" /> }
          @for (l of g().leds ?? []; track $index) { <circle [attr.cx]="l[0]" [attr.cy]="l[1]" r="1.05" fill="#22c55e" /> }
        </g>
      } @else {
        <g transform="translate(1 1) scale(.917)">
          <path [attr.d]="g().body" [attr.fill]="tone()" fill-opacity=".26" [attr.stroke]="tone()" stroke-width="1.6" stroke-linejoin="round" />
          @if (g().cut) { <path [attr.d]="g().cut" [attr.fill]="tone()" /> }
          @if (g().cutLine) { <path [attr.d]="g().cutLine" fill="none" [attr.stroke]="tone()" stroke-width="1.6" stroke-linecap="round" /> }
          @if (g().line) { <path [attr.d]="g().line" fill="none" [attr.stroke]="tone()" stroke-width="1.9" stroke-linecap="round" /> }
          @for (l of g().leds ?? []; track $index) { <circle [attr.cx]="l[0]" [attr.cy]="l[1]" r="1.05" fill="#22c55e" /> }
        </g>
      }
    </svg>`,
  styles: [`:host { display: inline-flex; line-height: 0; flex: none; } svg { display: block; }`],
  host: { '[attr.title]': 'spec().label' },
})
export class DeviceIcon {
  private ui = inject(Ui);
  readonly kind = input<string>('unknown');
  readonly size = input(18);
  private static seq = 0;
  readonly gid = computed(() => 'tw-di-' + this.kind() + '-' + DeviceIcon.seq++);
  readonly url = computed(() => `url(#${this.gid()})`);
  readonly spec = computed(() => DEVICE_KINDS[this.kind() as DeviceKind] ?? DEVICE_KINDS.unknown);
  readonly g = computed(() => GLYPHS[this.kind() as DeviceKind] ?? GLYPHS.unknown);
  /** View → Glyph Icons: no tile. */
  readonly glyph = computed(() => this.ui.settings()?.iconStyle === 'glyph');
  /** Glyph colour: the lighter end on a dark theme, the darker on a light one. */
  readonly tone = computed(() => (this.ui.lightUi() ? this.spec().to : this.spec().from));
}
