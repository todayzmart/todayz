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

  it('dims + at the stock limit without dropping focus, says so, and disables Add when out of stock', async () => {
    render(<Stateful start={2} max={3} />);
    const inc = screen.getByRole('button', { name: `${stepperLabels.increment}, Amul Taaza Milk` });
    await userEvent.click(inc);
    expect(inc).toHaveFocus();
    expect(inc).toBeEnabled();
    expect(inc).toHaveAttribute('aria-disabled', 'true');
    const count = screen.getByText('3').closest('[aria-live]') as HTMLElement;
    expect(count).toHaveTextContent('3Only 3 in stock');
    await userEvent.click(inc);
    await userEvent.keyboard('{Enter}');
    expect(count).toHaveTextContent(/^3/);
    expect(inc).toHaveFocus();
    expect(screen.getByRole('button', { name: `${stepperLabels.decrement}, Amul Taaza Milk` })).not.toHaveAttribute(
      'aria-disabled',
    );
  });

  it('Add is disabled when out of stock', () => {
    render(<QuantityStepper quantity={0} max={0} onChange={() => undefined} labels={stepperLabels} />);
    expect(screen.getByRole('button', { name: stepperLabels.add })).toBeDisabled();
  });

  it('without a max label only the count is read at the limit', () => {
    const { add, decrement, increment } = stepperLabels;
    render(<QuantityStepper quantity={2} max={2} onChange={() => undefined} labels={{ add, decrement, increment }} />);
    expect(screen.getByText('2')).toHaveTextContent(/^2$/);
  });

  it('a change the parent refuses does not move focus on a later outside change', async () => {
    const onChange = vi.fn();
    const { rerender } = render(<QuantityStepper quantity={1} onChange={onChange} labels={stepperLabels} />);
    // The parent ignores the request (e.g. the line is locked), so nothing re-renders.
    await userEvent.click(screen.getByRole('button', { name: stepperLabels.increment }));
    expect(onChange).toHaveBeenCalledWith(2);
    // Later the cart syncs and the line drops to 0: that is not this control's change.
    rerender(<QuantityStepper quantity={0} onChange={onChange} labels={stepperLabels} />);
    expect(screen.getByRole('button', { name: stepperLabels.add })).not.toHaveFocus();
  });

  it('does not pull focus back when the user moved on before the parent applied the change', async () => {
    const ui = (q: number) => (
      <>
        <QuantityStepper quantity={q} onChange={() => undefined} labels={stepperLabels} />
        <button type="button">Elsewhere</button>
      </>
    );
    const { rerender } = render(ui(0));
    await userEvent.click(screen.getByRole('button', { name: stepperLabels.add }));
    screen.getByRole('button', { name: 'Elsewhere' }).focus();
    // The server answers later with the requested quantity.
    rerender(ui(1));
    expect(screen.getByRole('button', { name: 'Elsewhere' })).toHaveFocus();
  });

  it('does not move focus when the quantity changes from outside', () => {
    const { rerender } = render(<QuantityStepper quantity={0} onChange={() => undefined} labels={stepperLabels} />);
    rerender(<QuantityStepper quantity={2} onChange={() => undefined} labels={stepperLabels} />);
    expect(screen.getByRole('button', { name: stepperLabels.increment })).not.toHaveFocus();
  });

  it('always shows the small stepper on POS lines, with − dimmed at zero', async () => {
    const onChange = vi.fn();
    const { container } = render(
      <QuantityStepper quantity={0} alwaysStepper size="sm" onChange={onChange} labels={stepperLabels} />,
    );
    expect(container.firstChild).toHaveClass('tz-stepper', 'tz-ui-stepper-sm');
    const dec = screen.getByRole('button', { name: stepperLabels.decrement });
    expect(dec).toHaveAttribute('aria-disabled', 'true');
    await userEvent.click(dec);
    expect(onChange).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: stepperLabels.increment }));
    expect(onChange).toHaveBeenCalledWith(1);
  });

  it('disables everything when disabled', () => {
    render(<QuantityStepper quantity={2} disabled onChange={() => undefined} labels={stepperLabels} />);
    for (const b of screen.getAllByRole('button')) expect(b).toBeDisabled();
  });
});
