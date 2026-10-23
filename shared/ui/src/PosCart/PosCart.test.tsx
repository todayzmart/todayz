import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { stepperLabels } from '../test/fixtures';
import { PosCart, type PosCartProps } from './PosCart';

function props(over: Partial<PosCartProps> = {}): PosCartProps {
  return {
    label: 'Current bill',
    customer: { label: 'Customer phone', phone: '98450', onPhoneChange: vi.fn(), summary: 'Ananya R. · 6 visits' },
    lines: [
      { id: 'rice', name: 'Sona Masoori Rice', detail: '5 kg · ₹289', quantity: 2, total: '₹578', max: 2 },
      { id: 'oil', name: 'Sunflower Oil', quantity: 1, total: '₹165' },
    ],
    onQuantityChange: vi.fn(),
    stepperLabels,
    emptyLabel: 'Scan or search to add items',
    payment: {
      label: 'Payment method',
      options: [
        { value: 'upi', label: 'UPI' },
        { value: 'cash', label: 'Cash' },
      ],
      value: 'upi',
      onChange: vi.fn(),
    },
    summary: '3 items',
    amount: '₹743',
    chargeLabel: 'Charge ₹743',
    onCharge: vi.fn(),
    ...over,
  };
}

describe('PosCart', () => {
  it('edits the phone, steps lines, picks payment and charges', () => {
    const p = props();
    render(<PosCart {...p} />);
    expect(screen.getByRole('region', { name: 'Current bill' })).toBeInTheDocument();
    fireEvent.change(screen.getByRole('textbox', { name: 'Customer phone' }), { target: { value: '984501' } });
    expect(p.customer.onPhoneChange).toHaveBeenCalledWith('984501');
    expect(screen.getByText('Ananya R. · 6 visits')).toBeInTheDocument();
    const rice = screen.getAllByRole('listitem')[0] as HTMLElement;
    expect(within(rice).getByText('5 kg · ₹289')).toBeInTheDocument();
    expect(within(rice).getByRole('button', { name: 'Add one, Sona Masoori Rice' })).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(within(rice).getByRole('button', { name: 'Remove one, Sona Masoori Rice' }));
    expect(p.onQuantityChange).toHaveBeenCalledWith('rice', 1);
    fireEvent.click(screen.getByRole('button', { name: 'Cash' }));
    expect(p.payment.onChange).toHaveBeenCalledWith('cash');
    fireEvent.click(screen.getByRole('button', { name: 'Charge ₹743' }));
    expect(p.onCharge).toHaveBeenCalled();
  });

  it('shows the empty label and disables Charge on an empty bill', () => {
    const p = props({ lines: [], summary: undefined, customer: { ...props().customer, summary: undefined } });
    render(<PosCart {...p} />);
    expect(screen.getByText('Scan or search to add items')).toBeInTheDocument();
    expect(screen.queryByRole('list')).toBeNull();
    expect(screen.getByRole('button', { name: 'Charge ₹743' })).toBeDisabled();
  });

  it('blocks a second charge while charging', () => {
    const p = props({ charging: true, chargingLabel: 'Charging' });
    render(<PosCart {...p} />);
    const charge = screen.getByRole('button', { name: /Charge ₹743/ });
    charge.focus();
    fireEvent.click(charge);
    expect(p.onCharge).not.toHaveBeenCalled();
    expect(charge).toHaveAttribute('aria-busy', 'true');
    expect(charge).toHaveAttribute('aria-disabled', 'true');
    expect(charge).toHaveFocus();
    expect(charge).toHaveTextContent('Charging');
  });
});
