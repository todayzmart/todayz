// C-03 header (D-249): where you are (the locality from the reverse lookup, else the PIN code), the
// wallet's credits and the account avatar. The page title stays for screen readers only.
import { reversePlace } from '@todayz/api-client/gen/discover/discover';
import { getMe } from '@todayz/api-client/gen/me/me';
import { getMyWallet } from '@todayz/api-client/gen/wallet/wallet';
import { useSessionState } from '@todayz/auth';
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ChevronDown, Coins, MapPin, User } from 'lucide-react';
import { useIntl } from 'react-intl';
import { usePlace } from '../app/place';
import { Icon } from '../shell/Icon';
import type { CoordsState } from './coords';
import styles from './home.module.css';
import { RouterLink } from './RouterLink';

export function Header({ coords }: { coords: CoordsState }) {
  const intl = useIntl();
  const place = usePlace();
  const { status } = useSessionState();
  const signedIn = status === 'signedIn';
  const point = coords.status === 'ready' ? coords.coords : null;

  const reverse = useQuery({
    queryKey: ['discover', 'reverse', point?.lat, point?.lng],
    enabled: point !== null,
    staleTime: 24 * 60 * 60_000,
    retry: false,
    queryFn: () => reversePlace({ lat: point?.lat ?? 0, lng: point?.lng ?? 0 }),
  });
  const me = useQuery({ queryKey: ['me'], queryFn: () => getMe(), enabled: signedIn });
  const wallet = useQuery({ queryKey: ['wallet'], queryFn: () => getMyWallet(), enabled: signedIn });

  const name =
    reverse.data?.name ??
    (place?.kind === 'pin'
      ? place.pin
      : place?.kind === 'gps'
        ? intl.formatMessage({ id: 'location.gps.label' })
        : intl.formatMessage({ id: 'location.set' }));
  const sub = reverse.data?.label;
  const initial = [...(me.data?.name?.trim() ?? '')][0]?.toUpperCase();

  return (
    <header className={styles.hero}>
      <h1 className="tz-ui-sr-only">{intl.formatMessage({ id: 'shell.hero.title' })}</h1>
      <div className={styles.topRow}>
        <Link to="/location" className={styles.place}>
          <Icon icon={MapPin} />
          <span className={styles.placeText}>
            <b className={styles.placeName}>
              <span className="tz-ui-sr-only">{intl.formatMessage({ id: 'home.place.current' })} </span>
              {name}
              <Icon icon={ChevronDown} />
            </b>
            {sub && <small className={styles.placeSub}>{sub}</small>}
            <span className="tz-ui-sr-only">{intl.formatMessage({ id: 'home.place.change' })}</span>
          </span>
        </Link>
        {signedIn && wallet.data && (
          <RouterLink
            href="/wallet"
            className={styles.wallet}
            aria-label={intl.formatMessage({ id: 'home.wallet.label' }, { count: wallet.data.availableCredits })}
          >
            <Icon icon={Coins} />
            <span className="tz-num">{wallet.data.availableCredits.toLocaleString('en-IN')}</span>
          </RouterLink>
        )}
        <Link to="/account" className={styles.avatar} aria-label={intl.formatMessage({ id: 'home.account.label' })}>
          {signedIn && initial ? <span aria-hidden="true">{initial}</span> : <Icon icon={User} />}
        </Link>
      </div>
    </header>
  );
}
