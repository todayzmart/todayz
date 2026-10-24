import { ApiError } from '@todayz/api-client';
import { getMe } from '@todayz/api-client/gen/me/me';
import { SessionPending, useSession, useSessionState } from '@todayz/auth';
import { NATIVE_NAMES, isLocale } from '@todayz/i18n';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, createFileRoute } from '@tanstack/react-router';
import { Button, Skeleton, TextField } from '@todayz/ui';
import { useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { patchMe } from '../app/me';
import { usePlace } from '../app/place';
import styles from '../shell/shell.module.css';

export const Route = createFileRoute('/account')({
  component: Account,
});

/** C-xx Account: sign in when signed out; name, language, location and sign out when signed in. */
function Account() {
  const intl = useIntl();
  const { status } = useSessionState();

  if (status === 'signedOut') {
    return (
      <div className={styles.body}>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'account.title' })}</h1>
        <section className={`tz-card ${styles.stack}`} aria-labelledby="acct-out">
          <h2 id="acct-out" className="tz-card-title">
            {intl.formatMessage({ id: 'account.signedOut.title' })}
          </h2>
          <p className="tz-card-desc">{intl.formatMessage({ id: 'account.signedOut.body' })}</p>
          <Link to="/sign-in" search={{ next: '/account' }} className="tz-btn tz-btn-primary tz-btn-block">
            {intl.formatMessage({ id: 'account.signIn' })}
          </Link>
        </section>
      </div>
    );
  }
  if (status === 'unknown') {
    return (
      <SessionPending
        className={styles.body}
        loadingLabel={intl.formatMessage({ id: 'account.loading' })}
        offlineTitle={intl.formatMessage({ id: 'app.offline.title' })}
        offlineMessage={intl.formatMessage({ id: 'app.offline.body' })}
        retryLabel={intl.formatMessage({ id: 'app.pending.retry' })}
      />
    );
  }
  return <SignedIn />;
}

type Note = 'saved' | 'conflict' | 'error' | null;

function SignedIn() {
  const intl = useIntl();
  // Kept here, not in Profile: a 412 reloads the account, which remounts Profile with the fresh name.
  const [note, setNote] = useState<Note>(null);
  const query = useQuery({ queryKey: ['me'], queryFn: () => getMe() });

  return (
    <div className={styles.body}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'account.title' })}</h1>
      {query.isPending && (
        <div role="status" aria-label={intl.formatMessage({ id: 'account.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {query.isError && (
        <div role="alert" className={styles.stack}>
          <p>{intl.formatMessage({ id: 'account.loadError' })}</p>
          <Button onClick={() => void query.refetch()}>{intl.formatMessage({ id: 'account.retry' })}</Button>
        </div>
      )}
      {query.data && <Profile key={query.dataUpdatedAt} me={query.data} note={note} setNote={setNote} />}
    </div>
  );
}

type Me = Awaited<ReturnType<typeof getMe>>;

function Profile({ me, note, setNote }: { me: Me; note: Note; setNote: (n: Note) => void }) {
  const intl = useIntl();
  const session = useSession();
  const client = useQueryClient();
  const place = usePlace();
  const [name, setName] = useState(me.name ?? '');
  const [saving, setSaving] = useState(false);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    setSaving(true);
    setNote(null);
    try {
      const next = await patchMe(session, { name: trimmed === '' ? null : trimmed });
      client.setQueryData(['me'], next);
      setNote('saved');
    } catch (err) {
      // 412: the account changed elsewhere. Reload it (which also refreshes the ETag) and ask again.
      if (err instanceof ApiError && (err.status === 412 || err.status === 428)) {
        setNote('conflict');
        void client.invalidateQueries({ queryKey: ['me'] });
      } else {
        setNote('error');
      }
    } finally {
      setSaving(false);
    }
  };

  const signOut = async () => {
    await session.signOut();
    // Nothing cached for this person may outlive the session: orders, bookings and addresses too.
    client.clear();
  };

  const place_ =
    place?.kind === 'pin'
      ? intl.formatMessage({ id: 'location.pin.saved' }, { pin: place.pin })
      : place?.kind === 'gps'
        ? intl.formatMessage({ id: 'location.gps.label' })
        : intl.formatMessage({ id: 'location.set' });
  const lang = isLocale(me.preferredLang) ? NATIVE_NAMES[me.preferredLang] : me.preferredLang;

  return (
    <>
      <form className={`tz-card ${styles.stack}`} onSubmit={(e) => void save(e)}>
        <TextField
          label={intl.formatMessage({ id: 'account.name.label' })}
          value={name}
          maxLength={80}
          autoComplete="name"
          onChange={(e) => {
            setName(e.target.value);
            setNote(null);
          }}
          error={
            note === 'conflict'
              ? intl.formatMessage({ id: 'account.name.conflict' })
              : note === 'error'
                ? intl.formatMessage({ id: 'account.name.error' })
                : undefined
          }
        />
        <Button type="submit" variant="primary" loading={saving}>
          {intl.formatMessage({ id: 'account.name.save' })}
        </Button>
        {note === 'saved' && <p role="status">{intl.formatMessage({ id: 'account.name.saved' })}</p>}
      </form>

      <dl className={`tz-card ${styles.stack}`}>
        <div>
          <dt>{intl.formatMessage({ id: 'account.phone' })}</dt>
          <dd>{me.phoneMasked}</dd>
        </div>
        <div>
          <dt>{intl.formatMessage({ id: 'account.language' })}</dt>
          <dd>
            {lang} <Link to="/language">{intl.formatMessage({ id: 'account.change' })}</Link>
          </dd>
        </div>
        <div>
          <dt>{intl.formatMessage({ id: 'account.location' })}</dt>
          <dd>
            {place_} <Link to="/location">{intl.formatMessage({ id: 'account.change' })}</Link>
          </dd>
        </div>
        <div>
          <dt>{intl.formatMessage({ id: 'basket.entry' })}</dt>
          <dd>
            <Link to="/baskets">{intl.formatMessage({ id: 'basket.entry.body' })}</Link>
          </dd>
        </div>
      </dl>

      <Button variant="soft-danger" block onClick={() => void signOut()}>
        {intl.formatMessage({ id: 'account.signOut' })}
      </Button>
    </>
  );
}
