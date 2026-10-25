import { ApiError } from '@todayz/api-client';
import { newUlid } from '@todayz/auth';
import type { BannerCreate } from '@todayz/api-client/gen/model/bannerCreate';
import type { BannerTargetType } from '@todayz/api-client/gen/model/bannerTargetType';
import { BannerTemplateTint } from '@todayz/api-client/gen/model/bannerTemplateTint';
import { Banner, Button, Chip, Segmented, Sheet, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { BannerCopy } from '../ai-drafts/BannerCopy';
import type { CurrentStore } from '../orders/store';
import { useCategories } from '../stock/queries';
import { checkSchedule, type PickedItem } from './form';
import { ItemPicker } from './ItemPicker';
import { BANNER_LIMITS } from './limits';
import { addBanner, offersErrorKey, useOffers } from './queries';
import styles from './offers.module.css';

const TINTS = Object.values(BannerTemplateTint);

/** B-08 "New banner" with a template (tint, headline, sub-line). Uploaded images are held for review by the server (D-155). */
export function BannerSheet({ store, onClose }: { store: CurrentStore; onClose: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const [headline, setHeadline] = useState('');
  const [subline, setSubline] = useState('');
  const [tint, setTint] = useState<BannerTemplateTint>('green');
  const [target, setTarget] = useState<BannerTargetType>('store');
  const [offerId, setOfferId] = useState('');
  const [categoryId, setCategoryId] = useState('');
  const [items, setItems] = useState<PickedItem[]>([]);
  const [startsAt, setStartsAt] = useState('');
  const [endsAt, setEndsAt] = useState('');
  const [showErrors, setShowErrors] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>(null);
  const key = useRef<{ sig: string; id: string } | null>(null);
  const offers = useOffers(store);
  const categories = useCategories();

  const [openedAt] = useState(() => Date.now());
  const sched = checkSchedule(startsAt, endsAt, openedAt);
  const errs = {
    headline: headline.trim() === '' ? 'headlineRequired' : headline.trim().length > BANNER_LIMITS.headlineMax ? 'headlineMax' : null,
    subline: subline.trim().length > BANNER_LIMITS.sublineMax ? 'sublineMax' : null,
    target:
      target === 'offer' && !offerId ? 'offerRequired'
      : target === 'category' && !categoryId ? 'categoryRequired'
      : target === 'items' && items.length === 0 ? 'itemsRequired'
      : null,
  };
  const bad = Object.values(errs).some(Boolean) || Object.keys(sched.errors).length > 0;
  const say = (code: string | null, values?: Record<string, number>) => (showErrors && code ? intl.formatMessage({ id: `offers.banner.err.${code}` }, values) : undefined);

  const body = (): BannerCreate => ({
    headline: headline.trim(),
    template: { tint, ...(subline.trim() ? { subline: subline.trim() } : {}) },
    targetType: target,
    ...(target === 'offer' ? { targetId: offerId } : target === 'category' ? { targetId: categoryId } : {}),
    ...(target === 'items' ? { targetItemIds: items.map((i) => i.id) } : {}),
    ...(sched.startsAt ? { startsAt: sched.startsAt } : {}),
    ...(sched.endsAt ? { endsAt: sched.endsAt } : {}),
  });

  const submit = async () => {
    setShowErrors(true);
    if (bad || Object.keys(checkSchedule(startsAt, endsAt, Date.now()).errors).length > 0) return;
    const b = body();
    const sig = JSON.stringify(b);
    key.current = key.current?.sig === sig ? key.current : { sig, id: newUlid() };
    setBusy(true);
    setFailure(null);
    try {
      await addBanner(qc, store, b, key.current.id);
      toast.show({ message: intl.formatMessage({ id: 'offers.banner.created' }) });
      onClose();
    } catch (err) {
      // Any answer from the server retires the key; a network failure keeps it so a retry cannot double-post.
      if (err instanceof ApiError) key.current = null;
      setFailure(err);
    } finally {
      setBusy(false);
    }
  };

  const copyFor = target === 'offer' && offerId ? { offerId } : target === 'items' && items.length > 0 ? { itemIds: items.map((i) => i.id) } : null;
  const liveOffers = (offers.data?.pages.flatMap((p) => p.items) ?? []).filter((o) => o.status === 'live' || o.status === 'scheduled');
  const flat = (categories.data?.items ?? []).flatMap((c) => [{ id: c.id, name: c.name }, ...c.children.map((k) => ({ id: k.id, name: `${c.name} · ${k.name}` }))]);

  return (
    <Sheet open onClose={onClose} title={intl.formatMessage({ id: 'offers.banner.new' })} closeLabel={intl.formatMessage({ id: 'offers.close' })}>
      <form
        className={styles.form}
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <TextField label={intl.formatMessage({ id: 'offers.banner.headline' })} help={intl.formatMessage({ id: 'offers.banner.headlineHelp' }, { max: BANNER_LIMITS.headlineMax })} error={say(errs.headline, { max: BANNER_LIMITS.headlineMax })} value={headline} onChange={(e) => setHeadline(e.target.value)} />
        <TextField label={intl.formatMessage({ id: 'offers.banner.subline' })} error={say(errs.subline, { max: BANNER_LIMITS.sublineMax })} value={subline} onChange={(e) => setSubline(e.target.value)} />
        <div className={styles.chips} role="group" aria-label={intl.formatMessage({ id: 'offers.banner.tint' })}>
          {TINTS.map((t) => (
            <Chip key={t} pressed={tint === t} onPressedChange={() => setTint(t)}>
              {intl.formatMessage({ id: `offers.banner.tint.${t}` })}
            </Chip>
          ))}
        </div>

        <div className={styles.bannerPreview} data-tint={tint} aria-label={intl.formatMessage({ id: 'offers.preview.label' })} role="img">
          <strong>{headline.trim() || intl.formatMessage({ id: 'offers.banner.sampleHeadline' })}</strong>
          {subline.trim() && <span>{subline.trim()}</span>}
        </div>

        <Segmented
          label={intl.formatMessage({ id: 'offers.banner.target' })}
          value={target}
          onChange={setTarget}
          options={(['store', 'offer', 'category', 'items'] as const).map((v) => ({ value: v, label: intl.formatMessage({ id: `offers.banner.target.${v}` }) }))}
        />
        {target === 'offer' && (
          <div className="tz-field">
            <label className="tz-field-label" htmlFor="banner-offer">{intl.formatMessage({ id: 'offers.banner.pickOffer' })}</label>
            <select id="banner-offer" className="tz-input" value={offerId} onChange={(e) => setOfferId(e.target.value)}>
              <option value="">{intl.formatMessage({ id: 'offers.banner.chooseOffer' })}</option>
              {liveOffers.map((o) => (
                <option key={o.id} value={o.id}>{o.title}</option>
              ))}
            </select>
          </div>
        )}
        {target === 'category' && (
          <div className="tz-field">
            <label className="tz-field-label" htmlFor="banner-category">{intl.formatMessage({ id: 'offers.category.label' })}</label>
            <select id="banner-category" className="tz-input" value={categoryId} onChange={(e) => setCategoryId(e.target.value)}>
              <option value="">{intl.formatMessage({ id: 'offers.category.choose' })}</option>
              {flat.map((c) => (
                <option key={c.id} value={c.id}>{c.name}</option>
              ))}
            </select>
          </div>
        )}
        {target === 'items' && <ItemPicker store={store} picked={items} max={BANNER_LIMITS.maxItems} onChange={setItems} />}
        {errs.target && showErrors && <p className="tz-field-error">{say(errs.target)}</p>}
        {/* W3-11 AI-R9: wording for the picked offer or items; "Use this" only fills the fields above. */}
        <BannerCopy
          key={JSON.stringify(copyFor)}
          store={store}
          request={copyFor}
          onUse={(c) => {
            setHeadline(c.headline);
            setSubline(c.subline);
            setTint(c.tint);
          }}
        />

        <div className={styles.pair}>
          <TextField label={intl.formatMessage({ id: 'offers.starts.label' })} help={intl.formatMessage({ id: 'offers.starts.help' })} error={showErrors && sched.errors.startsAt ? intl.formatMessage({ id: `offers.err.${sched.errors.startsAt.code}` }) : undefined} type="datetime-local" value={startsAt} onChange={(e) => setStartsAt(e.target.value)} />
          <TextField label={intl.formatMessage({ id: 'offers.ends.label' })} help={intl.formatMessage({ id: 'offers.ends.help' })} error={showErrors && sched.errors.endsAt ? intl.formatMessage({ id: `offers.err.${sched.errors.endsAt.code}` }) : undefined} type="datetime-local" value={endsAt} onChange={(e) => setEndsAt(e.target.value)} />
        </div>
        <p className={styles.note}>{intl.formatMessage({ id: 'offers.banner.uploadLater' })}</p>

        {failure !== null && (
          <Banner tone="warning">
            <span role="alert">{intl.formatMessage({ id: `offers.error.${offersErrorKey(failure)}` })}</span>
          </Banner>
        )}
        <div className={styles.actions}>
          <Button onClick={onClose}>{intl.formatMessage({ id: 'offers.cancel' })}</Button>
          <Button variant="primary" type="submit" loading={busy}>{intl.formatMessage({ id: 'offers.banner.create' })}</Button>
        </div>
      </form>
    </Sheet>
  );
}
