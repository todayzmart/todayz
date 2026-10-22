import { createFileRoute, notFound } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { NavLink } from '../shell/NavLink';
import { SoonCard } from '../shell/Placeholder';
import { MORE, PLACEHOLDER_SECTIONS, navItem } from '../shell/nav';
import styles from '../shell/shell.module.css';

/**
 * Placeholder for every nav section that has no route yet. A WP adds its static route file
 * (`src/routes/<area>/…` or `src/routes/<area>.tsx`), which outranks this param route, and changes
 * nothing in nav.ts. Unknown sections are a 404.
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
  if (section === 'more') {
    return (
      <>
        <h1 className="tz-page-title">{label}</h1>
        <section className="tz-card" aria-label={label}>
          <ul className={styles.list}>
            {MORE.map((m) => (
              <li key={m.section}>
                <NavLink item={m} />
              </li>
            ))}
          </ul>
        </section>
      </>
    );
  }
  return (
    <>
      {section === 'overview' ? (
        // B-03 keeps its mint greeting band until W3-03 builds it (D-84).
        <div className="tz-greet">
          <div>
            <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.greet.title' })}</h1>
            <p className="tz-page-sub">{intl.formatMessage({ id: 'shell.greet.sub' })}</p>
          </div>
        </div>
      ) : (
        <h1 className="tz-page-title">{label}</h1>
      )}
      <SoonCard section={label} />
    </>
  );
}
