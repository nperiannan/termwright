import { bootstrapApplication } from '@angular/platform-browser';
import { appConfig } from './app/app.config';
import { App } from './app/app';

// Text is sent exactly as typed: user names, passwords, prompts and commands
// are case-sensitive, so the operating system must not capitalise the first
// letter, correct spelling or replace quotes and dashes in any input field.
function plainInput(el: Element) {
  if (!(el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement)) return;
  el.setAttribute('autocapitalize', 'off');
  el.setAttribute('autocorrect', 'off');
  el.setAttribute('autocomplete', el.getAttribute('autocomplete') || 'off');
  el.spellcheck = false;
}
new MutationObserver(ms => ms.forEach(m => m.addedNodes.forEach(n => {
  if (n instanceof Element) { plainInput(n); n.querySelectorAll('input, textarea').forEach(plainInput); }
}))).observe(document.documentElement, { childList: true, subtree: true });

bootstrapApplication(App, appConfig)
  .catch((err) => console.error(err));
