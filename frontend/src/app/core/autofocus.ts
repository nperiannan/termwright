import { AfterViewInit, Directive, ElementRef, inject, input } from '@angular/core';

/** Focuses (and selects) the element when it is rendered. */
@Directive({ selector: '[appAutofocus]' })
export class Autofocus implements AfterViewInit {
  readonly appAutofocus = input<boolean | ''>(true);
  private el = inject(ElementRef<HTMLElement>);
  ngAfterViewInit() {
    if (this.appAutofocus() === false) return;
    setTimeout(() => {
      const e = this.el.nativeElement as HTMLInputElement;
      e.focus();
      if (typeof e.select === 'function') e.select();
    }, 30);
  }
}
