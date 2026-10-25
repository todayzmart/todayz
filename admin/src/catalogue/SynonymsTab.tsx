import type { AdminSynonym } from '@todayz/api-client/gen/model/adminSynonym';
import type { AdminSynonymLang } from '@todayz/api-client/gen/model/adminSynonymLang';
import { Button, DataTable, Dialog, TextField, useToast, type ColumnDef } from '@todayz/ui';
import { Plus, Trash2 } from 'lucide-react';
import { useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ulid } from '../app/search';
import { AuditPanel } from '../audit/AuditPanel';
import type { Go } from './CataloguePage';
import { LANGS, useCatalogueLabels } from './labels';
import { useCatalogueProblem } from './problem';
import { useCreateSynonym, useDeleteSynonym, useSynonyms } from './queries';
import { MAX_Q, type CatalogueSearch } from './search';
import css from '../app/page.module.css';

const MAX_TERM = 80;

/** Search synonyms: a word shoppers type that search reads as another (dahi for curd). */
export function SynonymsTab({ search, go }: { search: CatalogueSearch; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useCatalogueLabels();
  const problemText = useProblemText();
  const catalogueProblem = useCatalogueProblem();
  const manage = can(roles, 'catalogueManage');
  const q = useSynonyms(search.q ?? '');
  const remove = useDeleteSynonym();
  const [removing, setRemoving] = useState<AdminSynonym | null>(null);
  const rows = (q.data?.pages ?? []).flatMap((p) => p.items);
  const none = intl.formatMessage({ id: 'catalogue.none' });

  const columns = useMemo<ColumnDef<AdminSynonym, unknown>[]>(
    () => [
      {
        id: 'term',
        header: intl.formatMessage({ id: 'catalogue.col.term' }),
        cell: ({ row }) => (
          <Button
            size="sm"
            variant="ghost"
            aria-pressed={row.original.id === search.open}
            onClick={() => go({ ...search, open: row.original.id })}
          >
            {row.original.term}
          </Button>
        ),
      },
      { id: 'synonym', header: intl.formatMessage({ id: 'catalogue.col.synonym' }), accessorFn: (s) => s.synonym },
      { id: 'lang', header: intl.formatMessage({ id: 'catalogue.col.lang' }), accessorFn: (s) => labels.lang(s.lang) },
      { id: 'product', header: intl.formatMessage({ id: 'catalogue.col.product' }), accessorFn: (s) => s.productName ?? none },
      { id: 'added', header: intl.formatMessage({ id: 'catalogue.col.added' }), accessorFn: (s) => when(s.createdAt, s.createdAt) },
      ...(manage
        ? [
            {
              id: 'actions',
              header: intl.formatMessage({ id: 'catalogue.col.actions' }),
              cell: ({ row }: { row: { original: AdminSynonym } }) => (
                <Button
                  size="sm"
                  variant="ghost"
                  icon={Trash2}
                  aria-label={intl.formatMessage({ id: 'catalogue.synonym.removeOne' }, { term: row.original.term, synonym: row.original.synonym })}
                  onClick={() => {
                    remove.reset();
                    setRemoving(row.original);
                  }}
                >
                  {intl.formatMessage({ id: 'catalogue.synonym.remove' })}
                </Button>
              ),
            } satisfies ColumnDef<AdminSynonym, unknown>,
          ]
        : []),
    ],
    [intl, labels, when, none, manage, remove, search, go],
  );

  return (
    <div className={css.detail}>
      <div className={css.column}>
        <SynonymSearch key={search.q ?? ''} q={search.q ?? ''} onApply={(t) => go({ tab: 'synonyms', ...(t ? { q: t } : {}) })} />
        {q.isError ? (
          <p role="alert" className="tz-field-error">
            {problemText(q.error)}{' '}
            <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
              {intl.formatMessage({ id: 'common.retry' })}
            </Button>
          </p>
        ) : (
          <DataTable
            label={intl.formatMessage({ id: 'catalogue.synonyms.table' })}
            columns={columns}
            data={rows}
            getRowId={(s) => s.id}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: 'catalogue.synonyms.empty' })}
            footer={
              q.hasNextPage ? (
                <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                  {intl.formatMessage({ id: 'common.loadMore' })}
                </Button>
              ) : undefined
            }
          />
        )}
      </div>
      <div className={css.column}>
        {manage && <AddSynonym onAdded={(id) => go({ ...search, open: id })} />}
        {search.open ? (
          <AuditPanel type="synonym" id={search.open} />
        ) : (
          <p className={`tz-card ${css.muted}`}>{intl.formatMessage({ id: 'catalogue.pick.synonym' })}</p>
        )}
      </div>
      <Dialog
        open={removing !== null}
        onClose={() => setRemoving(null)}
        title={intl.formatMessage({ id: 'catalogue.synonym.removeTitle' })}
        description={removing ? intl.formatMessage({ id: 'catalogue.synonym.removeBody' }, { term: removing.term, synonym: removing.synonym }) : ''}
        closeLabel={intl.formatMessage({ id: 'common.close' })}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRemoving(null)}>
              {intl.formatMessage({ id: 'common.cancel' })}
            </Button>
            <Button
              variant="danger"
              loading={remove.isPending}
              loadingLabel={intl.formatMessage({ id: 'common.saving' })}
              onClick={() =>
                removing &&
                remove.mutate(removing.id, {
                  onSuccess: () => {
                    const id = removing.id;
                    setRemoving(null);
                    toast.show({ message: intl.formatMessage({ id: 'catalogue.synonym.removed' }) });
                    go({ ...search, open: id });
                  },
                })
              }
            >
              {intl.formatMessage({ id: 'catalogue.synonym.remove' })}
            </Button>
          </>
        }
      >
        {remove.error && (
          <p role="alert" className="tz-field-error">
            {catalogueProblem(remove.error)}
          </p>
        )}
      </Dialog>
    </div>
  );
}

