import { signal } from '@angular/core';

/** Folder and file icons with depth (Glacier) instead of flat shapes. Set by the app from the settings:
 *  Settings → General → Folder and file icons ("" = as the style: glass in Glacier, flat in Standard). */
export const glacierIcons = signal(false);
