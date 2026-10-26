import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type OnChangeFn,
  type RowData,
  type SortingState,
} from '@tanstack/react-table';
import { ArrowDown, ArrowUp, ArrowUpDown } from 'lucide-react';
import { useState, type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';
import { Skeleton } from '../Skeleton/Skeleton';

declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars -- the generics must match TanStack's declaration
  interface ColumnMeta<TData extends RowData, TValue> {
    /** Right-aligned tabular numbers (prices, counts). */
    numeric?: boolean;
  }
}

export type { ColumnDef, SortingState };

export interface DataTableProps<T> {
  /**
   * Accessible name of the table, e.g. "Stock". Given as aria-label rather than an sr-only
   * caption: Chromium keeps a caption in the table's flow even when it is absolutely
   * positioned, which shifts the body 1px off the design.
   */
  label: string;
  columns: ColumnDef<T, unknown>[];
  data: readonly T[];
  /** Stable row id, e.g. (row) => row.id. */
  getRowId: (row: T) => string;
  /** Controlled sorting; leave unset for local sorting. Server-sorted tables pass manualSorting. */
  sorting?: SortingState | undefined;
  onSortingChange?: OnChangeFn<SortingState> | undefined;
  manualSorting?: boolean | undefined;
  /** Shown in one full-width cell when there are no rows. */
  emptyLabel: ReactNode;
  loading?: boolean | undefined;
  /** Rows of skeletons while loading. */
  loadingRows?: number | undefined;
  /** Footer bar, e.g. "Showing 1–25 of 312" and paging buttons. */
  footer?: ReactNode;
  className?: string | undefined;
}

/**
 * Dense data table (tz-table) on TanStack Table: sortable headers are buttons with aria-sort on the
 * header cell; numeric columns align right. Rows are 44px, headers 36px. The wrap scrolls sideways on
 * a narrow screen, so it is a focusable, named group: keyboard users can scroll it (WCAG 2.1.1).
 */
export function DataTable<T>({
  label,
  columns,
  data,
  getRowId,
  sorting,
  onSortingChange,
  manualSorting = false,
  emptyLabel,
  loading = false,
  loadingRows = 5,
  footer,
  className,
}: DataTableProps<T>) {
  const [localSorting, setLocalSorting] = useState<SortingState>([]);
  // TanStack Table returns a stable instance with methods; React Compiler skips memoising this component.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable<T>({
    columns,
    data: data as T[],
    getRowId,
    state: { sorting: sorting ?? localSorting },
    onSortingChange: onSortingChange ?? setLocalSorting,
    manualSorting,
    enableSortingRemoval: true,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });
  const colCount = table.getVisibleLeafColumns().length;
  const rows = table.getRowModel().rows;

  return (
    // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- a scroll container must take focus so a keyboard can scroll it (axe scrollable-region-focusable)
    <div className={cx('tz-table-wrap', className)} role="group" aria-label={label} tabIndex={0}>
      <table className="tz-table" aria-label={label} aria-busy={loading || undefined}>
        <thead>
          {table.getHeaderGroups().map((hg) => (
            <tr key={hg.id}>
              {hg.headers.map((h) => {
                const numeric = h.column.columnDef.meta?.numeric === true;
                const sorted = h.column.getIsSorted();
                const content = h.isPlaceholder ? null : flexRender(h.column.columnDef.header, h.getContext());
                return (
                  <th
                    key={h.id}
                    scope="col"
                    className={cx(numeric && 'num')}
                    aria-sort={
                      h.column.getCanSort()
                        ? sorted === 'asc'
                          ? 'ascending'
                          : sorted === 'desc'
                            ? 'descending'
                            : 'none'
                        : undefined
                    }
                  >
                    {h.column.getCanSort() ? (
                      <button type="button" className="tz-ui-sort" onClick={h.column.getToggleSortingHandler()}>
                        {content}
                        <Icon icon={sorted === 'asc' ? ArrowUp : sorted === 'desc' ? ArrowDown : ArrowUpDown} small />
                      </button>
                    ) : (
                      content
                    )}
                  </th>
                );
              })}
            </tr>
          ))}
        </thead>
        <tbody>
          {loading ? (
            Array.from({ length: loadingRows }, (_, r) => (
              <tr key={`sk-${r}`}>
                {table.getVisibleLeafColumns().map((c) => (
                  <td key={c.id} className={cx(c.columnDef.meta?.numeric && 'num')}>
                    <Skeleton width={c.columnDef.meta?.numeric ? '4ch' : '70%'} />
                  </td>
                ))}
              </tr>
            ))
          ) : rows.length === 0 ? (
            <tr>
              <td colSpan={colCount} className="tz-ui-table-empty">
                {emptyLabel}
              </td>
            </tr>
          ) : (
            rows.map((row) => (
              <tr key={row.id}>
                {row.getVisibleCells().map((cell) => (
                  <td key={cell.id} className={cx(cell.column.columnDef.meta?.numeric && 'num')}>
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </td>
                ))}
              </tr>
            ))
          )}
        </tbody>
      </table>
      {footer !== undefined && <div className="tz-table-foot">{footer}</div>}
    </div>
  );
}
