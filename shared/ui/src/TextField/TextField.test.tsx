import { render, screen } from '@testing-library/react';
import { createRef } from 'react';
import { describe, expect, it } from 'vitest';
import { TextField } from './TextField';

describe('TextField', () => {
  it('labels the input and describes it with the help text', () => {
    render(<TextField label="Shop name" help="As it appears on your board" />);
    const input = screen.getByRole('textbox', { name: 'Shop name' });
    expect(input).toHaveAccessibleDescription('As it appears on your board');
    expect(input).not.toHaveAttribute('aria-invalid');
  });

  it('an error replaces the help, marks the input invalid and is announced with it', () => {
    render(<TextField label="PIN code" id="pin" help="6 digits" error="Enter a 6-digit PIN code" />);
    const input = screen.getByRole('textbox', { name: 'PIN code' });
    expect(input).toHaveAttribute('id', 'pin');
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(input).toHaveClass('tz-input-invalid');
    expect(input).toHaveAccessibleDescription('Enter a 6-digit PIN code');
    expect(screen.queryByText('6 digits')).toBeNull();
  });

  it('renders a prefix (phone) that turns invalid with the input, and forwards the ref', () => {
    const ref = createRef<HTMLInputElement>();
    const { container, rerender } = render(<TextField ref={ref} label="Phone" type="tel" prefix="+91" />);
    expect(screen.getByText('+91')).toBeInTheDocument();
    expect(ref.current).toBe(screen.getByRole('textbox', { name: 'Phone' }));
    expect(container.querySelector('.tz-ui-affix')).not.toHaveClass('tz-ui-affix-invalid');
    rerender(<TextField ref={ref} label="Phone" type="tel" prefix="+91" error="Check the number" />);
    expect(container.querySelector('.tz-ui-affix')).toHaveClass('tz-ui-affix-invalid');
  });

  it('treats false and null errors as no error', () => {
    render(<TextField label="Name" error={false} />);
    expect(screen.getByRole('textbox')).not.toHaveAttribute('aria-invalid');
    expect(screen.getByRole('textbox')).not.toHaveAttribute('aria-describedby');
  });
});
