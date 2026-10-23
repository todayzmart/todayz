import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it } from 'vitest';
import { Tabs } from './Tabs';

type V = 'new' | 'ready' | 'done';

function Orders({ withPanels = true }: { withPanels?: boolean }) {
  const [v, setV] = useState<V>('new');
  const panel = (t: string) => (withPanels ? <p>{t} orders</p> : undefined);
  return (
    <Tabs<V>
      label="Orders"
      value={v}
      onChange={setV}
      tabs={[
        { value: 'new', label: 'New', count: 3, panel: panel('New') },
        { value: 'ready', label: 'Ready', panel: panel('Ready') },
        { value: 'done', label: 'Done', panel: panel('Done') },
      ]}
    />
  );
}

describe('Tabs', () => {
  it('wires tabs to the selected panel', () => {
    render(<Orders />);
    const tab = screen.getByRole('tab', { name: 'New 3' });
    expect(tab).toHaveAttribute('aria-selected', 'true');
    expect(tab).toHaveAttribute('tabindex', '0');
    const panel = screen.getByRole('tabpanel');
    expect(panel).toHaveAccessibleName('New 3');
    expect(tab).toHaveAttribute('aria-controls', panel.id);
    expect(screen.getByRole('tab', { name: 'Ready' })).toHaveAttribute('tabindex', '-1');
  });

  it('selects with click, arrows (wrapping), Home and End', async () => {
    render(<Orders />);
    await userEvent.click(screen.getByRole('tab', { name: 'Ready' }));
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Ready orders');
    await userEvent.keyboard('{ArrowRight}');
    expect(screen.getByRole('tab', { name: 'Done' })).toHaveFocus();
    await userEvent.keyboard('{ArrowRight}');
    expect(screen.getByRole('tab', { name: 'New 3' })).toHaveAttribute('aria-selected', 'true');
    await userEvent.keyboard('{ArrowLeft}');
    expect(screen.getByRole('tab', { name: 'Done' })).toHaveAttribute('aria-selected', 'true');
    await userEvent.keyboard('{Home}');
    expect(screen.getByRole('tab', { name: 'New 3' })).toHaveFocus();
    await userEvent.keyboard('{End}');
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Done orders');
    await userEvent.keyboard('{ArrowDown}');
    expect(screen.getByRole('tab', { name: 'Done' })).toHaveFocus();
  });

  it('renders no panel when tabs carry none', () => {
    render(<Orders withPanels={false} />);
    expect(screen.queryByRole('tabpanel')).toBeNull();
    expect(screen.getByRole('tab', { name: 'Ready' })).not.toHaveAttribute('aria-controls');
  });
});
