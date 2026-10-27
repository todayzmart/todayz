import type { AdminShopCheck } from '@todayz/api-client/gen/model/adminShopCheck';
import type { AdminShopCheckPhotoKind } from '@todayz/api-client/gen/model/adminShopCheckPhotoKind';
import { Button, EmptyState, Skeleton, StatusPill, useToast } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { Check, ClipboardCheck, ExternalLink, MapPin, X } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ReasonDialog } from '../app/ReasonDialog';
import { kycTone, useBusinessLabels } from '../businesses/labels';
import { useCheckShop, useRejectShop, useShopChecks } from './queries';
import css from './shopChecks.module.css';

type Open = { kind: 'check' | 'reject'; shop: AdminShopCheck };

const PHOTO: Record<AdminShopCheckPhotoKind, string> = {
  storefront: 'shopChecks.photo.storefront',
  shop_photo: 'shopChecks.photo.inside',
};

/** A map search on the pin; on a phone it opens the maps app for directions. */
const mapUrl = (lat: number, lng: number) => `https://www.google.com/maps/search/?api=1&query=${lat},${lng}`;

/**
 * Shop checks (D-269): every live shop nobody has checked yet, newest first, with its details,
 * stores and photos. A field agent, admin or super_admin marks it checked (payouts may start) or
 * rejects it with a reason (hidden at once, open online orders cancelled). Phone first.
 */
export function ShopChecksPage() {
  const intl = useIntl();
  const toast = useToast();
  const problemText = useProblemText();
  const q = useShopChecks();
  const check = useCheckShop();
  const reject = useRejectShop();
  const [open, setOpen] = useState<Open | null>(null);
  const rows = q.data?.pages.flatMap((p) => p.items) ?? [];
  const now = q.data?.pages[0]?.serverNow ?? '';

  const close = () => {
    setOpen(null);
    check.reset();
    reject.reset();
  };
  const name = open?.shop.brandName ?? '';

  return (
    <div className={css.page}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.shopChecks' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'shopChecks.sub' })}</p>
      </div>
      {q.isPending && <Skeleton shape="block" height="240px" />}
      {q.isError && (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      )}
      {q.isSuccess && rows.length === 0 && (
        <EmptyState icon={ClipboardCheck} title={intl.formatMessage({ id: 'shopChecks.empty.title' })} description={intl.formatMessage({ id: 'shopChecks.empty.body' })} />
      )}
      {rows.length > 0 && (
        <ul className={css.list} aria-label={intl.formatMessage({ id: 'shopChecks.list' })}>
          {rows.map((s) => (
            <li key={s.id}>
              <ShopCard shop={s} now={now} onOpen={(kind) => setOpen({ kind, shop: s })} />
            </li>
          ))}
        </ul>
      )}
      {q.hasNextPage && (
        <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
          {intl.formatMessage({ id: 'common.loadMore' })}
        </Button>
      )}
      <ReasonDialog
        open={open?.kind === 'check'}
        title={intl.formatMessage({ id: 'shopChecks.check.title' }, { name })}
        description={intl.formatMessage({ id: 'shopChecks.check.body' })}
        confirmLabel={intl.formatMessage({ id: 'shopChecks.check' })}
        label={intl.formatMessage({ id: 'shopChecks.check.note' })}
        help={intl.formatMessage({ id: 'shopChecks.check.noteHelp' })}
        optional
        pending={check.isPending}
        error={check.isError ? problemText(check.error) : null}
        onConfirm={(note) => {
          if (!open) return;
          check.mutate(
            { id: open.shop.id, note },
            {
              onSuccess: () => {
                close();
                toast.show({ message: intl.formatMessage({ id: 'shopChecks.done.check' }, { name }) });
              },
            },
          );
        }}
        onClose={close}
      />
      <ReasonDialog
        open={open?.kind === 'reject'}
        danger
        title={intl.formatMessage({ id: 'shopChecks.reject.title' }, { name })}
        description={intl.formatMessage({ id: 'shopChecks.reject.body' })}
        confirmLabel={intl.formatMessage({ id: 'shopChecks.reject' })}
        help={intl.formatMessage({ id: 'shopChecks.reject.help' })}
        pending={reject.isPending}
        error={reject.isError ? problemText(reject.error) : null}
        onConfirm={(reason) => {
          if (!open) return;
          reject.mutate(
            { id: open.shop.id, reason },
            {
              onSuccess: (r) => {
                close();
                toast.show({ message: intl.formatMessage({ id: 'shopChecks.done.reject' }, { name, orders: r.cancelledOrders }) });
              },
            },
          );
        }}
        onClose={close}
      />
    </div>
  );
}

