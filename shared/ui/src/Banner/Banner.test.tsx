import { render, screen } from '@testing-library/react';
import { WifiOff } from 'lucide-react';
import { describe, expect, it } from 'vitest';
import { Banner } from './Banner';

describe('Banner', () => {
  it('is info by default, not live, with a spaced title and body', () => {
    const { container } = render(<Banner title="Heads up.">Prices were updated.</Banner>);
    expect(container.firstChild).toHaveClass('tz-banner');
    expect(container.firstChild).not.toHaveClass('tz-banner-warning');
    expect(container.firstChild).not.toHaveAttribute('role');
    expect(container).toHaveTextContent('Heads up. Prices were updated.');
    expect(container.querySelector('svg')).not.toBeNull();
  });

  it('warning tone, live status, custom icon and an action', () => {
    render(
      <Banner tone="warning" live="status" icon={WifiOff} action={<button type="button">Retry</button>}>
        You are offline
      </Banner>,
    );
    expect(screen.getByRole('status')).toHaveClass('tz-banner', 'tz-banner-warning');
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });

  it('a title alone has no trailing space', () => {
    const { container } = render(<Banner tone="warning" title="Only a title" />);
    expect(container.querySelector('.tz-ui-grow')?.textContent).toBe('Only a title');
  });
});
