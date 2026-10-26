// C-18 profile photo (D-259): the avatar shows the photo, else the initial. Without a photo the camera
// badge opens the picker (JPEG, PNG or WebP, at most 5 MB after the in-browser shrink); with one it
// opens a sheet to choose a new photo or remove it. The server re-encodes it without metadata and
// shows it only to its owner.
import { Button, Sheet } from '@todayz/ui';
import { Camera, LoaderCircle, Trash2, UserRound } from 'lucide-react';
import { useRef, useState, type ChangeEvent, type ReactNode } from 'react';
import { useIntl } from 'react-intl';
import { Icon } from '../shell/Icon';
import a from './account.module.css';
import { ERROR_IDS, PHOTO_TYPES, type Me, type PhotoState } from './photo';
import p from './photo.module.css';

/** The photo, falling back to `fallback` when it has none or the signed link no longer loads. */
export function PhotoImage({ url, fallback, className }: { url: string | null | undefined; fallback: ReactNode; className?: string | undefined }) {
  const [broken, setBroken] = useState<string | null>(null);
  if (!url || broken === url) return <>{fallback}</>;
  return <img src={url} alt="" className={className ?? p.img} onError={() => setBroken(url)} />;
}

/** The account avatar with its camera badge, the hidden file picker and, with a photo, its sheet. */
export function PhotoAvatar({ me, photo }: { me: Me; photo: PhotoState }) {
  const intl = useIntl();
  const input = useRef<HTMLInputElement>(null);
  const [sheet, setSheet] = useState(false);
  const initial = [...(me.name?.trim() ?? '')][0]?.toUpperCase();
  const has = Boolean(me.photoUrl);

  const chosen = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ''; // picking the same file again still fires change
    if (file) void photo.pick(file);
  };

  return (
    <span className={p.box}>
      <span className={`${a.avatar} ${p.avatar}`} data-tint="sky" aria-hidden="true">
        <PhotoImage url={me.photoUrl} fallback={initial || <Icon icon={UserRound} />} />
        {photo.busy && (
          <span className={p.busy}>
            <Icon icon={LoaderCircle} />
          </span>
        )}
      </span>
      <button
        type="button"
        className={p.badge}
        aria-label={intl.formatMessage({ id: has ? 'account.photo.change' : 'account.photo.add' })}
        aria-disabled={photo.busy !== null}
        onClick={() => {
          if (photo.busy !== null) return;
          if (has) setSheet(true);
          else input.current?.click();
        }}
      >
        <Icon icon={Camera} />
      </button>
      <input
        ref={input}
        type="file"
        accept={PHOTO_TYPES.join(',')}
        hidden
        data-testid="photo-input"
        onChange={chosen}
      />
      <Sheet
        open={sheet}
        onClose={() => setSheet(false)}
        title={intl.formatMessage({ id: 'account.photo.sheet.title' })}
        closeLabel={intl.formatMessage({ id: 'account.photo.sheet.close' })}
      >
        <div className={p.sheet}>
          <Button
            variant="soft"
            block
            icon={Camera}
            onClick={() => {
              setSheet(false);
              input.current?.click();
            }}
          >
            {intl.formatMessage({ id: 'account.photo.choose' })}
          </Button>
          <Button
            variant="soft-danger"
            block
            icon={Trash2}
            onClick={() => {
              setSheet(false);
              void photo.remove();
            }}
          >
            {intl.formatMessage({ id: 'account.photo.remove' })}
          </Button>
        </div>
      </Sheet>
    </span>
  );
}

/** The upload or remove state, spoken politely, and any error. */
export function PhotoActions({ photo }: { photo: PhotoState }) {
  const intl = useIntl();
  const status =
    photo.busy === 'upload'
      ? intl.formatMessage({ id: 'account.photo.uploading' })
      : photo.busy === 'remove'
        ? intl.formatMessage({ id: 'account.photo.removing' })
        : null;
  return (
    <div className={p.actions}>
      <p role="status" className={p.status}>
        {status}
      </p>
      {photo.error && (
        <p role="alert" className={p.error}>
          {intl.formatMessage({ id: ERROR_IDS[photo.error] })}
        </p>
      )}
    </div>
  );
}
