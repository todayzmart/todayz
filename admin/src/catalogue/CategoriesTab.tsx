import type { AdminCategory } from '@todayz/api-client/gen/model/adminCategory';
import type { AdminCategoryCreate } from '@todayz/api-client/gen/model/adminCategoryCreate';
import type { AdminCategoryNames } from '@todayz/api-client/gen/model/adminCategoryNames';
import type { AdminCategoryTint } from '@todayz/api-client/gen/model/adminCategoryTint';
import type { AdminCategoryUpdate } from '@todayz/api-client/gen/model/adminCategoryUpdate';
import { Button, DataTable, Dialog, TextField, useToast, type ColumnDef } from '@todayz/ui';
import { Pencil, Plus } from 'lucide-react';
import { Fragment, useId, useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { MAX_REASON } from '../app/ReasonDialog';
import { AuditPanel } from '../audit/AuditPanel';
import type { Go } from './CataloguePage';
import { isSlug } from './form';
import { LANGS, TINTS, useCatalogueLabels } from './labels';
import { useCatalogueProblem } from './problem';
import { useCategories, useCreateCategory, useUpdateCategory } from './queries';
import type { CatalogueSearch } from './search';
import css from '../app/page.module.css';

const MAX_NAME = 60;
const MAX_SORT = 10_000;

/** Categories with their names per language; admins add, rename, recolour and reorder them. */
export function CategoriesTab({ search, go }: { search: CatalogueSearch; go: Go }) {
  const intl = useIntl();
  const toast = useToast();
  const roles = useRoles();
  const labels = useCatalogueLabels();
  const problemText = useProblemText();
  const catalogueProblem = useCatalogueProblem();
  const manage = can(roles, 'catalogueManage');
  const q = useCategories();
  const rows = useMemo(() => q.data?.items ?? [], [q.data]);
  const create = useCreateCategory();
  const [adding, setAdding] = useState(false);
  const selected = search.open ? rows.find((c) => c.id === search.open) : undefined;

  const columns = useMemo<ColumnDef<AdminCategory, unknown>[]>(
    () => [
      {
        id: 'name',
        header: intl.formatMessage({ id: 'catalogue.col.category' }),
        cell: ({ row }) => (
          <Button
            size="sm"
            variant="ghost"
            aria-pressed={row.original.id === search.open}
            onClick={() => go({ tab: 'categories', open: row.original.id })}
          >
            {labels.category(row.original, rows)}
          </Button>
        ),
      },
      { id: 'slug', header: intl.formatMessage({ id: 'catalogue.col.slug' }), accessorFn: (c) => c.slug },
      { id: 'tint', header: intl.formatMessage({ id: 'catalogue.col.tint' }), accessorFn: (c) => labels.tint(c.tint) },
      { id: 'icon', header: intl.formatMessage({ id: 'catalogue.col.icon' }), accessorFn: (c) => c.icon },
      { id: 'sort', header: intl.formatMessage({ id: 'catalogue.col.sort' }), accessorFn: (c) => c.sort, meta: { numeric: true } },
      { id: 'products', header: intl.formatMessage({ id: 'catalogue.col.products' }), accessorFn: (c) => c.productCount, meta: { numeric: true } },
    ],
    [intl, labels, rows, search.open, go],
  );

  return (
    <div className={css.detail}>
      <div className={css.column}>
        {manage && (
          <div className={css.actions}>
            <Button
              variant="primary"
              icon={Plus}
              onClick={() => {
                create.reset();
                setAdding(true);
              }}
            >
              {intl.formatMessage({ id: 'catalogue.category.add' })}
            </Button>
          </div>
        )}
        {q.isError ? (
          <p role="alert" className="tz-field-error">
            {problemText(q.error)}{' '}
            <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
              {intl.formatMessage({ id: 'common.retry' })}
            </Button>
          </p>
        ) : (
          <DataTable
            label={intl.formatMessage({ id: 'catalogue.categories.table' })}
            columns={columns}
            data={rows}
            getRowId={(c) => c.id}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: 'catalogue.categories.empty' })}
          />
        )}
      </div>
      <div className={css.column}>
        {selected ? (
          <CategoryCard key={selected.id} c={selected} all={rows} />
        ) : (
          <p className={`tz-card ${css.muted}`}>{intl.formatMessage({ id: 'catalogue.pick.category' })}</p>
        )}
      </div>
      {adding && (
        <CategoryDialog
          all={rows}
          pending={create.isPending}
          error={create.error ? catalogueProblem(create.error, 'slug') : null}
          onSave={(body) =>
            'slug' in body &&
            create.mutate(body, {
              onSuccess: (c) => {
                setAdding(false);
                toast.show({ message: intl.formatMessage({ id: 'catalogue.category.added' }) });
                go({ tab: 'categories', open: c.id });
              },
            })
          }
          onClose={() => setAdding(false)}
        />
      )}
    </div>
  );
}

