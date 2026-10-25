import { useIntl } from 'react-intl';
import { changes } from './diff';
import css from './audit.module.css';

/** The before/after diff of one audit row. */
export function ChangeList({ before, after }: { before: Record<string, unknown> | null; after: Record<string, unknown> | null }) {
  const intl = useIntl();
  const rows = changes(before, after);
  if (rows.length === 0) return <p className={css.meta}>{intl.formatMessage({ id: 'audit.noChange' })}</p>;
  const none = intl.formatMessage({ id: 'audit.removed' });
  return (
    <dl className={css.changes}>
      {rows.map((c) => (
        <div key={c.key} className={css.pair}>
          <dt>{c.key}</dt>
          <dd>
            {c.before !== null && (
              <>
                <span className={css.was} aria-label={intl.formatMessage({ id: 'audit.was' }, { value: c.before })}>
                  {c.before}
                </span>{' '}
                →{' '}
              </>
            )}
            {c.after ?? none}
          </dd>
        </div>
      ))}
    </dl>
  );
}
