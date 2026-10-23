import { render, screen } from '@testing-library/react';
import { useState } from 'react';
import userEvent from '@testing-library/user-event';
import { CalendarDays } from 'lucide-react';
import { describe, expect, it, vi } from 'vitest';
import { Segmented } from './Segmented';

describe('Segmented', () => {
  it('presses one option and reports only real changes', async () => {
    const onChange = vi.fn();
    render(
      <Segmented
        label="Period"
        options={[
          { value: 'today', label: 'Today', icon: CalendarDays },
          { value: 'week', label: '7 days' },
        ]}
        value="today"
        onChange={onChange}
      />,
    );
    expect(screen.getByRole('radiogroup', { name: 'Period' })).toBeInTheDocument();
    const today = screen.getByRole('radio', { name: 'Today' });
    expect(today).toBeChecked();
    expect(today.querySelector('svg')).not.toBeNull();
    await userEvent.click(today);
    expect(onChange).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('radio', { name: '7 days' }));
    expect(onChange).toHaveBeenCalledWith('week');
  });

  it('is one tab stop; arrows, Home and End move and choose, wrapping at the ends', async () => {
    const onChange = vi.fn();
    function Stateful() {
      const [v, setV] = useState('week');
      return (
        <Segmented
          label="Period"
          options={[
            { value: 'today', label: 'Today' },
            { value: 'week', label: '7 days' },
            { value: 'month', label: '30 days' },
          ]}
          value={v}
          onChange={(next) => {
            onChange(next);
            setV(next);
          }}
        />
      );
    }
    render(
      <>
        <button type="button">Before</button>
        <Stateful />
      </>,
    );
    const radios = screen.getAllByRole('radio');
    expect(radios.map((r) => r.tabIndex)).toEqual([-1, 0, -1]);
    screen.getByRole('button', { name: 'Before' }).focus();
    await userEvent.tab();
    expect(screen.getByRole('radio', { name: '7 days' })).toHaveFocus();
    await userEvent.keyboard('{ArrowRight}');
    expect(screen.getByRole('radio', { name: '30 days' })).toHaveFocus();
    expect(screen.getByRole('radio', { name: '30 days' })).toBeChecked();
    await userEvent.keyboard('{ArrowRight}');
    expect(screen.getByRole('radio', { name: 'Today' })).toHaveFocus();
    await userEvent.keyboard('{ArrowLeft}');
    expect(screen.getByRole('radio', { name: '30 days' })).toHaveFocus();
    await userEvent.keyboard('{Home}');
    expect(screen.getByRole('radio', { name: 'Today' })).toBeChecked();
    await userEvent.keyboard('{End}');
    expect(screen.getByRole('radio', { name: '30 days' })).toBeChecked();
    await userEvent.keyboard('{ArrowUp}');
    expect(screen.getByRole('radio', { name: '7 days' })).toBeChecked();
    await userEvent.keyboard('{ArrowDown}');
    await userEvent.keyboard('a');
    expect(onChange).toHaveBeenLastCalledWith('month');
    expect(screen.getAllByRole('radio').map((r) => r.tabIndex)).toEqual([-1, -1, 0]);
  });

  it('keeps a tab stop when the value matches no option', () => {
    render(<Segmented label="Period" options={[{ value: 'a', label: 'A' }]} value={'z' as 'a'} onChange={vi.fn()} />);
    expect(screen.getByRole('radio', { name: 'A' })).toHaveAttribute('tabindex', '0');
  });
});
