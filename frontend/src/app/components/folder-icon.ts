import { Component, computed, input } from '@angular/core';

/** Folder colours: picked from the folder's name, so a folder keeps its colour. */
const PALETTE = ['#4f9cf7', '#22b07d', '#e5a93b', '#a970ff', '#ef6b62', '#14b8c6', '#e66bb0'];

export function folderColor(name: string): string {
  let h = 0;
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return PALETTE[h % PALETTE.length];
}

/** Modern Session Manager folder: a solid folder in the folder's colour; the
 *  front flap tips forward when the folder is open. */
@Component({
  selector: 'app-folder-icon',
  template: `
    <svg viewBox="0 0 24 20" [attr.width]="size()" [attr.height]="size() * 20 / 24" aria-hidden="true">
      <path d="M1.5 3.2A1.7 1.7 0 0 1 3.2 1.5h5.6c.5 0 .9.2 1.2.5l1.5 1.6h9.3a1.7 1.7 0 0 1 1.7 1.7v12.5a1.7 1.7 0 0 1-1.7 1.7H3.2a1.7 1.7 0 0 1-1.7-1.7z" [attr.fill]="color()" />
      @if (open()) {
        <path d="M4.6 8.2c.2-.6.8-1 1.4-1h16.2c.9 0 1.5.9 1.2 1.7l-2.9 8.7c-.2.6-.8 1-1.4 1H2.9c-.6 0-1-.6-.8-1.2z" fill="#fff" fill-opacity=".32" />
      } @else {
        <path d="M1.5 7.4c0-.7.6-1.3 1.3-1.3h18.4c.7 0 1.3.6 1.3 1.3v10.3a1.7 1.7 0 0 1-1.7 1.7H3.2a1.7 1.7 0 0 1-1.7-1.7z" fill="#fff" fill-opacity=".22" />
      }
    </svg>`,
  styles: [`:host { display: inline-flex; line-height: 0; flex: none; } svg { display: block; }`],
})
export class FolderIcon {
  readonly name = input('');
  readonly open = input(false);
  readonly size = input(20);
  readonly color = computed(() => folderColor(this.name()));
}
