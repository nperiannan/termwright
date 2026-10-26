import { Component, computed, input } from '@angular/core';

/** Kinds of discovered devices. */
export type DeviceKind = 'computer' | 'server' | 'nas' | 'phone' | 'printer' | 'switch' | 'router' | 'firewall' | 'wireless' | 'unknown';

export const DEVICE_KINDS: Record<DeviceKind, { from: string; to: string; label: string }> = {
  switch: { from: '#14b8a6', to: '#0f766e', label: 'Network switch' },
  router: { from: '#8b5cf6', to: '#6d28d9', label: 'Router' },
  firewall: { from: '#ef4444', to: '#b91c1c', label: 'Firewall' },
  wireless: { from: '#ec4899', to: '#be185d', label: 'Wireless access point' },
  server: { from: '#64748b', to: '#334155', label: 'Server (Linux/Unix)' },
  computer: { from: '#0ea5e9', to: '#2563eb', label: 'Computer' },
  nas: { from: '#eab308', to: '#a16207', label: 'NAS / storage' },
  phone: { from: '#22c55e', to: '#15803d', label: 'IP phone' },
  printer: { from: '#f97316', to: '#c2410c', label: 'Printer' },
  unknown: { from: '#94a3b8', to: '#64748b', label: 'Unknown device' },
};

/** Device icons for discovery results: a gradient tile with a white symbol per kind. */
@Component({
  selector: 'app-device-icon',
  template: `
    <svg viewBox="0 0 16 16" [attr.width]="size()" [attr.height]="size()" aria-hidden="true">
      <defs>
        <linearGradient [attr.id]="gid()" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" [style.stop-color]="spec().from" />
          <stop offset="1" [style.stop-color]="spec().to" />
        </linearGradient>
      </defs>
      <rect x=".8" y=".8" width="14.4" height="14.4" rx="3.6" [attr.fill]="url()" />
      <rect x=".8" y=".8" width="14.4" height="6.4" rx="3.6" fill="#fff" fill-opacity=".14" />
      <g fill="none" stroke="#fff" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round">
        @switch (kind()) {
          @case ('computer') { <rect x="3.4" y="3.8" width="9.2" height="6.2" rx=".8" /><path d="M6.4 12.3h3.2M8 10v2.3" /> }
          @case ('server') { <rect x="3.8" y="3.4" width="8.4" height="3.8" rx=".8" /><rect x="3.8" y="8.8" width="8.4" height="3.8" rx=".8" /><path d="M5.7 5.3h.01M5.7 10.7h.01M8 5.3h2.4M8 10.7h2.4" /> }
          @case ('nas') { <rect x="3.6" y="3.2" width="8.8" height="9.6" rx="1" /><path d="M5.4 5.8h5.2M5.4 8h5.2M5.4 10.2h2.4" /><path d="M10.4 10.2h.01" stroke-width="1.6" /> }
          @case ('phone') { <path d="M3.6 6.4c0-1.4 2-2.4 4.4-2.4s4.4 1 4.4 2.4" /><rect x="4.6" y="7.4" width="6.8" height="4.8" rx="1" /><path d="M6.6 9.2h.01M8 9.2h.01M9.4 9.2h.01M6.6 10.6h.01M8 10.6h.01M9.4 10.6h.01" stroke-width="1.3" /> }
          @case ('printer') { <path d="M5 6V3.6h6V6" /><rect x="3.2" y="6" width="9.6" height="4.6" rx=".9" /><path d="M5.2 9.2h5.6v3.4H5.2z" /> }
          @case ('switch') { <rect x="2.6" y="5.4" width="10.8" height="5.2" rx=".9" /><path d="M4.6 8h.01M6.4 8h.01M8.2 8h.01M10 8h.01M11.6 8h.01" stroke-width="1.6" /> }
          @case ('router') { <circle cx="8" cy="8" r="4.8" /><path d="M5.6 6.7h4.6l-1.3-1.3M10.4 9.3H5.8l1.3 1.3" /> }
          @case ('firewall') { <rect x="3.2" y="4" width="9.6" height="8" rx=".6" /><path d="M3.2 6.7h9.6M3.2 9.3h9.6M6.4 4v2.7M9.6 6.7v2.6M6.4 9.3V12" /> }
          @case ('wireless') { <path d="M3.9 7a5.8 5.8 0 0 1 8.2 0M5.6 8.9a3.3 3.3 0 0 1 4.8 0" /><path d="M8 11.3h.01" stroke-width="2" /> }
          @default { <path d="M6.3 6.2a1.75 1.75 0 1 1 2.5 1.6c-.5.3-.8.6-.8 1.2v.4" /><path d="M8 11.6h.01" stroke-width="1.8" /> }
        }
      </g>
    </svg>`,
  styles: [`:host { display: inline-flex; line-height: 0; flex: none; } svg { display: block; }`],
  host: { '[attr.title]': 'spec().label' },
})
export class DeviceIcon {
  readonly kind = input<string>('unknown');
  readonly size = input(18);
  private static seq = 0;
  readonly gid = computed(() => 'tw-di-' + this.kind() + '-' + DeviceIcon.seq++);
  readonly url = computed(() => `url(#${this.gid()})`);
  readonly spec = computed(() => DEVICE_KINDS[this.kind() as DeviceKind] ?? DEVICE_KINDS.unknown);
}
