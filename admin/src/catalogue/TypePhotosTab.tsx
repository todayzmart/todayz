import type { AdminTypeKind } from '@todayz/api-client/gen/model/adminTypeKind';
import type { AdminTypePhoto } from '@todayz/api-client/gen/model/adminTypePhoto';
import { Button, useToast } from '@todayz/ui';
import { Check, X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ReasonDialog } from '../app/ReasonDialog';
import type { Go } from './CataloguePage';
import { imageUrl } from './image';
import { ProductThumb } from './ProductThumb';
import { useCatalogueProblem } from './problem';
import { useDecideTypePhoto, useTypeKinds, type ImageDecision } from './queries';
import type { CatalogueSearch } from './search';
import css from '../app/page.module.css';
import img from './images.module.css';

/**
 * Item photos (D-253): one free-licence Commons photo per kind of item ("toor dal") in a category,
 * shown on every product of that kind until a pack photo of its own is approved. The importer finds
 * up to three candidates per kind; an admin picks one or turns them down.
 */
export function TypePhotosTab({ search, go }: { search: CatalogueSearch; go: Go }) {
  const intl = useIntl();
  const problemText = useProblemText();
  const status = search.photos ?? 'pending';
  const q = useTypeKinds(status);
  const pages = q.data?.pages;
  const kinds = useMemo(() => (pages ?? []).flatMap((p) => p.items), [pages]);

  return (
    <div className={css.column}>
      <div className={css.head}>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="catalogue-type-status">
            {intl.formatMessage({ id: 'catalogue.types.filter' })}
          </label>
          <select
            id="catalogue-type-status"
            className="tz-input"
            value={status}
            onChange={(e) => go({ tab: 'types', ...(e.target.value === 'approved' ? { photos: 'approved' } : {}) })}
          >
            <option value="pending">{intl.formatMessage({ id: 'catalogue.types.filter.pending' })}</option>
            <option value="approved">{intl.formatMessage({ id: 'catalogue.types.filter.approved' })}</option>
          </select>
        </div>
      </div>
      <p className={css.muted}>{intl.formatMessage({ id: 'catalogue.types.help' })}</p>
      {q.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : q.isPending ? (
        <p className={css.muted}>{intl.formatMessage({ id: 'catalogue.types.loading' })}</p>
      ) : kinds.length === 0 ? (
        <p className={`tz-card ${css.muted}`}>
          {intl.formatMessage({ id: status === 'approved' ? 'catalogue.types.emptyApproved' : 'catalogue.types.empty' })}
        </p>
      ) : (
        <>
          {kinds.map((k) => (
            <KindCard key={`${k.categoryId}.${k.typeKey}`} kind={k} />
          ))}
          {q.hasNextPage && (
            <div>
              <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                {intl.formatMessage({ id: 'common.loadMore' })}
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}

/** One kind: what it covers, and its photo or the candidates to pick from. */
function KindCard({ kind }: { kind: AdminTypeKind }) {
  const intl = useIntl();
  const id = `kind-${kind.categoryId}-${kind.typeKey.replace(/\W+/g, '-')}`;
  return (
    <section className="tz-card" aria-labelledby={id}>
      <h2 id={id} className={`tz-card-title ${img.kindTitle}`}>
        {kind.typeKey}
      </h2>
      <p className={css.muted}>
        {intl.formatMessage(
          { id: 'catalogue.types.meta' },
          { category: kind.categoryName, n: kind.productCount, showing: kind.showing },
        )}
      </p>
      {kind.samples.length > 0 && (
        <p className={css.muted}>{intl.formatMessage({ id: 'catalogue.types.samples' }, { names: kind.samples.join(', ') })}</p>
      )}
      <ul className={img.candidates}>
        {kind.photos.map((p) => (
          <PhotoChoice key={p.id} kind={kind} photo={p} />
        ))}
      </ul>
    </section>
  );
}

/** One candidate (or the picked photo) with its licence, credit, Commons page and the decisions. */
function PhotoChoice({ kind, photo }: { kind: AdminTypeKind; photo: AdminTypePhoto }) {
  const intl = useIntl();
  const toast = useToast();
  const roles = useRoles();
  const catalogueProblem = useCatalogueProblem();
  const decide = useDecideTypePhoto(photo.id);
  const [asking, setAsking] = useState<ImageDecision['kind'] | null>(null);
  const may = can(roles, 'catalogueReview');
  const approved = photo.status === 'approved';
  const src = (approved ? imageUrl(photo.imageKey) : undefined) ?? photo.previewUrl ?? undefined;
  const ask = (k: ImageDecision['kind']) => {
    decide.reset();
    setAsking(k);
  };
  const run = (d: ImageDecision) =>
    decide.mutate(d, {
      onSuccess: (r) => {
        setAsking(null);
        const done = d.kind === 'approve' ? 'catalogue.types.picked' : approved ? 'catalogue.types.takenDown' : 'catalogue.types.turnedDown';
        toast.show({ message: intl.formatMessage({ id: done }, { name: kind.typeKey, n: r.products }) });
      },
    });

  return (
    <li className={img.candidate}>
      <figure className={img.figure}>
        <ProductThumb large name={kind.typeKey} src={src} alt={intl.formatMessage({ id: 'catalogue.types.alt' }, { name: kind.typeKey, rank: photo.rank })} />
        <figcaption>
          {approved ? intl.formatMessage({ id: 'catalogue.types.inUse' }) : intl.formatMessage({ id: 'catalogue.types.rank' }, { rank: photo.rank })}
        </figcaption>
      </figure>
      <p className={img.credit}>
        {photo.licence} · {photo.attribution}
        {' · '}
        <a href={photo.pageUrl} target="_blank" rel="noopener noreferrer">
          {intl.formatMessage({ id: 'catalogue.types.commonsPage' })}
        </a>
      </p>
      {may && (
        <div className={css.actions}>
          {approved ? (
            <Button size="sm" variant="danger" icon={X} onClick={() => ask('reject')}>
              {intl.formatMessage({ id: 'catalogue.types.takeDown' })}
            </Button>
          ) : (
            <>
              <Button size="sm" variant="primary" icon={Check} onClick={() => ask('approve')}>
                {intl.formatMessage({ id: 'catalogue.types.pick' })}
              </Button>
              <Button size="sm" variant="ghost" icon={X} onClick={() => ask('reject')}>
                {intl.formatMessage({ id: 'catalogue.types.turnDown' })}
              </Button>
            </>
          )}
        </div>
      )}
      <ReasonDialog
        open={asking === 'approve'}
        title={intl.formatMessage({ id: 'catalogue.types.pickTitle' }, { name: kind.typeKey })}
        description={intl.formatMessage({ id: 'catalogue.types.pickBody' }, { n: kind.productCount })}
        confirmLabel={intl.formatMessage({ id: 'catalogue.types.pick' })}
        label={intl.formatMessage({ id: 'catalogue.review.note' })}
        help={intl.formatMessage({ id: 'catalogue.field.optional' })}
        optional
        pending={decide.isPending}
        error={decide.error ? catalogueProblem(decide.error) : null}
        onConfirm={(reason) => run({ kind: 'approve', ...(reason ? { reason } : {}) })}
        onClose={() => setAsking(null)}
      />
      <ReasonDialog
        open={asking === 'reject'}
        title={intl.formatMessage({ id: approved ? 'catalogue.types.takeDownTitle' : 'catalogue.types.turnDownTitle' }, { name: kind.typeKey })}
        description={intl.formatMessage({ id: approved ? 'catalogue.types.takeDownBody' : 'catalogue.types.turnDownBody' }, { n: kind.showing })}
        confirmLabel={intl.formatMessage({ id: approved ? 'catalogue.types.takeDown' : 'catalogue.types.turnDown' })}
        danger
        pending={decide.isPending}
        error={decide.error ? catalogueProblem(decide.error) : null}
        onConfirm={(reason) => run({ kind: 'reject', reason })}
        onClose={() => setAsking(null)}
      />
    </li>
  );
}
