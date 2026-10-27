import { Component, computed, input } from '@angular/core';

// Simple stroke icons drawn for Termwright (24x24 viewBox).
const PATHS: Record<string, string> = {
  plus: 'M12 5v14M5 12h14',
  'folder-plus': 'M3 6h6l2 2h10v11H3zM12 11v6M9 14h6',
  folder: 'M3 6h6l2 2h10v11H3z',
  'folder-open': 'M3 6h6l2 2h8v3M3 19l3-8h16l-3 8z',
  refresh: 'M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7',
  ssh: 'M4 5h16v14H4zM7 10l3 2-3 2M12 15h5',
  telnet: 'M4 5h16v14H4zM8 9h8M12 9v7',
  serial: 'M5 8h14l-2 8H7zM9 11h.01M12 11h.01M15 11h.01M10 14h.01M14 14h.01',
  shell: 'M4 17l6-5-6-5M12 19h8',
  connect: 'M8 7l-5 5 5 5M3 12h12M14 4h6v16h-6',
  quick: 'M13 3L5 14h6l-1 7 8-11h-6z',
  reconnect: 'M4 12a8 8 0 0 1 14-5.3L20 9M20 4v5h-5M20 12a8 8 0 0 1-14 5.3L4 15M4 20v-5h5',
  disconnect: 'M18 6L6 18M6 6l12 12',
  copy: 'M9 9h11v11H9zM5 15H4V4h11v1',
  paste: 'M9 4h6v3H9zM7 5H5v15h14V5h-2',
  find: 'M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14zM21 21l-5-5',
  // eraser
  clear: 'M7 20h13M4.6 15.4l8.8-8.8a2 2 0 0 1 2.8 0l2.2 2.2a2 2 0 0 1 0 2.8L11 19.1a3 3 0 0 1-2.1.9H7.5a2 2 0 0 1-1.4-.6l-1.5-1.5a2 2 0 0 1 0-2.5zM9 11l5 5',
  // arrow down onto a line: follow new output
  autoscroll: 'M12 4v11M7 10l5 5 5-5M5 20h14',
  log: 'M6 3h9l4 4v14H6zM14 3v5h5M9 13h7M9 17h7',
  options: 'M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1-2 2-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21h-3v-.2a1.7 1.7 0 0 0-1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1-2-2 .1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3v-3h.2a1.7 1.7 0 0 0 1.5-1 1.7 1.7 0 0 0-.3-1.8l-.1-.1 2-2 .1.1a1.7 1.7 0 0 0 1.8.3H8a1.7 1.7 0 0 0 1-1.5V3h3v.2a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1 2 2-.1.1a1.7 1.7 0 0 0-.3 1.8V8a1.7 1.7 0 0 0 1.5 1H21v3h-.2a1.7 1.7 0 0 0-1.4 1z',
  tree: 'M4 4h6v5H4zM14 15h6v5h-6zM14 4h6v5h-6zM7 9v8.5h7M10 6.5h4',
  command: 'M3 15h18v5H3zM6 17.5h8',
  buttons: 'M3 7h8v5H3zM13 7h8v5h-8zM3 14h8v5H3z',
  close: 'M6 6l12 12M18 6L6 18',
  import: 'M12 3v12M7 10l5 5 5-5M4 19h16',
  export: 'M12 15V3M7 8l5-5 5 5M4 19h16',
  up: 'M12 19V5M6 11l6-6 6 6',
  down: 'M12 5v14M6 13l6 6 6-6',
  file: 'M6 3h8l4 4v14H6zM14 3v4h4',
  sftp: 'M4 7h16v12H4zM4 7l2-3h5l2 3M9 13l3-3 3 3M12 10v6',
  script: 'M8 4h11v13a3 3 0 0 1-3 3H6a2 2 0 0 1-2-2v-2h11v2M8 4a2 2 0 0 0-2 2v10M11 9h5M11 12h5',
  record: 'M12 7a5 5 0 1 0 0 10 5 5 0 0 0 0-10z',
  tabs: 'M3 6h7v3H3zM10 6h4v3M3 9h18v11H3z',
  'tile-v': 'M3 4h18v16H3zM12 4v16',
  'tile-h': 'M3 4h18v16H3zM3 12h18',
  grid: 'M3 4h18v16H3zM12 4v16M3 12h18',
  lock: 'M6 11h12v9H6zM8.5 11V8a3.5 3.5 0 0 1 7 0v3',
  trash: 'M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3',
  'dock-left': 'M3 4h18v16H3zM9 4v16M16 9l-3 3 3 3',
  'dock-right': 'M3 4h18v16H3zM15 4v16M8 9l3 3-3 3',
  'hide-panel': 'M3 4h18v16H3zM9 4v16M5.5 9l1.5 3-1.5 3',
  pulse: 'M3 12h4l3-7 4 14 3-7h4',
  star: 'M12 3l2.8 5.7 6.2.9-4.5 4.4 1 6.2L12 17.3 6.5 20.2l1-6.2L3 9.6l6.2-.9z',
};

@Component({
  selector: 'app-icon',
  template: `<svg viewBox="0 0 24 24" [attr.width]="size()" [attr.height]="size()" fill="none" stroke="currentColor"
    stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path [attr.d]="d()" /></svg>`,
  styles: [`:host { display: inline-flex; line-height: 0; } svg { display: block; }`],
})
export class Icon {
  readonly name = input.required<string>();
  readonly size = input<number>(16);
  readonly d = computed(() => PATHS[this.name()] ?? '');
}
