import { afterEach, describe, expect, it } from 'vitest';
import { captureInstallPrompt, clearDeferredPrompt, getDeferredPrompt } from './install';

function installPromptEvent() {
  return new Event('beforeinstallprompt', { cancelable: true });
}

describe('install prompt', () => {
  let stop: (() => void) | undefined;
  afterEach(() => {
    stop?.();
    clearDeferredPrompt();
  });

  it('is empty until the browser offers one', () => {
    stop = captureInstallPrompt();
    expect(getDeferredPrompt()).toBeNull();
  });

  it('suppresses the browser prompt and keeps the event for later', () => {
    stop = captureInstallPrompt();
    const event = installPromptEvent();
    window.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(true);
    expect(getDeferredPrompt()).toBe(event);
  });

  it('forgets the event once the app is installed or the prompt was used', () => {
    stop = captureInstallPrompt();
    window.dispatchEvent(installPromptEvent());
    window.dispatchEvent(new Event('appinstalled'));
    expect(getDeferredPrompt()).toBeNull();
    window.dispatchEvent(installPromptEvent());
    clearDeferredPrompt();
    expect(getDeferredPrompt()).toBeNull();
  });

  it('stops listening when asked', () => {
    captureInstallPrompt()();
    const event = installPromptEvent();
    window.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
    expect(getDeferredPrompt()).toBeNull();
  });
});
