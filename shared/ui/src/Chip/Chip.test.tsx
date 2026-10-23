import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Filter } from 'lucide-react';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { radiusOptions } from '../test/fixtures';
import { Chip, ChipGroup, RadiusChips } from './Chip';

describe('Chip', () => {
  it('toggles aria-pressed through onPressedChange', async () => {
    const onPressedChange = vi.fn();
    render(
      <Chip pressed={false} onPressedChange={onPressedChange} icon={Filter}>
        Open now
      </Chip>,
    );
    const chip = screen.getByRole('button', { name: 'Open now' });
    expect(chip).toHaveAttribute('aria-pressed', 'false');
    expect(chip.querySelector('svg.tz-ico')).not.toBeNull();
    await userEvent.click(chip);
    expect(onPressedChange).toHaveBeenCalledWith(true);
  });

  it('lets onClick cancel the toggle with preventDefault', async () => {
    const onPressedChange = vi.fn();
    const onClick = vi.fn((e: { preventDefault: () => void }) => e.preventDefault());
    render(
      <Chip pressed onPressedChange={onPressedChange} onClick={onClick}>
        Veg
      </Chip>,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Veg' }));
    expect(onClick).toHaveBeenCalled();
    expect(onPressedChange).not.toHaveBeenCalled();
  });
});

const fruit = [
  { value: 'a', label: 'Apples' },
  { value: 'b', label: 'Bananas', disabled: true },
  { value: 'c', label: 'Cherries' },
] as const;

describe('ChipGroup', () => {
  it('single choice keeps the pressed chip and changes to another', async () => {
    const onChange = vi.fn();
    render(<ChipGroup<string> label="Fruit" options={fruit} value="a" onChange={onChange} />);
    expect(screen.getByRole('group', { name: 'Fruit' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Apples' }));
    expect(onChange).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: 'Cherries' }));
    expect(onChange).toHaveBeenCalledWith('c');
  });

  it('multiple choice adds and removes values', async () => {
    function Multi() {
      const [v, setV] = useState<string[]>(['a']);
      return <ChipGroup<string> multiple label="Fruit" options={fruit} value={v} onChange={setV} />;
    }
    render(<Multi />);
    const apples = screen.getByRole('button', { name: 'Apples' });
    const cherries = screen.getByRole('button', { name: 'Cherries' });
    await userEvent.click(cherries);
    expect(cherries).toHaveAttribute('aria-pressed', 'true');
    await userEvent.click(apples);
    expect(apples).toHaveAttribute('aria-pressed', 'false');
  });

  it('arrow keys move focus across enabled chips and wrap', async () => {
    render(<ChipGroup<string> label="Fruit" options={fruit} value="a" onChange={() => undefined} />);
    const apples = screen.getByRole('button', { name: 'Apples' });
    const cherries = screen.getByRole('button', { name: 'Cherries' });
    apples.focus();
    await userEvent.keyboard('{ArrowRight}');
    expect(cherries).toHaveFocus();
    await userEvent.keyboard('{ArrowRight}');
    expect(apples).toHaveFocus();
    await userEvent.keyboard('{ArrowLeft}');
    expect(cherries).toHaveFocus();
    await userEvent.keyboard('{ArrowDown}');
    expect(cherries).toHaveFocus();
  });
});

describe('RadiusChips', () => {
  it('passes the chosen km up', async () => {
    const onChange = vi.fn();
    render(<RadiusChips label="Search radius" options={radiusOptions} value={2} onChange={onChange} />);
    expect(screen.getByRole('button', { name: '2 km' })).toHaveAttribute('aria-pressed', 'true');
    await userEvent.click(screen.getByRole('button', { name: '5 km' }));
    expect(onChange).toHaveBeenCalledWith(5);
  });
});
