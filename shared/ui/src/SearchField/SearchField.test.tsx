import { createRef } from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { SearchField } from './SearchField';

describe('SearchField', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('debounces the query, waits for two characters and does not fire on mount', () => {
    const onSearch = vi.fn();
    const onChange = vi.fn();
    render(<SearchField label="Search" defaultValue="mi" onSearch={onSearch} onChange={onChange} />);
    act(() => vi.advanceTimersByTime(500));
    expect(onSearch).not.toHaveBeenCalled();

    const input = screen.getByRole('searchbox', { name: 'Search' });
    fireEvent.change(input, { target: { value: 'm' } });
    act(() => vi.advanceTimersByTime(500));
    expect(onSearch).not.toHaveBeenCalled();
    expect(onChange).toHaveBeenLastCalledWith('m');

    fireEvent.change(input, { target: { value: 'mil' } });
    act(() => vi.advanceTimersByTime(100));
    fireEvent.change(input, { target: { value: 'milk ' } });
    act(() => vi.advanceTimersByTime(199));
    expect(onSearch).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(1));
    expect(onSearch).toHaveBeenCalledTimes(1);
    expect(onSearch).toHaveBeenCalledWith('milk');
  });

  it('clears with the clear button, refocuses and searches for the empty string', () => {
    const onSearch = vi.fn();
    render(<SearchField label="Search" defaultValue="atta" clearLabel="Clear search" onSearch={onSearch} />);
    const input = screen.getByRole('searchbox');
    fireEvent.click(screen.getByRole('button', { name: 'Clear search' }));
    expect(input).toHaveValue('');
    expect(input).toHaveFocus();
    expect(screen.queryByRole('button', { name: 'Clear search' })).toBeNull();
    act(() => vi.advanceTimersByTime(200));
    expect(onSearch).toHaveBeenCalledWith('');
  });

  it('is controlled when value is given and renders the trailing slot', () => {
    const onChange = vi.fn();
    render(
      <SearchField
        label="Search"
        id="q"
        value="rice"
        onChange={onChange}
        trailing={<button type="button">Voice search</button>}
      />,
    );
    const input = screen.getByRole('searchbox');
    expect(input).toHaveAttribute('id', 'q');
    fireEvent.change(input, { target: { value: 'rice flour' } });
    expect(onChange).toHaveBeenCalledWith('rice flour');
    expect(input).toHaveValue('rice');
    expect(screen.getByRole('button', { name: 'Voice search' })).toBeInTheDocument();
  });

  it('forwards the ref to the input', () => {
    const ref = createRef<HTMLInputElement>();
    render(<SearchField ref={ref} label="Search products" />);
    expect(ref.current).toBe(screen.getByRole('searchbox', { name: 'Search products' }));
    ref.current?.focus();
    expect(screen.getByRole('searchbox')).toHaveFocus();
  });
});