function CategoryCard({ c, all }: { c: AdminCategory; all: readonly AdminCategory[] }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useCatalogueLabels();
  const catalogueProblem = useCatalogueProblem();
  const update = useUpdateCategory(c.id);
  const [editing, setEditing] = useState(false);
  const none = intl.formatMessage({ id: 'catalogue.none' });
  return (
    <>
      <section className="tz-card" aria-labelledby={`category-${c.id}`}>
        <h2 id={`category-${c.id}`} className="tz-card-title">
          {labels.category(c, all)}
        </h2>
        <dl className={css.facts}>
          <dt>{intl.formatMessage({ id: 'catalogue.col.slug' })}</dt>
          <dd>{c.slug}</dd>
          {LANGS.map((l) => (
            <Fragment key={l}>
              <dt>{labels.lang(l)}</dt>
              <dd>{c.names[l] ?? none}</dd>
            </Fragment>
          ))}
          <dt>{intl.formatMessage({ id: 'catalogue.col.tint' })}</dt>
          <dd>{labels.tint(c.tint)}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.icon' })}</dt>
          <dd>{c.icon}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.sort' })}</dt>
          <dd className={css.mono}>{c.sort}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.products' })}</dt>
          <dd className={css.mono}>{c.productCount}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.updated' })}</dt>
          <dd>{when(c.updatedAt, c.updatedAt)}</dd>
        </dl>
        {can(roles, 'catalogueManage') && (
          <div className={css.actions}>
            <Button
              icon={Pencil}
              onClick={() => {
                update.reset();
                setEditing(true);
              }}
            >
              {intl.formatMessage({ id: 'catalogue.category.edit' })}
            </Button>
          </div>
        )}
      </section>
      <AuditPanel type="category" id={c.id} />
      {editing && (
        <CategoryDialog
          category={c}
          all={all}
          pending={update.isPending}
          error={update.error ? catalogueProblem(update.error) : null}
          onSave={(body) =>
            'reason' in body &&
            update.mutate(body, {
              onSuccess: () => {
                setEditing(false);
                toast.show({ message: intl.formatMessage({ id: 'catalogue.category.saved' }) });
              },
            })
          }
          onClose={() => setEditing(false)}
        />
      )}
    </>
  );
}

interface CategoryDialogProps {
  /** Absent adds a category. */
  category?: AdminCategory;
  all: readonly AdminCategory[];
  pending: boolean;
  error: string | null;
  onSave: (body: AdminCategoryCreate | AdminCategoryUpdate) => void;
  onClose: () => void;
}