function ShopCard({ shop: s, now, onOpen }: { shop: AdminShopCheck; now: string; onOpen: (kind: 'check' | 'reject') => void }) {
  const intl = useIntl();
  const when = useWhen();
  const roles = useRoles();
  const labels = useBusinessLabels();
  const t = (id: string, v?: Record<string, string | number>) => intl.formatMessage({ id }, v);
  const meta = [
    s.category ?? t('shopChecks.noCategory'),
    s.liveAt ? t('shopChecks.liveAt', { when: when(s.liveAt, now) }) : null,
    t(s.byFieldAgent ? 'shopChecks.byAgent' : 'shopChecks.bySelf'),
  ].filter(Boolean);
  return (
    <article className={css.card} aria-labelledby={`shop-${s.id}`}>
      <div className={css.top}>
        <div className={css.names}>
          <h2 id={`shop-${s.id}`} className={css.name}>
            {s.brandName}
          </h2>
          {s.legalName && <span className={css.muted}>{s.legalName}</span>}
        </div>
        <StatusPill tone={kycTone(s.kycStatus)}>{t('shopChecks.kyc', { status: labels.kyc(s.kycStatus) })}</StatusPill>
      </div>
      <p className={css.muted}>{meta.join(' · ')}</p>

      <section aria-label={t('shopChecks.stores')}>
        {s.stores.length === 0 ? (
          <p className={css.muted}>{t('shopChecks.noStores')}</p>
        ) : (
          <ul className={css.stores}>
            {s.stores.map((st) => (
              <li key={st.id} className={css.store}>
                <span className={css.storeName}>{st.name}</span>
                <span className={css.muted}>{[st.addressLine1, st.locality, [st.city, st.pincode].filter(Boolean).join(' ')].filter(Boolean).join(', ')}</span>
                {st.lat !== null && st.lng !== null ? (
                  <a className={css.map} href={mapUrl(st.lat, st.lng)} target="_blank" rel="noreferrer">
                    <MapPin size={16} aria-hidden="true" />
                    {t('shopChecks.map', { store: st.name })}
                  </a>
                ) : (
                  <span className={css.muted}>{t('shopChecks.noPin')}</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section aria-label={t('shopChecks.photos')}>
        {s.photos.length === 0 ? (
          <p className={css.muted}>{t('shopChecks.noPhotos')}</p>
        ) : (
          <ul className={css.photos}>
            {s.photos.map((p, i) => {
              const alt = t('shopChecks.photoAlt', { kind: t(PHOTO[p.kind]), n: i + 1 });
              return (
                <li key={p.id}>
                  {p.previewUrl ? (
                    <a href={p.previewUrl} target="_blank" rel="noreferrer" className={css.photo}>
                      <img src={p.previewUrl} alt={alt} loading="lazy" />
                    </a>
                  ) : (
                    <span className={css.photo} role="img" aria-label={alt} />
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>

      <div className={css.actions}>
        <Button variant="primary" icon={Check} onClick={() => onOpen('check')}>
          {t('shopChecks.check')}
        </Button>
        <Button variant="soft-danger" icon={X} onClick={() => onOpen('reject')}>
          {t('shopChecks.reject')}
        </Button>
        {can(roles, 'businessView') && (
          <Link className={css.more} to="/businesses/$businessId" params={{ businessId: s.id }}>
            {t('shopChecks.open')}
            <ExternalLink size={14} aria-hidden="true" />
          </Link>
        )}
      </div>
    </article>
  );
}
