import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { OtpInput, type OtpInputProps } from './OtpInput';

type Partial = Omit<OtpInputProps, 'value' | 'onChange' | 'label' | 'digitLabel'> & { start?: string };

function Code({ start = '', ...rest }: Partial) {
  const [v, setV] = useState(start);
  return (
    <>
      <OtpInput label="Enter customer's code" digitLabel={(i) => `Digit ${i}`} value={v} onChange={setV} {...rest} />
      <output data-testid="v">{v}</output>
    </>
  );
}

const boxes = () => screen.getAllByRole('textbox') as HTMLInputElement[];
const value = () => screen.getByTestId('v').textContent;

describe('OtpInput', () => {
  it('names each box and advances as digits are typed, then completes once', async () => {
    const onComplete = vi.fn();
    render(<Code onComplete={onComplete} help="Ask the customer for the 4-digit code" />);
    expect(screen.getByRole('textbox', { name: "Enter customer's code" })).toBe(boxes()[0]);
    expect(screen.getByRole('textbox', { name: 'Digit 4' })).toBeInTheDocument();
    expect(boxes()[0]).toHaveAccessibleDescription('Ask the customer for the 4-digit code');
    await userEvent.click(boxes()[0] as HTMLInputElement);
    await userEvent.keyboard('4a82');
    expect(value()).toBe('482');
    expect(boxes()[3]).toHaveFocus();
    await userEvent.keyboard('7');
    expect(value()).toBe('4827');
    expect(onComplete).toHaveBeenCalledTimes(1);
    expect(onComplete).toHaveBeenCalledWith('4827');
  });

  it('fills every box from a paste, ignoring non-digits', () => {
    const onComplete = vi.fn();
    render(<Code onComplete={onComplete} />);
    fireEvent.paste(boxes()[1] as HTMLInputElement, { clipboardData: { getData: () => 'code: 9-1-3-5-6' } });
    expect(value()).toBe('9135');
    expect(onComplete).toHaveBeenCalledWith('9135');
    fireEvent.paste(boxes()[0] as HTMLInputElement, { clipboardData: { getData: () => 'none' } });
    expect(value()).toBe('9135');
  });

  it('treats several digits typed at once (autofill) like a paste from that box', () => {
    render(<Code start="1" />);
    fireEvent.change(boxes()[1] as HTMLInputElement, { target: { value: '234' } });
    expect(value()).toBe('1234');
  });

  it('replaces a filled box when typing over it without selecting', () => {
    render(<Code start="12" />);
    fireEvent.change(boxes()[0] as HTMLInputElement, { target: { value: '15' } });
    expect(value()).toBe('52');
    fireEvent.change(boxes()[0] as HTMLInputElement, { target: { value: '75' } });
    expect(value()).toBe('72');
    fireEvent.change(boxes()[0] as HTMLInputElement, { target: { value: 'x' } });
    expect(value()).toBe('72');
  });

  it('Backspace clears the box or steps back, arrows move', async () => {
    render(<Code start="482" />);
    const [b0, b1, b2, b3] = boxes();
    await userEvent.click(b3 as HTMLInputElement);
    await userEvent.keyboard('{Backspace}');
    expect(value()).toBe('48');
    expect(b2).toHaveFocus();
    await userEvent.keyboard('{Backspace}');
    expect(value()).toBe('4');
    expect(b1).toHaveFocus();
    await userEvent.keyboard('{ArrowLeft}');
    expect(b0).toHaveFocus();
    await userEvent.keyboard('{ArrowLeft}');
    expect(b0).toHaveFocus();
    await userEvent.keyboard('{ArrowRight}');
    expect(b1).toHaveFocus();
    await userEvent.click(b0 as HTMLInputElement);
    await userEvent.keyboard('{Backspace}');
    expect(value()).toBe('');
    await userEvent.keyboard('{Backspace}');
    expect(value()).toBe('');
    expect(b0).toHaveFocus();
  });

  it('shows the error, marks boxes invalid and shakes until the animation ends', () => {
    const { container, rerender } = render(
      <OtpInput label="Code" digitLabel={(i) => `Digit ${i}`} value="1111" onChange={() => undefined} />,
    );
    const row = container.querySelector('.tz-otp') as HTMLElement;
    expect(row).not.toHaveClass('tz-ui-shake');
    rerender(
      <OtpInput
        label="Code"
        digitLabel={(i) => `Digit ${i}`}
        value="1111"
        onChange={() => undefined}
        error="Code does not match"
        errorKey={1}
      />,
    );
    expect(screen.getByRole('alert')).toHaveTextContent('Code does not match');
    for (const b of boxes()) expect(b).toHaveAttribute('aria-invalid', 'true');
    expect(row).toHaveClass('tz-ui-shake');
    fireEvent.animationEnd(row);
    expect(row).not.toHaveClass('tz-ui-shake');
    rerender(
      <OtpInput
        label="Code"
        digitLabel={(i) => `Digit ${i}`}
        value="1111"
        onChange={() => undefined}
        error="Code does not match"
        errorKey={2}
      />,
    );
    expect(row).toHaveClass('tz-ui-shake');
  });

  it('supports six-digit codes, autofocus and disabled', () => {
    // The component's own autoFocus prop (the counter's code screen) is what is under test.
    // eslint-disable-next-line jsx-a11y/no-autofocus
    render(<Code length={6} autoFocus />);
    expect(boxes()).toHaveLength(6);
    expect(boxes()[0]).toHaveFocus();
    expect(boxes()[0]).toHaveAttribute('autocomplete', 'one-time-code');
  });

  it('disables every box', () => {
    render(<Code disabled />);
    for (const b of boxes()) expect(b).toBeDisabled();
  });
});
