import { ApiError } from '@todayz/api-client';
import type { FieldBusiness } from '@todayz/api-client/gen/model/fieldBusiness';
import type { FieldDocumentKind } from '@todayz/api-client/gen/model/fieldDocumentKind';
import type { FieldPhotoContentType } from '@todayz/api-client/gen/model/fieldPhotoContentType';
import { Button, useToast } from '@todayz/ui';
import { Camera } from 'lucide-react';
import { useRef, useState, type ChangeEvent } from 'react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';
import { PHOTO_TYPES, UploadError, useAddPhoto } from './queries';
import css from './field.module.css';

/** The contract's upload ceiling (FieldDocumentCreate.sizeBytes). */
const MAX_BYTES = 10 * 1024 * 1024;

/** Step 3: photos of the shop and its front, straight from the phone camera. */
export function PhotosStep({ business: b, onDone }: { business: FieldBusiness; onDone: () => void }) {
  const intl = useIntl();
  const toast = useToast();
  const problemText = useProblemText();
  const add = useAddPhoto(b.id);
  const [error, setError] = useState<string | null>(null);
  const [kind, setKind] = useState<FieldDocumentKind>('storefront');
  const input = useRef<HTMLInputElement>(null);
  const t = (id: string) => intl.formatMessage({ id });

  const pick = (k: FieldDocumentKind) => {
    setKind(k);
    setError(null);
    input.current?.click();
  };

  const chosen = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    if (!PHOTO_TYPES.includes(file.type as FieldPhotoContentType)) return setError(t('field.photos.type'));
    if (file.size <= 0 || file.size > MAX_BYTES) return setError(t('field.photos.size'));
    add.mutate(
      { file, kind, storeId: b.stores[0]?.id },
      {
        onSuccess: () => toast.show({ message: t('field.photos.added') }),
        onError: (err) =>
          setError(
            err instanceof UploadError
              ? t('field.photos.uploadFailed')
              : err instanceof ApiError && err.code === 'conflict'
                ? t('field.photos.tooMany')
                : problemText(err),
          ),
      },
    );
  };

  return (
    <div className={css.form}>
      <p className={css.muted}>{intl.formatMessage({ id: 'field.photos.count' }, { n: b.photoCount })}</p>
      {b.itemsEditable ? (
        <>
          <input
            ref={input}
            className={css.fileInput}
            type="file"
            accept={PHOTO_TYPES.join(',')}
            capture="environment"
            tabIndex={-1}
            aria-hidden="true"
            onChange={chosen}
            data-testid="field-photo-input"
          />
          <div className={css.photos}>
            <Button icon={Camera} variant="soft" loading={add.isPending && kind === 'storefront'} onClick={() => pick('storefront')}>
              {t('field.photos.storefront')}
            </Button>
            <Button icon={Camera} variant="soft" loading={add.isPending && kind === 'shop_photo'} onClick={() => pick('shop_photo')}>
              {t('field.photos.inside')}
            </Button>
          </div>
          {error && (
            <p role="alert" className="tz-field-error">
              {error}
            </p>
          )}
          <Button variant="primary" block onClick={onDone}>
            {t(b.photoCount > 0 ? 'field.next' : 'field.photos.skip')}
          </Button>
        </>
      ) : (
        <p className={css.muted}>{t('field.readOnly')}</p>
      )}
    </div>
  );
}
