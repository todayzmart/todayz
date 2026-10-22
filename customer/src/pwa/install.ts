/**
 * The browser's "Install app" prompt, held back until the customer has placed their 2nd order
 * (06-screens.md, customer app global: "install prompt after the 2nd order (not before)").
 *
 * main.tsx calls captureInstallPrompt() once at startup: the browser's own mini-infobar is
 * suppressed and the event is kept here. The WP that builds the prompt reads getDeferredPrompt()
 * after the 2nd order, calls prompt() once, then clearDeferredPrompt() (an event can prompt once).
 */

/** Chromium's beforeinstallprompt event; not in lib.dom. */
export interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<void>;
  readonly userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>;
}

let deferred: BeforeInstallPromptEvent | null = null;

/** Starts holding back the install prompt. Returns a function that stops listening. */
export function captureInstallPrompt(target: Window = window): () => void {
  const onPrompt = (event: Event): void => {
    event.preventDefault();
    deferred = event as BeforeInstallPromptEvent;
  };
  const onInstalled = (): void => {
    deferred = null;
  };
  target.addEventListener('beforeinstallprompt', onPrompt);
  target.addEventListener('appinstalled', onInstalled);
  return () => {
    target.removeEventListener('beforeinstallprompt', onPrompt);
    target.removeEventListener('appinstalled', onInstalled);
  };
}

/** The held-back prompt, or null if the browser has not offered one (or the app is installed). */
export function getDeferredPrompt(): BeforeInstallPromptEvent | null {
  return deferred;
}

/** Forgets the prompt after it has been shown; the same event cannot prompt twice. */
export function clearDeferredPrompt(): void {
  deferred = null;
}
