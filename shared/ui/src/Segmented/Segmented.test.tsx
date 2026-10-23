import { render, screen } from '@testing-library/react';
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
    const today = screen.getByRole('button', { name: 'Today' });
    expect(today).toHaveAttribute('aria-pressed', 'true');
    expect(today.querySelector('svg')).not.toBeNull();
    await userEvent.click(today);
    expect(onChange).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: '7 days' }));
    expect(onChange).toHaveBeenCalledWith('week');
  });
});
