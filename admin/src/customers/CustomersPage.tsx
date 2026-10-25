import { searchAdminCustomers } from '@todayz/api-client/gen/admin-customers/admin-customers';
import type { AdminCustomerSummary } from '@todayz/api-client/gen/model/adminCustomerSummary';
import { Button, DataTable, StatusPill, TextField, type ColumnDef } from '@todayz/ui';
import { useMutation } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Search } from 'lucide-react';
import { useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { useProblemText } from '../app/problems';
import { statusTone, useCustomerLabels } from './labels';
import { mobileDigits } from './phone';
import css from '../app/page.module.css';

/**
 * A-06 Customers: exact phone search only (the server matches the keyed hash, so there is no list
 * to scroll). The number stays in component state, never in the URL, history or a query key.
 */
export function CustomersPage() {
  const intl = useIntl();
  const when = useWhen();
  const labels = useCustomerLabels();
  const problemText = useProblemText();
  const [phone, setPhone] = useState('');
  const [invalid, setInvalid] = useState(false);
  const search = useMutation({ mutationFn: (digits: string) => searchAdminCustomers({ phone: digits }) });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const digits = mobileDigits(phone);
    setInvalid(digits === null);
    if (digits) search.mutate(digits);
  };

  const columns = useMemo<ColumnDef<AdminCustomerSummary, unknown>[]>(
    () => [
      {
        id: 'name',
        header: intl.formatMessage({ id: 'customers.col.name' }),
        cell: ({ row }) => (
          <Link to="/customers/$customerId" params={{ customerId: row.original.id }}>
            {row.original.name ?? intl.formatMessage({ id: 'customers.noName' })}
          </Link>
        ),
      },
      { id: 'phone', header: intl.formatMessage({ id: 'customers.col.phone' }), accessorFn: (c) => c.phoneMasked ?? '—' },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'customers.col.status' }),
        cell: ({ row }) => <StatusPill tone={statusTone(row.original.status)}>{labels.status(row.original.status)}</StatusPill>,
      },
      { id: 'strikes', header: intl.formatMessage({ id: 'customers.col.strikes' }), accessorFn: (c) => c.activeStrikes, meta: { numeric: true } },
      {
        id: 'joined',
        header: intl.formatMessage({ id: 'customers.col.joined' }),
        accessorFn: (c) => when(c.createdAt, search.data?.serverNow ?? c.createdAt),
      },
    ],
    [intl, labels, when, search.data?.serverNow],
  );

  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.customers' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'customers.sub' })}</p>
      </div>
      <form className={css.form} onSubmit={submit} noValidate role="search" aria-label={intl.formatMessage({ id: 'customers.search.label' })}>
        <TextField
          label={intl.formatMessage({ id: 'customers.search.phone' })}
          prefix="+91"
          inputMode="tel"
          autoComplete="off"
          value={phone}
          onChange={(e) => setPhone(e.target.value)}
          help={intl.formatMessage({ id: 'customers.search.help' })}
          error={invalid ? intl.formatMessage({ id: 'customers.search.invalid' }) : undefined}
        />
        <Button type="submit" variant="primary" icon={Search} loading={search.isPending} loadingLabel={intl.formatMessage({ id: 'customers.search.searching' })}>
          {intl.formatMessage({ id: 'customers.search.submit' })}
        </Button>
      </form>
      {search.isError && (
        <p role="alert" className="tz-field-error">
          {problemText(search.error)}
        </p>
      )}
      {search.isSuccess && (
        <DataTable
          label={intl.formatMessage({ id: 'customers.results' })}
          columns={columns}
          data={search.data.items}
          getRowId={(c) => c.id}
          emptyLabel={intl.formatMessage({ id: 'customers.search.none' })}
        />
      )}
    </div>
  );
}
