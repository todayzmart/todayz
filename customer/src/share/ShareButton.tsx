// C-08 Share (D-101): the item's share card `https://app.todayz.in/x/{code}` with the text
// "{item} at {shop}, ₹{price} · TodayZ". On Android a TodayZ sheet puts WhatsApp first, then More (the
// system sheet) and Copy link, because a web app cannot reorder the system sheet; elsewhere the system
// share sheet, or Copy link where there is none. Works signed out.
import type { ShareCard } from '@todayz/api-client/gen/model/shareCard';
import { createShareCard } from '@todayz/api-client/gen/share/share';
import { anonId } from '@todayz/telemetry';
import { Button, Sheet, useToast } from '@todayz/ui';
import { Copy, MessageCircle, Share2 } from 'lucide-react';
import { useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { isAndroid, whatsappHref } from './links';
import styles from './share.module.css';

function canNativeShare(): boolean {
  return typeof navigator !== 'undefined' && typeof navigator.share === 'function';
}

export function ShareButton({ storeItemId }: { storeItemId: string }) {
  const intl = useIntl();
  const toast = useToast();
  const [card, setCard] = useState<ShareCard | null>(null);
  const [sheet, setSheet] = useState(false);
  const [busy, setBusy] = useState(false);
  // One card per store item: ask the server once per page.
  const cached = useRef<{ id: string; card: ShareCard } | null>(null);

  const failed = () => toast.show({ message: intl.formatMessage({ id: 'product.share.failed' }) });

  const copy = (c: ShareCard) => {
    if (!navigator.clipboard) {
      failed();
      return;
    }
    void navigator.clipboard
      .writeText(c.url)
      .then(() => toast.show({ message: intl.formatMessage({ id: 'product.share.copied' }) }))
      .catch(failed);
  };

  /** The system sheet; false when the browser refused (no gesture left after the fetch) or has none. */
  const native = async (c: ShareCard): Promise<boolean> => {
    if (!canNativeShare()) return false;
    try {
      await navigator.share({ text: c.text, url: c.url });
      return true;
    } catch (err) {
      // The customer closing the sheet is not an error.
      return err instanceof DOMException && err.name === 'AbortError';
    }
  };

  const open = async () => {
    setBusy(true);
    let c = cached.current?.id === storeItemId ? cached.current.card : null;
    try {
      c ??= await createShareCard({ storeItemId, anonId: anonId() });
      cached.current = { id: storeItemId, card: c };
    } catch {
      setBusy(false);
      return failed();
    }
    setBusy(false);
    setCard(c);
    if (isAndroid()) return setSheet(true);
    if (await native(c)) return;
    // No system sheet, or it needs a fresh tap: our sheet offers it again with Copy link.
    setSheet(true);
  };

  return (
    <>
      <Button variant="ghost" size="sm" icon={Share2} loading={busy} onClick={() => void open()}>
        {intl.formatMessage({ id: 'share.button' })}
      </Button>
      <Sheet
        open={sheet && card !== null}
        onClose={() => setSheet(false)}
        title={intl.formatMessage({ id: 'share.title' })}
        description={card?.text}
        closeLabel={intl.formatMessage({ id: 'share.close' })}
      >
        {card && (
          <div className={styles.list}>
            {isAndroid() && (
              <a className="tz-btn tz-btn-primary tz-btn-block" href={whatsappHref(card)} target="_blank" rel="noopener noreferrer" onClick={() => setSheet(false)}>
                <MessageCircle size={18} strokeWidth={1.5} aria-hidden />
                {intl.formatMessage({ id: 'share.whatsapp' })}
              </a>
            )}
            {canNativeShare() && (
              <Button
                variant="soft"
                block
                icon={Share2}
                onClick={() => {
                  setSheet(false);
                  void native(card).then((ok) => ok || failed());
                }}
              >
                {intl.formatMessage({ id: 'share.more' })}
              </Button>
            )}
            <Button
              variant="soft"
              block
              icon={Copy}
              onClick={() => {
                setSheet(false);
                copy(card);
              }}
            >
              {intl.formatMessage({ id: 'share.copy' })}
            </Button>
          </div>
        )}
      </Sheet>
    </>
  );
}
