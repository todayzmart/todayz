import { ToastProvider } from '@todayz/ui';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { ErrorBoundary } from './error-boundary';
import { OfflineBanner } from './online';
import { authDefines } from '../build.mjs';
import { TURNSTILE_DUMMY_TOKEN, getTurnstileToken } from './turnstile';
import { UpdateToastHost, createUpdateController, useHoldUpdates } from './update';

describe('update toast', () => {
  const labels = { message: 'A new version is ready', action: 'Update' };

  function Holder({ controller, active }: { controller: ReturnType<typeof createUpdateController>; active: boolean }) {
    useHoldUpdates(controller, active);
    return null;
  }

  it('shows when ready and reloads only on the tap', async () => {
    const controller = createUpdateController();
    const updater = vi.fn(async () => undefined);
    controller.setUpdater(updater);
    render(
      <ToastProvider>
        <UpdateToastHost controller={controller} labels={labels} />
      </ToastProvider>,
    );
    expect(screen.queryByText(labels.message)).toBeNull();
    act(() => controller.notifyReady());
    expect(await screen.findByText(labels.message)).toBeInTheDocument();
    expect(updater).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: labels.action }));
    expect(updater).toHaveBeenCalledWith(true);
  });

  it('holds back while a sale or checkout is open, then appears', async () => {
    const controller = createUpdateController();
    const { rerender } = render(
      <ToastProvider>
        <UpdateToastHost controller={controller} labels={labels} />
        <Holder controller={controller} active />
      </ToastProvider>,
    );
    act(() => controller.notifyReady());
    expect(screen.queryByText(labels.message)).toBeNull();
    rerender(
      <ToastProvider>
        <UpdateToastHost controller={controller} labels={labels} />
        <Holder controller={controller} active={false} />
      </ToastProvider>,
    );
    expect(await screen.findByText(labels.message)).toBeInTheDocument();
  });

  it('never calls the updater by itself', () => {
    const controller = createUpdateController();
    const updater = vi.fn(async () => undefined);
    controller.setUpdater(updater);
    controller.notifyReady();
    expect(updater).not.toHaveBeenCalled();
  });
});

describe('offline banner', () => {
  it('shows only while offline', () => {
    const online = vi.spyOn(navigator, 'onLine', 'get');
    online.mockReturnValue(true);
    const { container } = render(<OfflineBanner title="You're offline." message="Showing saved data." />);
    expect(container).toBeEmptyDOMElement();
    online.mockReturnValue(false);
    act(() => {
      window.dispatchEvent(new Event('offline'));
    });
    expect(screen.getByRole('status')).toHaveTextContent("You're offline.");
    online.mockRestore();
  });
});

describe('error boundary', () => {
  it('shows the fallback, reports the error and recovers on reset', async () => {
    const onError = vi.fn();
    let broken = true;
    function Bomb() {
      if (broken) throw new Error('boom');
      return <p>fine</p>;
    }
    const spy = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    render(
      <ErrorBoundary
        onError={onError}
        fallback={(reset) => (
          <button type="button" onClick={reset}>
            Try again
          </button>
        )}
      >
        <Bomb />
      </ErrorBoundary>,
    );
    expect(onError).toHaveBeenCalledTimes(1);
    broken = false;
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(screen.getByText('fine')).toBeInTheDocument();
    spy.mockRestore();
  });
});

describe('turnstile', () => {
  it('uses the dummy token for the always-pass test key and loads no script', async () => {
    expect(await getTurnstileToken('1x00000000000000000000AA')).toBe(TURNSTILE_DUMMY_TOKEN);
    expect(document.querySelector('script[src*="challenges.cloudflare.com"]')).toBeNull();
  });

  it('fails a production build without a real site key or an https API URL', () => {
    const api = 'https://api.todayz.in';
    expect(() => authDefines('production', { VITE_API_URL: api })).toThrow(/VITE_TURNSTILE_SITE_KEY/);
    expect(() => authDefines('production', { VITE_API_URL: api, VITE_TURNSTILE_SITE_KEY: '1x00000000000000000000AA' })).toThrow(
      /VITE_TURNSTILE_SITE_KEY/,
    );
    expect(() => authDefines('production', { VITE_TURNSTILE_SITE_KEY: '0x4AAAAAAAreal' })).toThrow(/VITE_API_URL/);
    expect(() => authDefines('production', { VITE_TURNSTILE_SITE_KEY: '0x4AAAAAAAreal', VITE_API_URL: 'http://api.x' })).toThrow(
      /VITE_API_URL/,
    );
    const ok = authDefines('production', { VITE_TURNSTILE_SITE_KEY: '0x4AAAAAAAreal', VITE_API_URL: `${api}/` });
    expect(ok['import.meta.env.VITE_TURNSTILE_SITE_KEY']).toBe('"0x4AAAAAAAreal"');
    expect(ok['import.meta.env.VITE_API_URL']).toBe(`"${api}"`);
  });

  it('falls back to the local API and the test key outside production', () => {
    for (const mode of ['development', 'test', 'e2e']) {
      const d = authDefines(mode, {});
      expect(d['import.meta.env.VITE_TURNSTILE_SITE_KEY']).toBe('"1x00000000000000000000AA"');
      expect(d['import.meta.env.VITE_API_URL']).toBe('"https://api.todayz.localhost"');
    }
  });
});
