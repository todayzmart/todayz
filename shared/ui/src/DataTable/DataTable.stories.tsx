import type { Meta, StoryObj } from '@storybook/react-vite';
import { Button } from '../Button/Button';
import { StockTag, type StockLevel } from '../StockTag/StockTag';
import { designRef } from '../test/fixtures';
import { DataTable, type ColumnDef } from './DataTable';

interface StockRow {
  id: string;
  name: string;
  detail: string;
  price: number;
  priceLabel: string;
  stock: number;
  change: string;
  when: string;
  level: StockLevel;
  levelLabel: string;
}

const rows: readonly StockRow[] = [
  {
    id: 'atta',
    name: 'Aashirvaad Shudh Chakki Atta',
    detail: '5 kg · Staples',
    price: 289,
    priceLabel: '₹289',
    stock: 24,
    change: 'POS sale',
    when: '12 min ago',
    level: 'in',
    levelLabel: 'In stock',
  },
  {
    id: 'oil',
    name: 'Fortune Sunlite Sunflower Oil',
    detail: '1 L · Oils',
    price: 165,
    priceLabel: '₹165',
    stock: 3,
    change: 'Online order',
    when: '1 hr ago',
    level: 'low',
    levelLabel: 'Low',
  },
  {
    id: 'salt',
    name: 'Tata Salt',
    detail: '1 kg · Staples',
    price: 28,
    priceLabel: '₹28',
    stock: 0,
    change: 'Voice update',
    when: 'Today, 9:15 am',
    level: 'out',
    levelLabel: 'Out',
  },
  {
    id: 'maggi',
    name: 'Maggi 2-Minute Noodles',
    detail: '4 × 70 g · Snacks',
    price: 56,
    priceLabel: '₹56',
    stock: 40,
    change: 'Delivery received',
    when: 'Yesterday',
    level: 'in',
    levelLabel: 'In stock',
  },
];

function columns(sortable: boolean): ColumnDef<StockRow, unknown>[] {
  return [
    {
      id: 'product',
      header: 'Product',
      accessorFn: (r) => r.name,
      enableSorting: sortable,
      cell: ({ row }) => (
        <>
          {row.original.name}
          <span className="tz-sub">{row.original.detail}</span>
        </>
      ),
    },
    {
      id: 'price',
      header: 'Price',
      accessorFn: (r) => r.price,
      enableSorting: sortable,
      meta: { numeric: true },
      cell: ({ row }) => row.original.priceLabel,
    },
    { id: 'stock', header: 'Stock', accessorFn: (r) => r.stock, enableSorting: sortable, meta: { numeric: true } },
    {
      id: 'change',
      header: 'Last change',
      enableSorting: false,
      cell: ({ row }) => (
        <>
          {row.original.change}
          <span className="tz-sub">{row.original.when}</span>
        </>
      ),
    },
    {
      id: 'status',
      header: 'Status',
      enableSorting: false,
      cell: ({ row }) => <StockTag level={row.original.level} label={row.original.levelLabel} />,
    },
  ];
}

const meta = {
  title: 'Dashboard/DataTable',
  component: DataTable<StockRow>,
  args: {
    label: 'Stock',
    columns: columns(true),
    data: rows,
    getRowId: (r: StockRow) => r.id,
    emptyLabel: 'No products match these filters.',
  },
} satisfies Meta<typeof DataTable<StockRow>>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Sortable: Story = {};

export const SortedByStock: Story = { args: { sorting: [{ id: 'stock', desc: false }] } };

export const Loading: Story = { args: { loading: true, loadingRows: 4 } };

export const Empty: Story = { args: { data: [] } };

export const WithFooter: Story = {
  args: {
    footer: (
      <>
        <span>Showing 1–4 of 312</span>
        <span className="tz-row" style={{ padding: 0 }}>
          <Button size="sm" disabled>
            Previous
          </Button>
          <Button size="sm">Next</Button>
        </span>
      </>
    ),
  },
};

export const DesignReference: Story = {
  ...designRef('DataTable', 330),
  args: { columns: columns(false) },
  render: (args) => (
    <div style={{ padding: 'var(--space-4)' }}>
      <DataTable {...args} />
    </div>
  ),
};
