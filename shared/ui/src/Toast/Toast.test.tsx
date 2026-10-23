import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useToast } from './context';
import { Toast, ToastProvider } from './Toast';
import type { ShowToast } from './context';

describe('Toast', () => {
  it('renders a link action or a button action', () => {
    const onClick = vi.fn();
    const { rerender } = render(<Toast message="Sale recorded" action={{ label: 'View', href: '/sales/1' }} />);
    expect(screen.getByRole('link', { name: 'View' })).toHaveAttribute('href', '/sales/1');
    rerender(<Toast message="Could not sync" action={{ label: 'Retry', onClick }} />);
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(onClick).toHaveBeenCalled();
    rerender(<Toast message="Stock updated" />);
    expect(screen.queryByRole('button')).toBeNull();
  });
});

function Trigger({ toast }: { toast: ShowToast }) {
  const api = useToast();
  return (
    <>
      <button type="button" onClick={() => api.show(toast)}>
        show
      </button>
      <button type="button" onClick={() => api.dismiss(1)}>
        dismiss first
      </button>
    </>
  );
}

describe('ToastProvider', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const show = () => fireEvent.click(screen.getByRole('button', { name: 'show' }));
  const region = () => screen.getByRole('status');

  it('shows toasts in a polite region and hides them after the duration', () => {
    render(
      <ToastProvider duration={1000}>
        <Trigger toast={{ message: 'Sale recorded' }} />
      </ToastProvider>,
    );
    expect(region()).toHaveAttribute('aria-live', 'polite');
    show();
    expect(region()).toHaveTextContent('Sale recorded');
    act(() => vi.advanceTimersByTime(999));
    expect(region()).toHaveTextContent('Sale recorded');
    act(() => vi.advanceTimersByTime(1));
    expect(region()).toBeEmptyDOMElement();
  });

  it('pauses while hovered or focused', () => {
    render(
      <ToastProvider duration={1000}>
        <Trigger toast={{ message: 'Paused toast' }} />
      </ToastProvider>,
    );
    show();
    const item = region().firstElementChild as HTMLElement;
    fireEvent.mouseEnter(item);
    act(() => vi.advanceTimersByTime(5000));
    expect(region()).toHaveTextContent('Paused toast');
    fireEvent.mouseLeave(item);
    fireEvent.focus(item);
    act(() => vi.advanceTimersByTime(5000));
    expect(region()).toHaveTextContent('Paused toast');
    fireEvent.blur(item);
    act(() => vi.advanceTimersByTime(1000));
    expect(region()).toBeEmptyDOMElement();
  });

  it('keeps at most max toasts, oldest out first, and dismisses by id', () => {
    let n = 0;
    function Many() {
      const api = useToast();
      return (
        <button type="button" onClick={() => api.show({ message: `Toast ${++n}`, duration: null })}>
          show
        </button>
      );
    }
    render(
      <ToastProvider max={2}>
        <Many />
      </ToastProvider>,
    );
    show();
    show();
    show();
    expect(region()).not.toHaveTextContent('Toast 1');
    expect(region()).toHaveTextContent('Toast 2');
    expect(region()).toHaveTextContent('Toast 3');
    act(() => vi.advanceTimersByTime(60_000));
    expect(region().children).toHaveLength(2);
  });

  it('runs the action, then dismisses the toast', () => {
    const onClick = vi.fn();
    render(
      <ToastProvider>
        <Trigger toast={{ message: 'A new version is ready', duration: null, action: { label: 'Reload', onClick } }} />
      </ToastProvider>,
    );
    show();
    fireEvent.click(screen.getByRole('button', { name: 'Reload' }));
    expect(onClick).toHaveBeenCalled();
    expect(region()).toBeEmptyDOMElement();
    show();
    fireEvent.click(screen.getByRole('button', { name: 'dismiss first' }));
    expect(region()).toHaveTextContent('A new version is ready');
  });

  it('dismisses through the api and with an action that has no handler', () => {
    render(
      <ToastProvider>
        <Trigger toast={{ message: 'Saved', action: { label: 'Open', href: '#saved' } }} />
      </ToastProvider>,
    );
    show();
    fireEvent.click(screen.getByRole('button', { name: 'dismiss first' }));
    expect(region()).toBeEmptyDOMElement();
    show();
    fireEvent.click(screen.getByRole('link', { name: 'Open' }));
    expect(region()).toBeEmptyDOMElement();
  });

  it('useToast outside the provider throws', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    expect(() => render(<Trigger toast={{ message: 'x' }} />)).toThrow(/ToastProvider/);
  });
});
