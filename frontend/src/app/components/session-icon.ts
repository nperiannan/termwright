import { Component, computed, input } from '@angular/core';

/** Kinds of session icons: each has its own color and symbol so sessions are told apart at a glance. */
export type SessionKind = 'ssh' | 'sftp' | 'telnet' | 'serial' | 'console' | 'shell' | 'raw' | 'rdp' | 'vnc' | 'folder' | 'folder-open';

const KINDS: Record<Exclude<SessionKind, 'folder' | 'folder-open'>, { from: string; to: string; title: string }> = {
  ssh: { from: '#10b981', to: '#0d9488', title: 'SSH2 terminal' },
  sftp: { from: '#3b82f6', to: '#6366f1', title: 'SFTP file browser' },
  telnet: { from: '#f59e0b', to: '#ea580c', title: 'Telnet (not encrypted)' },
  serial: { from: '#a855f7', to: '#7c3aed', title: 'Serial console' },
  console: { from: '#8b5cf6', to: '#4338ca', title: 'Console server port' },
  shell: { from: '#64748b', to: '#334155', title: 'Local shell' },
  raw: { from: '#ec4899', to: '#be185d', title: 'Raw TCP' },
  rdp: { from: '#0ea5e9', to: '#1d4ed8', title: 'Remote Desktop (RDP)' },
  vnc: { from: '#14b8a6', to: '#0369a1', title: 'VNC remote screen' },
};

/**
 * Session Manager icons: a small gradient tile with a white symbol per protocol
 * (SSH ›_, SFTP ⇅, Telnet T, Serial connector, local $, raw <>) and a glassy
 * folder whose front flips open.
 */
@Component({
  selector: 'app-session-icon',
  template: `
    <svg viewBox="0 0 16 16" [attr.width]="size()" [attr.height]="size()" aria-hidden="true">
      <defs>
        <linearGradient [attr.id]="gid()" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" [style.stop-color]="from()" />
          <stop offset="1" [style.stop-color]="to()" />
        </linearGradient>
      </defs>
      @switch (kind()) {
        @case ('folder') {
          <path d="M1.5 4.2c0-.8.6-1.4 1.4-1.4h3.2l1.5 1.6h5.5c.8 0 1.4.6 1.4 1.4v6.6c0 .8-.6 1.4-1.4 1.4H2.9c-.8 0-1.4-.6-1.4-1.4z" [attr.fill]="url()" />
          <path d="M1.5 6.2h13" stroke="#fff" stroke-opacity=".45" stroke-width=".9" />
          <path d="M3.4 11.2h4.2" stroke="#fff" stroke-opacity=".8" stroke-width="1.1" stroke-linecap="round" />
        }
        @case ('folder-open') {
          <path d="M1.5 4.2c0-.8.6-1.4 1.4-1.4h3.2l1.5 1.6h5c.8 0 1.4.6 1.4 1.4v.6H4.2c-.6 0-1.1.4-1.3 1L1.5 11.6z" [attr.fill]="url()" opacity=".55" />
          <path d="M3.2 7.4c.2-.6.7-1 1.3-1h9.6c.6 0 1 .6.8 1.2l-1.6 5c-.2.6-.7.9-1.3.9H2.2c-.6 0-1-.6-.8-1.2z" [attr.fill]="url()" />
          <path d="M5 10.6h4.6" stroke="#fff" stroke-opacity=".85" stroke-width="1.1" stroke-linecap="round" />
        }
        @default {
          <rect x="1" y="1.5" width="14" height="13" rx="3.4" [attr.fill]="url()" />
          <rect x="1" y="1.5" width="14" height="6" rx="3.4" fill="#fff" fill-opacity=".14" />
          <g fill="none" stroke="#fff" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
            @switch (kind()) {
              @case ('ssh') { <path d="M4.2 5.6l2.4 2.4-2.4 2.4M8.3 10.6h3.6" /> }
              @case ('sftp') { <path d="M5.6 11.3V4.9M3.8 6.6l1.8-1.8 1.8 1.8M10.4 4.7v6.4M8.6 9.4l1.8 1.8 1.8-1.8" /> }
              @case ('telnet') { <path d="M4.6 4.9h6.8M8 4.9v6.4" /> }
              @case ('console') { <path d="M3.6 5.6h8.8l-1.2 4.8H4.8z" /><path d="M6 7.8h.01M8 7.8h.01M10 7.8h.01" stroke-width="1.7" /> }
              @case ('serial') { <path d="M3.6 5.6h8.8l-1.2 4.8H4.8z" /><path d="M6 7.8h.01M8 7.8h.01M10 7.8h.01" stroke-width="1.7" /> }
              @case ('shell') { <path d="M9.9 5.2c-.4-.5-1.1-.8-1.9-.8-1.1 0-1.9.6-1.9 1.4 0 1.9 4 1 4 3 0 .8-.9 1.5-2.1 1.5-.8 0-1.6-.4-2-.9M8 3.4v9.2" stroke-width="1.3" /> }
              @case ('raw') { <path d="M6.2 5.2L3.6 8l2.6 2.8M9.8 5.2L12.4 8l-2.6 2.8" /> }
              @case ('rdp') { <rect x="3.3" y="4" width="9.4" height="6.2" rx=".9" stroke-width="1.3" /><path d="M6.4 12.3h3.2M8 10.2v2.1" stroke-width="1.3" /><path d="M5.4 6.4l1.4 1-1.4 1" stroke-width="1.1" /> }
              @case ('vnc') { <rect x="3.3" y="4" width="9.4" height="6.2" rx=".9" stroke-width="1.3" /><path d="M6.4 12.3h3.2M8 10.2v2.1" stroke-width="1.3" /><path d="M6.6 6.2l.9 2.3 1.6-.7 1 .9" stroke-width="1" /> }
            }
          </g>
          @if (via()) { <circle cx="13.2" cy="12.7" r="2.4" fill="#f8fafc" stroke="#0f172a" stroke-width=".6" /><path d="M12 12.7h2.3M13.4 11.7l1 1-1 1" fill="none" stroke="#0f172a" stroke-width=".9" stroke-linecap="round" stroke-linejoin="round" /> }
        }
      }
    </svg>`,
  styles: [`:host { display: inline-flex; line-height: 0; flex: none; } svg { display: block; overflow: visible; }`],
  host: { '[attr.title]': 'title()' },
})
export class SessionIcon {
  readonly kind = input.required<SessionKind>();
  /** Reached through a jump station (remote command "ssh … host"): small arrow badge. */
  readonly via = input(false);
  readonly size = input(16);
  private static seq = 0;
  readonly gid = computed(() => 'tw-si-' + this.kind() + '-' + (SessionIcon.seq++));
  readonly url = computed(() => `url(#${this.gid()})`);
  private readonly spec = computed(() => {
    const k = this.kind();
    if (k === 'folder' || k === 'folder-open') return { from: 'var(--folder-from, #38bdf8)', to: 'var(--folder-to, #6366f1)', title: '' };
    return KINDS[k];
  });
  readonly from = computed(() => this.spec().from);
  readonly to = computed(() => this.spec().to);
  readonly title = computed(() => this.spec().title || null);
}
