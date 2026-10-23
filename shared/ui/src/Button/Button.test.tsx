import { fireEvent, render, screen } from '@testing-library/react';
import { Plus, Trash2 } from 'lucide-react';
import { createRef } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { Button, IconButton } from './Button';

describe('Button', () => {
  it('maps variant, size and block to tz- classes and defaults to type=button', () => {
    const { rerender } = render(<Button>Save</Button>);
    const b = screen.getByRole('button', { name: 'Save' });
    expect(b).toHaveAttribute('type', 'button');
    expect(b.className).toBe('tz-btn');
    rerender(
      <Button variant="brand" size="lg" block icon={Plus} type="submit" className="x">
        Place order
      </Button>,
    );
    expect(b).toHaveClass('tz-btn', 'tz-btn-brand', 'tz-btn-lg', 'tz-btn-block', 'x');
    expect(b).toHaveAttribute('type', 'submit');
    expect(b.querySelector('svg.tz-ico')).not.toBeNull();
  });

  it('loading keeps the label and focus, shows a spinner, and blocks clicks without disabling', () => {
    const onClick = vi.fn();
    render(
      <Button loading icon={Plus} onClick={onClick}>
        Charge
      </Button>,
    );
    const b = screen.getByRole('button', { name: 'Charge' });
    expect(b).toBeEnabled();
    expect(b).toHaveAttribute('aria-disabled', 'true');
    expect(b).toHaveAttribute('aria-busy', 'true');
    expect(b.querySelector('.tz-ui-spin')).not.toBeNull();
    b.focus();
    fireEvent.click(b);
    expect(onClick).not.toHaveBeenCalled();
    expect(b).toHaveFocus();
  });

  it('keeps focus when loading starts and announces the loading label', () => {
    const { rerender } = render(
      <Button loadingLabel="Charging" onClick={() => undefined}>
        Charge
      </Button>,
    );
    const b = screen.getByRole('button', { name: 'Charge' });
    const live = b.querySelector('[aria-live="polite"]');
    expect(live).toBeEmptyDOMElement();
    b.focus();
    rerender(
      <Button loading loadingLabel="Charging" onClick={() => undefined}>
        Charge
      </Button>,
    );
    expect(b).toHaveFocus();
    expect(live).toHaveTextContent('Charging');
    expect(b).toHaveAccessibleName('Charge Charging');
  });

  it('a loading submit button does not submit its form', () => {
    const onSubmit = vi.fn((e: { preventDefault: () => void }) => e.preventDefault());
    render(
      <form onSubmit={onSubmit}>
        <Button type="submit" loading>
          Save
        </Button>
      </form>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('disabled stays a real disabled button, even while loading', () => {
    const onClick = vi.fn();
    render(
      <Button disabled loading onClick={onClick}>
        Charge
      </Button>,
    );
    const b = screen.getByRole('button', { name: 'Charge' });
    expect(b).toBeDisabled();
    expect(b).not.toHaveAttribute('aria-disabled');
  });

  it('calls onClick when not loading', () => {
    const onClick = vi.fn();
    render(<Button onClick={onClick}>Go</Button>);
    fireEvent.click(screen.getByRole('button', { name: 'Go' }));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('forwards the ref', () => {
    const ref = createRef<HTMLButtonElement>();
    render(<Button ref={ref}>Go</Button>);
    expect(ref.current).toBeInstanceOf(HTMLButtonElement);
  });
});

describe('IconButton', () => {
  it('is named by its label and takes small and variant classes', () => {
    const ref = createRef<HTMLButtonElement>();
    const { rerender } = render(<IconButton ref={ref} icon={Trash2} label="Delete item" />);
    const b = screen.getByRole('button', { name: 'Delete item' });
    expect(b.className).toBe('tz-btn tz-btn-icon');
    expect(ref.current).toBe(b);
    rerender(<IconButton icon={Trash2} label="Delete item" size="sm" variant="danger" />);
    expect(b).toHaveClass('tz-btn-sm', 'tz-btn-danger');
  });
});
