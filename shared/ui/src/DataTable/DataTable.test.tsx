import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { DataTable, type ColumnDef } from './DataTable';

interface Row {
  id: string;
  name: string;
  qty: number;
}

const columns: ColumnDef<Row, unknown>[] = [
  { accessorKey: 'name', header: 'Item' },
  { accessorKey: 'qty', header: 'Qty', meta: { numeric: true } },
  { id: 'note', header: 'Note', enableSorting: false, cell: () => '—' },
];

const data: Row[] = [
  { id: 'a', name: 'Atta', qty: 12 },
  { id: 'b', name: 'Bread', qty: 3 },
  { id: 'c', name: 'Curd', qty: 8 },
];

const firstColumn = () =>
  screen
    .getAllByRole('row')
    .slice(1)
    .map((r) => within(r).getAllByRole('cell')[0]?.textContent);

describe('DataTable', () => {
  it('is named by its label, has sortable headers with aria-sort and right-aligned numeric cells', () => {
    render(<DataTable label="Stock" columns={columns} data={data} getRowId={(r) => r.id} emptyLabel="No items" />);
    expect(screen.getByRole('table', { name: 'Stock' })).toBeInTheDocument();
    const [item, qty, note] = screen.getAllByRole('columnheader');
    expect(item).toHaveAttribute('aria-sort', 'none');
    expect(qty).toHaveClass('num');
    expect(note).not.toHaveAttribute('aria-sort');
    expect(within(note as HTMLElement).queryByRole('button')).toBeNull();
    expect(screen.getByText('12').closest('td')).toHaveClass('num');
    expect(firstColumn()).toEqual(['Atta', 'Bread', 'Curd']);
  });

  it('sorts locally: ascending, descending, then off', () => {
    render(<DataTable label="Stock" columns={columns} data={data} getRowId={(r) => r.id} emptyLabel="No items" />);
    const qtyHeader = screen.getAllByRole('columnheader')[1] as HTMLElement;
    const sortQty = within(qtyHeader).getByRole('button', { name: 'Qty' });
    // TanStack starts numeric columns descending.
    fireEvent.click(sortQty);
    expect(qtyHeader).toHaveAttribute('aria-sort', 'descending');
    expect(firstColumn()).toEqual(['Atta', 'Curd', 'Bread']);
    fireEvent.click(sortQty);
    expect(qtyHeader).toHaveAttribute('aria-sort', 'ascending');
    expect(firstColumn()).toEqual(['Bread', 'Curd', 'Atta']);
    fireEvent.click(sortQty);
    expect(qtyHeader).toHaveAttribute('aria-sort', 'none');
  });

  it('controlled sorting reports changes and follows the given state', () => {
    const onSortingChange = vi.fn();
    render(
      <DataTable
        label="Stock"
        columns={columns}
        data={data}
        getRowId={(r) => r.id}
        emptyLabel="No items"
        sorting={[{ id: 'name', desc: true }]}
        onSortingChange={onSortingChange}
        manualSorting
        footer={<span>Showing 1–3 of 3</span>}
      />,
    );
    expect(screen.getAllByRole('columnheader')[0]).toHaveAttribute('aria-sort', 'descending');
    // manualSorting: the data order is the server's.
    expect(firstColumn()).toEqual(['Atta', 'Bread', 'Curd']);
    fireEvent.click(screen.getByRole('button', { name: 'Item' }));
    expect(onSortingChange).toHaveBeenCalled();
    expect(screen.getByText('Showing 1–3 of 3')).toBeInTheDocument();
  });

  it('shows skeleton rows while loading and one empty cell when there are no rows', () => {
    const { container, rerender } = render(
      <DataTable
        label="Stock"
        columns={columns}
        data={[]}
        getRowId={(r) => r.id}
        emptyLabel="No items"
        loading
        loadingRows={2}
      />,
    );
    expect(screen.getByRole('table')).toHaveAttribute('aria-busy', 'true');
    expect(container.querySelectorAll('tbody tr')).toHaveLength(2);
    expect(container.querySelectorAll('tbody .tz-ui-skeleton')).toHaveLength(6);
    rerender(<DataTable label="Stock" columns={columns} data={[]} getRowId={(r) => r.id} emptyLabel="No items" />);
    const empty = screen.getByText('No items');
    expect(empty).toHaveAttribute('colspan', '3');
    expect(screen.getByRole('table')).not.toHaveAttribute('aria-busy');
    expect(container.querySelector('.tz-table-foot')).toBeNull();
  });
});
