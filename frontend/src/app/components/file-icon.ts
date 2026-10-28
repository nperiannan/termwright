import { Component, computed, input } from '@angular/core';

/** File categories by extension, each with its own color and mark. */
const GROUPS: { re: RegExp; from: string; to: string; mark: string; title: string }[] = [
  { re: /\.(cfg|conf|config|ini|txt|log|xml|json|ya?ml|toml|csv|md|properties)$/i, from: '#60a5fa', to: '#2563eb', mark: 'lines', title: 'Text / configuration' },
  { re: /\.(zip|tar|gz|tgz|bz2|xz|7z|rar|zst)$/i, from: '#fbbf24', to: '#d97706', mark: 'zip', title: 'Archive' },
  { re: /\.(png|jpe?g|gif|svg|bmp|webp|ico)$/i, from: '#f472b6', to: '#db2777', mark: 'image', title: 'Image' },
  { re: /\.(voss|img|bin|iso|exe|msi|deb|rpm|dmg|pkg|apk|xos|lst|tar\.z)$/i, from: '#a78bfa', to: '#7c3aed', mark: 'chip', title: 'Firmware / binary' },
  { re: /\.(sh|bash|zsh|py|js|ts|go|pl|rb|ps1|bat|cmd|vbs|lua|tcl|exp)$/i, from: '#34d399', to: '#059669', mark: 'code', title: 'Script' },
  { re: /\.(pub|pem|key|crt|cer|ppk|p12|pfx|csr)$|(^|\/)(id_\w+|authorized_keys|known_hosts)$/i, from: '#f87171', to: '#dc2626', mark: 'key', title: 'Key / certificate' },
];

/** File browser icons: a folded page in the file type's color. */
@Component({
  selector: 'app-file-icon',
  template: `
    @if (badge()) {
      <span class="fb" [style.background]="b().bg" [style.opacity]="hidden() ? .7 : 1">{{ b().label }}</span>
    } @else {
    <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" [style.opacity]="hidden() ? .6 : 1">
      <defs>
        <linearGradient [attr.id]="gid" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" [style.stop-color]="g().from" />
          <stop offset="1" [style.stop-color]="g().to" />
        </linearGradient>
      </defs>
      <path d="M3.5 1.5h6l3.5 3.5v8.6c0 .5-.4.9-.9.9H3.5c-.5 0-.9-.4-.9-.9V2.4c0-.5.4-.9.9-.9z" [attr.fill]="'url(#' + gid + ')'" />
      <path d="M9.5 1.5V4.1c0 .5.4.9.9.9H13" fill="#fff" fill-opacity=".35" />
      <g fill="none" stroke="#fff" stroke-width="1.1" stroke-linecap="round" stroke-linejoin="round">
        @switch (g().mark) {
          @case ('lines') { <path d="M5 8h5.5M5 10.2h5.5M5 12.4h3.5" /> }
          @case ('zip') { <path d="M7.5 6.5v1M7.5 9v1M6.5 11.2h2v1.6h-2z" /> }
          @case ('image') { <path d="M4.8 12.6l2.2-2.6 1.6 1.7 1.2-1.2 1.4 2.1" /><circle cx="6.4" cy="7.8" r=".9" /> }
          @case ('chip') { <rect x="5.4" y="7.6" width="4.4" height="4.4" rx=".6" /><path d="M7.6 6.6v1M7.6 12v1M4.4 9.8h1M9.8 9.8h1" /> }
          @case ('code') { <path d="M6.3 8.2L4.7 9.9l1.6 1.7M9 8.2l1.6 1.7L9 11.6" /> }
          @case ('key') { <circle cx="6.2" cy="10" r="1.4" /><path d="M7.6 10h3.2M9.6 10v1.3" /> }
          @default { <path d="M5 9h5.5M5 11.4h3.5" stroke-opacity=".75" /> }
        }
      </g>
      @if (link()) { <path d="M2.2 14.6c0-1.6 1.1-2.6 2.8-2.6h1M5 10.9l1.1 1.1L5 13.1" fill="none" stroke="var(--text)" stroke-width="1.1" stroke-linecap="round" stroke-linejoin="round" /> }
    </svg>
    }`,
  styles: [`:host { display: inline-flex; line-height: 0; flex: none; } svg { display: block; overflow: visible; }
    .fb { width: 20px; height: 20px; border-radius: 5px; display: grid; place-items: center; color: #fff; font: 700 7.5px/1 var(--ui-font, system-ui), sans-serif; letter-spacing: .02em; }`],
  host: { '[attr.title]': 'g().title' },
})
export class FileIcon {
  readonly name = input.required<string>();
  readonly link = input(false);
  /** A small coloured square with the file type (CFG, LOG, IMG…), as in the modern layout. */
  readonly badge = input(false);
  readonly b = computed(() => badgeOf(this.name()));
  private static seq = 0;
  readonly gid = 'tw-fi-' + FileIcon.seq++;
  readonly hidden = computed(() => this.name().startsWith('.'));
  readonly g = computed(() => GROUPS.find(x => x.re.test(this.name())) ?? { from: '#94a3b8', to: '#64748b', mark: '', title: 'File' });
}

/** Badge colour and label for a file name: the extension (up to three
 *  letters), or IMG / KEY / PIC for firmware, keys and pictures. */
export function badgeOf(name: string): { bg: string; label: string } {
  const ext = (/\.([a-z0-9]+)$/i.exec(name)?.[1] ?? '').toLowerCase();
  const up = (ext || 'file').slice(0, 3).toUpperCase();
  if (/^(log|out)$/.test(ext)) return { bg: '#64748b', label: 'LOG' };
  if (/^(txt|md|csv|rtf)$/.test(ext)) return { bg: '#2563eb', label: up };
  if (/^(cfg|conf|config|ini|xml|json|ya?ml|toml|properties|bak)$/.test(ext)) return { bg: '#0d9488', label: up };
  if (/^(voss|img|bin|iso|exe|msi|deb|rpm|dmg|pkg|apk|xos|lst|tgz)$/.test(ext) && !/^(tgz)$/.test(ext)) return { bg: '#7c3aed', label: 'IMG' };
  if (/^(zip|tar|gz|tgz|bz2|xz|7z|rar|zst)$/.test(ext)) return /voss|exos|xos|fw|firmware|image/i.test(name) ? { bg: '#7c3aed', label: 'IMG' } : { bg: '#d97706', label: up };
  if (/^(png|jpe?g|gif|svg|bmp|webp|ico)$/.test(ext)) return { bg: '#db2777', label: 'PIC' };
  if (/^(sh|bash|zsh|py|js|ts|go|pl|rb|ps1|bat|cmd|vbs|lua|tcl|exp)$/.test(ext)) return { bg: '#059669', label: up };
  if (/^(pub|pem|key|crt|cer|ppk|p12|pfx|csr)$/.test(ext) || /(^|\/)(id_\w+|authorized_keys|known_hosts)$/i.test(name)) return { bg: '#dc2626', label: 'KEY' };
  return { bg: '#6b7280', label: ext ? up : '' };
}
