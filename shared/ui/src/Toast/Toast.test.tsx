import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { forwardRef } from 'react';
import { Dialog } from '../Dialog/Dialog';
import type { LinkProps } from '../internal/link';
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

  it('styles a link action like a button action and routes it through LinkComponent', () => {
    const RouterLink = forwardRef<HTMLAnchorElement, LinkProps>(function RouterLink({ children, ...props }, ref) {
      return (
        <a ref={ref} data-router="" {...props}>
          {children}
        </a>
      );
    });
    render(<Toast message="Sale recorded" action={{ label: 'View', href: '/sales/1' }} LinkComponent={RouterLink} />);
    const link = screen.getByRole('link', { name: 'View' });
    expect(link).toHaveClass('tz-ui-toast-action');
    expect(link).toHaveAttribute('data-router');
  });

  it('forwards ref and rest props', () => {
    const ref = { current: null as HTMLSpanElement | null };
    render(<Toast ref={ref} message="Saved" data-testid="t" />);
    expect(ref.current).toBe(screen.getByTestId('t'));
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

  it('gives a toast with an action the longer default', () => {
    render(
      <ToastProvider duration={1000} actionDuration={8000}>
        <Trigger toast={{ message: 'Could not sync', action: { label: 'Retry' } }} />
      </ToastProvider>,
    );
    show();
    act(() => vi.advanceTimersByTime(7999));
    expect(region()).toHaveTextContent('Could not sync');
    act(() => vi.advanceTimersByTime(1));
    expect(region()).toBeEmptyDOMElement();
  });

  it('pauses every toast while focus is anywhere in the region, and resumes when focus leaves', () => {
    render(
      <ToastProvider duration={1000}>
        <Trigger toast={{ message: 'Undo?', action: { label: 'Undo' }, duration: 1000 }} />
      </ToastProvider>,
    );
    show();
    show();
    const [first] = screen.getAllByRole('button', { name: 'Undo' });
    act(() => (first as HTMLElement).focus());
    act(() => vi.advanceTimersByTime(5000));
    expect(screen.getAllByRole('button', { name: 'Undo' })).toHaveLength(2);
    // Focus moving between toasts keeps the pause.
    const second = screen.getAllByRole('button', { name: 'Undo' })[1] as HTMLElement;
    act(() => second.focus());
    act(() => vi.advanceTimersByTime(5000));
    expect(screen.getAllByRole('button', { name: 'Undo' })).toHaveLength(2);
    act(() => screen.getByRole('button', { name: 'show' }).focus());
    act(() => vi.advanceTimersByTime(1000));
    expect(region()).toBeEmptyDOMElement();
  });

  it('renders inside the open Dialog, so it stays visible and usable, and moves back when it closes', () => {
    const onClick = vi.fn();
    function Screen({ open }: { open: boolean }) {
      return (
        <ToastProvider>
          <Dialog open={open} onClose={() => undefined} title="Edit stock">
            <Trigger toast={{ message: 'Saved', duration: null, action: { label: 'Undo', onClick } }} />
          </Dialog>
        </ToastProvider>
      );
    }
    const { rerender } = render(<Screen open />);
    const dialog = document.querySelector('dialog') as HTMLDialogElement;
    expect(dialog.contains(region())).toBe(true);
    show();
    fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
    expect(onClick).toHaveBeenCalled();
    rerender(<Screen open={false} />);
    expect(dialog.contains(region())).toBe(false);
  });

  it('an emptied region does not keep the next toast paused', () => {
    render(
      <ToastProvider duration={1000}>
        <Trigger toast={{ message: 'Saved', action: { label: 'Undo' }, duration: 1000 }} />
      </ToastProvider>,
    );
    show();
    const undo = screen.getByRole('button', { name: 'Undo' });
    fireEvent.mouseEnter(undo);
    act(() => undo.focus());
    // The action removes the toast together with the focused element: no blur, no mouseleave.
    fireEvent.click(undo);
    expect(region()).toBeEmptyDOMElement();
    show();
    act(() => vi.advanceTimersByTime(1000));
    expect(region()).toBeEmptyDOMElement();
  });
});
