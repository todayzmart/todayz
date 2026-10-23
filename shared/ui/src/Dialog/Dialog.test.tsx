import { act, fireEvent, render, screen } from '@testing-library/react';
import { createRef, useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { Sheet } from '../Sheet/Sheet';
import { Dialog } from './Dialog';

const cancel = (el: HTMLElement) => {
  const e = new Event('cancel', { cancelable: true });
  el.dispatchEvent(e);
  return e;
};

describe('Dialog and Sheet (shared modal)', () => {
  it('opens as a modal with a name, description and footer, and locks page scroll', () => {
    const onClose = vi.fn();
    const { rerender } = render(
      <Dialog
        open
        onClose={onClose}
        title="Cancel this order?"
        description="The customer gets a refund"
        footer={<button type="button">Keep order</button>}
        closeLabel="Close"
      />,
    );
    const dialog = screen.getByRole('dialog', { name: 'Cancel this order?' });
    expect(dialog).toHaveAccessibleDescription('The customer gets a refund');
    expect(dialog).toHaveClass('tz-ui-dialog');
    expect((dialog as HTMLDialogElement).open).toBe(true);
    expect(document.documentElement).toHaveClass('tz-ui-scroll-lock');
    expect(screen.getByRole('button', { name: 'Keep order' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    rerender(<Dialog open={false} onClose={onClose} title="Cancel this order?" closeLabel="Close" />);
    expect(document.documentElement).not.toHaveClass('tz-ui-scroll-lock');
    expect((document.querySelector('dialog') as HTMLDialogElement).open).toBe(false);
  });

  it('closes on Escape (cancel) and on a backdrop click, but not on a content click', () => {
    const onClose = vi.fn();
    render(
      <Sheet open onClose={onClose} title="Filters">
        <p>Body</p>
      </Sheet>,
    );
    const sheet = document.querySelector('dialog') as HTMLDialogElement;
    expect(sheet).toHaveClass('tz-ui-sheet');
    expect(cancel(sheet).defaultPrevented).toBe(true);
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.pointerDown(screen.getByText('Body'));
    fireEvent.click(screen.getByText('Body'));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.pointerDown(sheet);
    fireEvent.click(sheet);
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole('button', { name: 'Close' })).toBeNull();
  });

  it('a non-dismissible alertdialog ignores Escape and the backdrop', () => {
    const onClose = vi.fn();
    render(<Dialog open role="alertdialog" dismissible={false} onClose={onClose} title="Reject order?" />);
    const dialog = screen.getByRole('alertdialog', { name: 'Reject order?' });
    expect(cancel(dialog).defaultPrevented).toBe(true);
    fireEvent.pointerDown(dialog);
    fireEvent.click(dialog);
    expect(onClose).not.toHaveBeenCalled();
    expect(dialog).not.toHaveAttribute('aria-describedby');
  });

  it('nested modals share one scroll lock', () => {
    const { rerender } = render(
      <>
        <Sheet open onClose={() => undefined} title="Outer" />
        <Dialog open onClose={() => undefined} title="Inner" />
      </>,
    );
    rerender(
      <>
        <Sheet open onClose={() => undefined} title="Outer" />
        <Dialog open={false} onClose={() => undefined} title="Inner" />
      </>,
    );
    expect(document.documentElement).toHaveClass('tz-ui-scroll-lock');
    rerender(
      <>
        <Sheet open={false} onClose={() => undefined} title="Outer" />
        <Dialog open={false} onClose={() => undefined} title="Inner" />
      </>,
    );
    expect(document.documentElement).not.toHaveClass('tz-ui-scroll-lock');
  });

  it('uses the latest onClose without re-binding', () => {
    const first = vi.fn();
    const second = vi.fn();
    const { rerender } = render(<Dialog open onClose={first} title="T" />);
    rerender(<Dialog open onClose={second} title="T" />);
    cancel(document.querySelector('dialog') as HTMLElement);
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalled();
  });

  it('a drag that starts in the content and ends on the backdrop does not close', () => {
    const onClose = vi.fn();
    render(
      <Sheet open onClose={onClose} title="Filters">
        <p>Body</p>
      </Sheet>,
    );
    const sheet = document.querySelector('dialog') as HTMLDialogElement;
    fireEvent.pointerDown(screen.getByText('Body'));
    fireEvent.click(sheet);
    expect(onClose).not.toHaveBeenCalled();
    // A click with no press first (a synthetic click) does not close either.
    fireEvent.click(sheet);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('syncs when the browser closes the dialog itself (form method=dialog)', async () => {
    function Host() {
      const [open, setOpen] = useState(true);
      return (
        <>
          <span data-testid="state">{open ? 'open' : 'closed'}</span>
          <Dialog open={open} onClose={() => setOpen(false)} title="Edit">
            <form method="dialog">
              <button type="submit">Done</button>
            </form>
          </Dialog>
        </>
      );
    }
    render(<Host />);
    const dialog = document.querySelector('dialog') as HTMLDialogElement;
    await act(async () => {
      dialog.close();
      await Promise.resolve();
    });
    expect(screen.getByTestId('state')).toHaveTextContent('closed');
    expect(dialog.open).toBe(false);
    expect(document.documentElement).not.toHaveClass('tz-ui-scroll-lock');
  });

  it('opens again when the parent keeps it open after a native close', async () => {
    const onClose = vi.fn();
    render(<Dialog open onClose={onClose} title="Stays" />);
    const dialog = document.querySelector('dialog') as HTMLDialogElement;
    await act(async () => {
      dialog.close();
      await Promise.resolve();
    });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(dialog.open).toBe(true);
    expect(screen.getByRole('heading', { name: 'Stays' })).toBeInTheDocument();
  });

  it('a forced Escape close (non-cancelable cancel) asks once, and a non-dismissible modal reopens', async () => {
    const onClose = vi.fn();
    const { rerender } = render(<Dialog open onClose={onClose} title="Esc" />);
    const dialog = document.querySelector('dialog') as HTMLDialogElement;
    await act(async () => {
      dialog.dispatchEvent(new Event('cancel', { cancelable: false }));
      dialog.close();
      await Promise.resolve();
    });
    // Asked to close in cancel; the close event does not ask again.
    expect(onClose).toHaveBeenCalledTimes(1);
    rerender(<Dialog open dismissible={false} onClose={onClose} title="Esc" />);
    await act(async () => {
      dialog.dispatchEvent(new Event('cancel', { cancelable: false }));
      dialog.close();
      await Promise.resolve();
    });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(dialog.open).toBe(true);
  });

  it('renders no content until the first open, and keeps it until the dialog has closed', async () => {
    const { rerender } = render(
      <Dialog open={false} onClose={() => undefined} title="Lazy">
        <p>Heavy body</p>
      </Dialog>,
    );
    expect(screen.queryByText('Heavy body')).toBeNull();
    expect(document.querySelector('dialog')).not.toHaveAttribute('aria-labelledby');
    rerender(
      <Dialog open onClose={() => undefined} title="Lazy">
        <p>Heavy body</p>
      </Dialog>,
    );
    expect(screen.getByText('Heavy body')).toBeInTheDocument();
    await act(async () => {
      rerender(
        <Dialog open={false} onClose={() => undefined} title="Lazy">
          <p>Heavy body</p>
        </Dialog>,
      );
      await Promise.resolve();
    });
    expect(screen.queryByText('Heavy body')).toBeNull();
  });

  it('focuses initialFocusRef, else the first control, else the dialog', () => {
    const safe = createRef<HTMLButtonElement>();
    const { unmount } = render(
      <Dialog
        open
        onClose={() => undefined}
        title="Delete?"
        initialFocusRef={safe}
        footer={
          <>
            <button type="button">Delete</button>
            <button type="button" ref={safe}>
              Keep
            </button>
          </>
        }
      />,
    );
    expect(document.activeElement).toBe(safe.current);
    unmount();
    render(<Sheet open onClose={() => undefined} title="Info" />);
    const sheet = document.querySelector('dialog') as HTMLDialogElement;
    expect(sheet.contains(document.activeElement)).toBe(true);
  });
});
