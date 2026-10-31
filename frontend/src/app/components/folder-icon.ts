import { Component, computed, input } from '@angular/core';

import { envInfo } from '../core/env';

/** Folder colours by level: the top level and five levels of subfolders (deeper ones keep the last).
 *  A folder in an environment - its own or a parent folder's - takes the environment's colour instead;
 *  the environment colours (core/env.ts) are none of these. */
export const FOLDER_LEVEL_COLORS = ['#e5a93b', '#a970ff', '#4f9cf7', '#14b8c6', '#ec4899', '#a3e635'];

export function folderColor(depth: number, env = ''): string {
  const e = envInfo(env);
  if (e) return e.color;
  return FOLDER_LEVEL_COLORS[Math.max(0, Math.min(depth, FOLDER_LEVEL_COLORS.length - 1))];
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
  /** Environment of the folder (its own or a parent's): its colour. */
  readonly env = input('');
  readonly open = input(false);
  readonly size = input(20);
  readonly color = computed(() => folderColor(this.depth(), this.env()));
}
