// `/x/$code` in the app: the Pages Function sends link previews and most visitors straight to C-08;
// this route covers the rest (an installed app's service worker, the function falling through). A
// shop that is no longer live (410) shows "This shop isn't on TodayZ right now" with a search box.
import type { SharePreview } from '@todayz/api-client/gen/model/sharePreview';
import { getShareCard } from '@todayz/api-client/gen/share/share';
import { ApiError } from '@todayz/api-client';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from '@tanstack/react-router';
import { Button, EmptyState, Skeleton, TextField } from '@todayz/ui';
import { SearchX, Store } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useIntl } from 'react-intl';
import { itemPath } from '../../functions/x/[code]';
import styles from './share.module.css';

export function SharedLink({ code }: { code: string }) {
  const intl = useIntl();
  const router = useRouter();
  const q = useQuery<SharePreview, Error>({ queryKey: ['share', code], queryFn: () => getShareCard(code), retry: false });

  useEffect(() => {
    if (q.data) router.history.replace(itemPath(q.data));
  }, [q.data, router]);

  const gone = q.error instanceof ApiError && q.error.status === 410;
  return (
    <div className={styles.page}>
      {(q.isPending || q.data) && <Skeleton shape="block" height="160px" />}
      {q.isError &&
        (gone ? (
          <>
            <EmptyState icon={Store} title={intl.formatMessage({ id: 'share.gone' })} description={intl.formatMessage({ id: 'share.gone.body' })} />
            <SearchBox />
          </>
        ) : (
          <EmptyState
            icon={SearchX}
            title={intl.formatMessage({ id: q.error instanceof ApiError && q.error.status === 404 ? 'share.notFound' : 'share.error' })}
            action={
              <Button variant="soft" onClick={() => router.history.push('/')}>
                {intl.formatMessage({ id: 'share.home' })}
              </Button>
            }
          />
        ))}
    </div>
  );
}

function SearchBox() {
  const intl = useIntl();
  const router = useRouter();
  const [text, setText] = useState('');
  return (
    <form
      role="search"
      className={styles.search}
      onSubmit={(e) => {
        e.preventDefault();
        const t = text.trim();
        if (t) router.history.push(`/search?q=${encodeURIComponent(t)}`);
      }}
    >
      <TextField label={intl.formatMessage({ id: 'share.search' })} value={text} onChange={(e) => setText(e.target.value)} enterKeyHint="search" type="search" />
      <Button type="submit" variant="primary">
        {intl.formatMessage({ id: 'share.searchGo' })}
      </Button>
    </form>
  );
}
