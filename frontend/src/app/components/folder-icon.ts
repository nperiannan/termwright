import { Component, computed, input } from '@angular/core';

import { envInfo } from '../core/env';
import { glacierIcons } from '../core/icon-finish';

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
    @if (glass()) {
      <svg viewBox="0 0 24 20" [attr.width]="size()" [attr.height]="size() * 20 / 24" aria-hidden="true" class="gl">
        <defs>
          <linearGradient [attr.id]="id + 'b'" x1="0" y1="0" x2="0" y2="1"><stop offset="0" [attr.stop-color]="color()" stop-opacity=".95" /><stop offset="1" [attr.stop-color]="color()" stop-opacity=".72" /></linearGradient>
          <linearGradient [attr.id]="id + 'f'" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#fff" stop-opacity=".55" /><stop offset=".2" stop-color="#fff" stop-opacity=".12" /><stop offset="1" stop-color="#000" stop-opacity=".12" /></linearGradient>
        </defs>
        <path d="M0 4.6Q0 2.2 2.4 2.2H8.6L10.8 0H20.6Q24 0 24 3.4V15Q24 20 19 20H5Q0 20 0 15Z" [attr.fill]="'url(#' + id + 'b)'" />
        <path [attr.d]="flap()" [attr.fill]="color()" />
        <path [attr.d]="flap()" [attr.fill]="'url(#' + id + 'f)'" />
        <path [attr.d]="open() ? 'M2.6 7.6H21.4' : 'M2.4 5.6H21.6'" stroke="#fff" stroke-opacity=".7" stroke-width=".8" stroke-linecap="round" />
      </svg>
    } @else {
    <svg viewBox="0 0 24 20" [attr.width]="size()" [attr.height]="size() * 20 / 24" aria-hidden="true">
      <path d="M0 5.1Q0 3.6 1.5 3.6H9.1L11.5 0H19Q24 0 24 5V14Q24 20 18 20H6Q0 20 0 14Z" [attr.fill]="color()" />
    </svg>
    }`,
  styles: [`:host { display: inline-flex; line-height: 0; flex: none; } svg { display: block; } svg.gl { filter: drop-shadow(0 1.5px 2px rgba(0, 0, 0, .3)); }`],
})
export class FolderIcon {
  /** Level: 0 = top-level folder. */
  readonly depth = input(0);
  /** Environment of the folder (its own or a parent's): its colour. */
  readonly env = input('');
  readonly open = input(false);
  readonly size = input(20);
  readonly color = computed(() => folderColor(this.depth(), this.env()));
  /** Glacier: a back plate and a front flap with a light edge (lower when the folder is open). */
  readonly glass = glacierIcons;
  private static seq = 0;
  readonly id = 'tw-fo-' + FolderIcon.seq++;
  readonly flap = computed(() => this.open()
    ? 'M0 9Q0 7 2.6 7H22Q24 7 24 9V15.4Q24 20 19.4 20H4.6Q0 20 0 15.4Z'
    : 'M0 7Q0 5 2.4 5H21.6Q24 5 24 7.4V15.4Q24 20 19.4 20H4.6Q0 20 0 15.4Z');
}
