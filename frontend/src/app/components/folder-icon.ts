import { Component, computed, input } from '@angular/core';

/** Folder colours by level: top-level folders amber, their subfolders purple,
 *  deeper folders blue, then teal; production folders (and everything in
 *  them) light red. */
export const FOLDER_COLORS = { top: '#e5a93b', sub: '#a970ff', nested: '#4f9cf7', deeper: '#14b8c6', production: '#ef6b62' };

export function folderColor(depth: number, env = ''): string {
  if (env === 'production') return FOLDER_COLORS.production;
  return depth <= 0 ? FOLDER_COLORS.top : depth === 1 ? FOLDER_COLORS.sub : depth === 2 ? FOLDER_COLORS.nested : FOLDER_COLORS.deeper;
}

/** Modern Session Manager folder: a solid folder shape in its level's colour. */
@Component({
  selector: 'app-folder-icon',
  template: `
    <svg viewBox="0 0 24 20" [attr.width]="size()" [attr.height]="size() * 20 / 24" aria-hidden="true">
      <path d="M0 5.1Q0 3.6 1.5 3.6H9.1L11.5 0H19Q24 0 24 5V14Q24 20 18 20H6Q0 20 0 14Z" [attr.fill]="color()" />
    </svg>`,
  styles: [`:host { display: inline-flex; line-height: 0; flex: none; } svg { display: block; }`],
})
export class FolderIcon {
  /** Level: 0 = top-level folder. */
  readonly depth = input(0);
  /** Environment of the folder ("production" makes it red). */
  readonly env = input('');
  readonly open = input(false);
  readonly size = input(20);
  readonly color = computed(() => folderColor(this.depth(), this.env()));
}
