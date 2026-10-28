import { Component, computed, input } from '@angular/core';

/** Folder colours: picked from the folder's name, so a folder keeps its colour. */
const PALETTE = ['#4f9cf7', '#22b07d', '#e5a93b', '#a970ff', '#ef6b62', '#14b8c6', '#e66bb0'];

export function folderColor(name: string): string {
  let h = 0;
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return PALETTE[h % PALETTE.length];
}

/** Modern Session Manager folder: a solid folder shape in the folder's colour. */
@Component({
  selector: 'app-folder-icon',
  template: `
    <svg viewBox="0 0 24 20" [attr.width]="size()" [attr.height]="size() * 20 / 24" aria-hidden="true">
      <path d="M0 5.1Q0 3.6 1.5 3.6H9.1L11.5 0H19Q24 0 24 5V14Q24 20 18 20H6Q0 20 0 14Z" [attr.fill]="color()" />
    </svg>`,
  styles: [`:host { display: inline-flex; line-height: 0; flex: none; } svg { display: block; }`],
})
export class FolderIcon {
  readonly name = input('');
  readonly open = input(false);
  readonly size = input(20);
  readonly color = computed(() => folderColor(this.name()));
}
