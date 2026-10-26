// C-18 profile photo (D-259): the avatar shows the photo, else the initial; the camera badge picks a
// new one (JPEG, PNG or WebP, at most 5 MB after the in-browser shrink) and "Remove photo" takes it
// away. The server re-encodes it without metadata and shows it only to its owner.
import { Button } from '@todayz/ui';
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

/** The account avatar with its camera badge and the hidden file picker. */
export function PhotoAvatar({ me, photo }: { me: Me; photo: PhotoState }) {
  const intl = useIntl();
  const input = useRef<HTMLInputElement>(null);
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
          if (photo.busy === null) input.current?.click();
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
    </span>
  );
}

/** "Remove photo" (only with a photo) and the upload state, spoken politely. */
export function PhotoActions({ me, photo }: { me: Me; photo: PhotoState }) {
  const intl = useIntl();
  const status =
    photo.busy === 'upload'
      ? intl.formatMessage({ id: 'account.photo.uploading' })
      : photo.busy === 'remove'
        ? intl.formatMessage({ id: 'account.photo.removing' })
        : null;
  return (
    <div className={p.actions}>
      {me.photoUrl && (
        <Button size="sm" variant="ghost" icon={Trash2} disabled={photo.busy !== null} onClick={() => void photo.remove()}>
          {intl.formatMessage({ id: 'account.photo.remove' })}
        </Button>
      )}
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
