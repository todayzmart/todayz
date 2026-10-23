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

  it('loading keeps the label, shows a spinner, sets aria-busy and blocks clicks', () => {
    const onClick = vi.fn();
    render(
      <Button loading icon={Plus} onClick={onClick}>
        Charge
      </Button>,
    );
    const b = screen.getByRole('button', { name: 'Charge' });
    expect(b).toBeDisabled();
    expect(b).toHaveAttribute('aria-busy', 'true');
    expect(b.querySelector('.tz-ui-spin')).not.toBeNull();
    fireEvent.click(b);
    expect(onClick).not.toHaveBeenCalled();
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
