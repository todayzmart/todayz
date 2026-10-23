import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { LanguagePicker } from './LanguagePicker';

const options = [
  { code: 'en', native: 'English', name: 'English' },
  { code: 'kn', native: 'ಕನ್ನಡ', name: 'Kannada' },
];

describe('LanguagePicker', () => {
  it('marks the current language, sets lang on the native name, and reports a change', async () => {
    const onChange = vi.fn();
    render(<LanguagePicker label="Language" options={options} value="en" onChange={onChange} />);
    expect(screen.getByRole('group', { name: 'Language' })).toBeInTheDocument();
    const kn = screen.getByRole('button', { name: /Kannada/ });
    expect(kn).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByText('ಕನ್ನಡ')).toHaveAttribute('lang', 'kn');
    await userEvent.click(screen.getByRole('button', { name: /^English/ }));
    expect(onChange).not.toHaveBeenCalled();
    await userEvent.click(kn);
    expect(onChange).toHaveBeenCalledWith('kn');
  });
});