/** Add a category (slug and parent are fixed once made) or change its names, tint, icon and order with a reason. */
function CategoryDialog({ category: c, all, pending, error, onSave, onClose }: CategoryDialogProps) {
  const intl = useIntl();
  const id = useId();
  const labels = useCatalogueLabels();
  const [slug, setSlug] = useState('');
  const [parentId, setParentId] = useState('');
  const [names, setNames] = useState<Record<string, string>>(() => Object.fromEntries(LANGS.map((l) => [l, c?.names[l] ?? ''])));
  const [tint, setTint] = useState<AdminCategoryTint>(c?.tint ?? 'green');
  const [icon, setIcon] = useState(c?.icon ?? '');
  const [sort, setSort] = useState(c ? String(c.sort) : '');
  const [reason, setReason] = useState('');
  const [touched, setTouched] = useState(false);

  const clean: AdminCategoryNames = { en: (names.en ?? '').trim() };
  for (const l of LANGS) {
    const v = (names[l] ?? '').trim();
    if (l !== 'en' && v) clean[l] = v;
  }
  const namesBad = LANGS.some((l) => (names[l] ?? '').trim().length > MAX_NAME) || clean.en.length === 0;
  const slugBad = !c && !isSlug(slug.trim());
  const iconBad = !isSlug(icon.trim()) || icon.trim().length > 40;
  const sortN = sort.trim() === '' ? null : Number(sort.trim());
  const sortBad = sortN !== null && (!Number.isInteger(sortN) || sortN < 0 || sortN > MAX_SORT);
  const cleanReason = reason.trim();
  const reasonBad = !!c && (cleanReason.length === 0 || cleanReason.length > MAX_REASON);
  // Only a top-level category may be a parent (one level of children).
  const parents = all.filter((p) => p.parentId === null);

  const build = (): AdminCategoryCreate | AdminCategoryUpdate | null => {
    if (namesBad || slugBad || iconBad || sortBad || reasonBad) return null;
    if (!c) {
      const body: AdminCategoryCreate = { slug: slug.trim(), names: clean, tint, icon: icon.trim() };
      if (parentId) body.parentId = parentId;
      if (sortN !== null) body.sort = sortN;
      return body;
    }
    const body: AdminCategoryUpdate = { reason: cleanReason };
    if (LANGS.some((l) => (clean[l] ?? '') !== (c.names[l] ?? ''))) body.names = clean;
    if (tint !== c.tint) body.tint = tint;
    if (icon.trim() !== c.icon) body.icon = icon.trim();
    if (sortN !== null && sortN !== c.sort) body.sort = sortN;
    return Object.keys(body).length > 1 ? body : null;
  };
  const body = build();
  const unchanged = !!c && body === null && !namesBad && !iconBad && !sortBad && !reasonBad;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (body && !pending) onSave(body);
  };

  return (
    <Dialog
      open
      onClose={onClose}
      title={c ? intl.formatMessage({ id: 'catalogue.category.editTitle' }, { name: c.names.en }) : intl.formatMessage({ id: 'catalogue.category.addTitle' })}
      description={intl.formatMessage({ id: c ? 'catalogue.category.editBody' : 'catalogue.category.addBody' })}
      closeLabel={intl.formatMessage({ id: 'common.close' })}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {intl.formatMessage({ id: 'common.cancel' })}
          </Button>
          <Button type="submit" form={id} variant="primary" loading={pending} loadingLabel={intl.formatMessage({ id: 'common.saving' })}>
            {intl.formatMessage({ id: c ? 'catalogue.category.save' : 'catalogue.category.add' })}
          </Button>
        </>
      }
    >
      <form id={id} onSubmit={submit} noValidate>
        {!c && (
          <>
            <TextField
              label={intl.formatMessage({ id: 'catalogue.field.slug' })}
              help={intl.formatMessage({ id: 'catalogue.field.slugHelp' })}
              value={slug}
              maxLength={64}
              autoComplete="off"
              onChange={(e) => setSlug(e.target.value)}
              error={touched && slugBad ? intl.formatMessage({ id: 'catalogue.field.slugError' }) : undefined}
            />
            <div className="tz-field">
              <label className="tz-field-label" htmlFor={`${id}-parent`}>
                {intl.formatMessage({ id: 'catalogue.field.parent' })}
              </label>
              <select id={`${id}-parent`} className="tz-input" value={parentId} onChange={(e) => setParentId(e.target.value)}>
                <option value="">{intl.formatMessage({ id: 'catalogue.field.parentNone' })}</option>
                {parents.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.names.en}
                  </option>
                ))}
              </select>
            </div>
          </>
        )}
        {LANGS.map((l) => (
          <TextField
            key={l}
            label={intl.formatMessage({ id: 'catalogue.field.nameIn' }, { lang: labels.lang(l) })}
            help={l === 'en' ? undefined : intl.formatMessage({ id: 'catalogue.field.optional' })}
            value={names[l] ?? ''}
            maxLength={MAX_NAME}
            onChange={(e) => setNames((n) => ({ ...n, [l]: e.target.value }))}
            error={touched && l === 'en' && clean.en.length === 0 ? intl.formatMessage({ id: 'catalogue.field.nameError' }) : undefined}
          />
        ))}
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={`${id}-tint`}>
            {intl.formatMessage({ id: 'catalogue.col.tint' })}
          </label>
          <select id={`${id}-tint`} className="tz-input" value={tint} onChange={(e) => setTint(e.target.value as AdminCategoryTint)}>
            {TINTS.map((t) => (
              <option key={t} value={t}>
                {labels.tint(t)}
              </option>
            ))}
          </select>
        </div>
        <TextField
          label={intl.formatMessage({ id: 'catalogue.col.icon' })}
          help={intl.formatMessage({ id: 'catalogue.field.iconHelp' })}
          value={icon}
          maxLength={40}
          autoComplete="off"
          onChange={(e) => setIcon(e.target.value)}
          error={touched && iconBad ? intl.formatMessage({ id: 'catalogue.field.iconError' }) : undefined}
        />
        <TextField
          label={intl.formatMessage({ id: 'catalogue.col.sort' })}
          help={intl.formatMessage({ id: 'catalogue.field.sortHelp' })}
          inputMode="numeric"
          className="tz-num"
          maxLength={5}
          value={sort}
          onChange={(e) => setSort(e.target.value)}
          error={touched && sortBad ? intl.formatMessage({ id: 'catalogue.field.sortError' }) : undefined}
        />
        {c && (
          <div className="tz-field">
            <label className="tz-field-label" htmlFor={`${id}-reason`}>
              {intl.formatMessage({ id: 'common.reason.label' })}
            </label>
            <textarea
              id={`${id}-reason`}
              className="tz-input"
              rows={3}
              maxLength={MAX_REASON}
              value={reason}
              aria-invalid={touched && reasonBad}
              aria-describedby={`${id}-reason-help`}
              onChange={(e) => setReason(e.target.value)}
            />
            <p id={`${id}-reason-help`} className={touched && reasonBad ? 'tz-field-error' : 'tz-field-help'}>
              {intl.formatMessage({ id: touched && reasonBad ? 'common.reason.required' : 'common.reason.help' })}
            </p>
          </div>
        )}
        {touched && unchanged && (
          <p role="alert" className="tz-field-error">
            {intl.formatMessage({ id: 'catalogue.error.unchanged' })}
          </p>
        )}
        {error && (
          <p role="alert" className="tz-field-error">
            {error}
          </p>
        )}
      </form>
    </Dialog>
  );
}