function SynonymSearch({ q, onApply }: { q: string; onApply: (q: string) => void }) {
  const intl = useIntl();
  const [text, setText] = useState(q);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    onApply(text.trim().toLowerCase());
  };
  return (
    <form className={css.form} onSubmit={submit} role="search" aria-label={intl.formatMessage({ id: 'catalogue.synonyms.search' })}>
      <TextField
        label={intl.formatMessage({ id: 'catalogue.filter.prefix' })}
        value={text}
        maxLength={MAX_Q}
        autoComplete="off"
        onChange={(e) => setText(e.target.value)}
      />
      <Button type="submit" variant="primary">
        {intl.formatMessage({ id: 'catalogue.filter.apply' })}
      </Button>
      <Button type="button" variant="ghost" onClick={() => onApply('')}>
        {intl.formatMessage({ id: 'catalogue.filter.clear' })}
      </Button>
    </form>
  );
}

/** Add a pair; the API trims and lower-cases both words. A product id ties it to one product. */
function AddSynonym({ onAdded }: { onAdded: (id: string) => void }) {
  const intl = useIntl();
  const toast = useToast();
  const labels = useCatalogueLabels();
  const catalogueProblem = useCatalogueProblem();
  const create = useCreateSynonym();
  const [term, setTerm] = useState('');
  const [synonym, setSynonym] = useState('');
  const [lang, setLang] = useState<AdminSynonymLang>('en');
  const [product, setProduct] = useState('');
  const [touched, setTouched] = useState(false);
  const t = term.trim().toLowerCase();
  const s = synonym.trim().toLowerCase();
  const productId = product.trim() ? ulid(product) : undefined;
  const termBad = t.length === 0 || t.length > MAX_TERM;
  const synonymBad = s.length === 0 || s.length > MAX_TERM || s === t;
  const productBad = product.trim() !== '' && !productId;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (termBad || synonymBad || productBad || create.isPending) return;
    create.mutate(
      { term: t, synonym: s, lang, ...(productId ? { productId } : {}) },
      {
        onSuccess: (row) => {
          setTerm('');
          setSynonym('');
          setProduct('');
          setTouched(false);
          toast.show({ message: intl.formatMessage({ id: 'catalogue.synonym.added' }) });
          onAdded(row.id);
        },
      },
    );
  };
  return (
    <section className="tz-card" aria-labelledby="synonym-add">
      <h2 id="synonym-add" className="tz-card-title">
        {intl.formatMessage({ id: 'catalogue.synonym.addTitle' })}
      </h2>
      <form className={css.stack} onSubmit={submit} noValidate>
        <TextField
          label={intl.formatMessage({ id: 'catalogue.col.term' })}
          help={intl.formatMessage({ id: 'catalogue.field.termHelp' })}
          value={term}
          maxLength={MAX_TERM}
          autoComplete="off"
          onChange={(e) => setTerm(e.target.value)}
          error={touched && termBad ? intl.formatMessage({ id: 'catalogue.field.termError' }) : undefined}
        />
        <TextField
          label={intl.formatMessage({ id: 'catalogue.col.synonym' })}
          help={intl.formatMessage({ id: 'catalogue.field.synonymHelp' })}
          value={synonym}
          maxLength={MAX_TERM}
          autoComplete="off"
          onChange={(e) => setSynonym(e.target.value)}
          error={touched && synonymBad ? intl.formatMessage({ id: 'catalogue.error.same' }) : undefined}
        />
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="synonym-lang">
            {intl.formatMessage({ id: 'catalogue.col.lang' })}
          </label>
          <select id="synonym-lang" className="tz-input" value={lang} onChange={(e) => setLang(e.target.value as AdminSynonymLang)}>
            {LANGS.map((l) => (
              <option key={l} value={l}>
                {labels.lang(l)}
              </option>
            ))}
          </select>
        </div>
        <TextField
          label={intl.formatMessage({ id: 'catalogue.field.productId' })}
          help={intl.formatMessage({ id: 'catalogue.field.productIdHelp' })}
          value={product}
          maxLength={26}
          autoComplete="off"
          onChange={(e) => setProduct(e.target.value)}
          error={touched && productBad ? intl.formatMessage({ id: 'catalogue.field.productIdError' }) : undefined}
        />
        {create.error && (
          <p role="alert" className="tz-field-error">
            {catalogueProblem(create.error, 'synonym')}
          </p>
        )}
        <div className={css.actions}>
          <Button type="submit" variant="primary" icon={Plus} loading={create.isPending} loadingLabel={intl.formatMessage({ id: 'common.saving' })}>
            {intl.formatMessage({ id: 'catalogue.synonym.add' })}
          </Button>
        </div>
      </form>
    </section>
  );
}
