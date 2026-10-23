import { fireEvent, render, screen } from '@testing-library/react';
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
    fireEvent.click(screen.getByText('Body'));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(sheet);
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole('button', { name: 'Close' })).toBeNull();
  });

  it('a non-dismissible alertdialog ignores Escape and the backdrop', () => {
    const onClose = vi.fn();
    render(<Dialog open role="alertdialog" dismissible={false} onClose={onClose} title="Reject order?" />);
    const dialog = screen.getByRole('alertdialog', { name: 'Reject order?' });
    expect(cancel(dialog).defaultPrevented).toBe(true);
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
});
