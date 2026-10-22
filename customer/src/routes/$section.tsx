import { createFileRoute, notFound } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { SoonCard } from '../shell/Placeholder';
import { PLACEHOLDER_SECTIONS, navItem } from '../shell/nav';
import styles from '../shell/shell.module.css';

/**
 * Placeholder for every nav section that has no route yet. A WP adds `src/routes/<area>/…`,
 * which takes precedence over this param route, and removes its section from nav.ts.
 */
export const Route = createFileRoute('/$section')({
  beforeLoad: ({ params }) => {
    if (!PLACEHOLDER_SECTIONS.includes(params.section)) throw notFound();
  },
  component: SectionPlaceholder,
});

function SectionPlaceholder() {
  const intl = useIntl();
  const { section } = Route.useParams();
  const item = navItem(section);
  const label = item ? intl.formatMessage({ id: item.label }) : section;
  return (
    <div className={styles.body}>
      <h1 className="tz-page-title">{label}</h1>
      <SoonCard section={label} />
    </div>
  );
}
