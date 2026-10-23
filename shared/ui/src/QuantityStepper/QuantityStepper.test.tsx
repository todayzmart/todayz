import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { stepperLabels } from '../test/fixtures';
import { QuantityStepper } from './QuantityStepper';

function Stateful({ start = 0, max }: { start?: number; max?: number }) {
  const [q, setQ] = useState(start);
  return <QuantityStepper quantity={q} onChange={setQ} max={max} labels={stepperLabels} itemName="Amul Taaza Milk" />;
}

describe('QuantityStepper', () => {
  it('turns the Add pill into a stepper and moves focus with it', async () => {
    render(<Stateful />);
    await userEvent.click(screen.getByRole('button', { name: `${stepperLabels.add}, Amul Taaza Milk` }));
    const inc = screen.getByRole('button', { name: `${stepperLabels.increment}, Amul Taaza Milk` });
    expect(inc).toHaveFocus();
    expect(screen.getByText('1')).toHaveAttribute('aria-live', 'polite');
    await userEvent.click(inc);
    expect(screen.getByText('2')).toBeInTheDocument();
    const dec = screen.getByRole('button', { name: `${stepperLabels.decrement}, Amul Taaza Milk` });
    await userEvent.click(dec);
    await userEvent.click(dec);
    expect(screen.getByRole('button', { name: `${stepperLabels.add}, Amul Taaza Milk` })).toHaveFocus();
  });

  it('disables + at the stock limit and Add when out of stock', () => {
    const { rerender } = render(
      <QuantityStepper quantity={3} max={3} onChange={() => undefined} labels={stepperLabels} />,
    );
    expect(screen.getByRole('button', { name: stepperLabels.increment })).toBeDisabled();
    expect(screen.getByRole('button', { name: stepperLabels.decrement })).toBeEnabled();
    rerender(<QuantityStepper quantity={0} max={0} onChange={() => undefined} labels={stepperLabels} />);
    expect(screen.getByRole('button', { name: stepperLabels.add })).toBeDisabled();
  });

  it('does not move focus when the quantity changes from outside', () => {
    const { rerender } = render(<QuantityStepper quantity={0} onChange={() => undefined} labels={stepperLabels} />);
    rerender(<QuantityStepper quantity={2} onChange={() => undefined} labels={stepperLabels} />);
    expect(screen.getByRole('button', { name: stepperLabels.increment })).not.toHaveFocus();
  });

  it('always shows the small stepper on POS lines, with − disabled at zero', async () => {
    const onChange = vi.fn();
    const { container } = render(
      <QuantityStepper quantity={0} alwaysStepper size="sm" onChange={onChange} labels={stepperLabels} />,
    );
    expect(container.firstChild).toHaveClass('tz-stepper', 'tz-ui-stepper-sm');
    expect(screen.getByRole('button', { name: stepperLabels.decrement })).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: stepperLabels.increment }));
    expect(onChange).toHaveBeenCalledWith(1);
  });

  it('disables everything when disabled', () => {
    render(<QuantityStepper quantity={2} disabled onChange={() => undefined} labels={stepperLabels} />);
    for (const b of screen.getAllByRole('button')) expect(b).toBeDisabled();
  });
});
