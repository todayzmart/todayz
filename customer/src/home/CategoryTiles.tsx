// C-03 categories as a product photo on a constant light-grey square: the top-level ones filter
// the shops on Home (`/?category=`), and each one's subcategories open the category page C-06.
import type { Category } from '@todayz/api-client/gen/model/category';
import { useIntl } from 'react-intl';
import { categoryPhoto } from './photos';
import { RouterLink } from './RouterLink';
import styles from './home.module.css';

export function CategoryTiles({ categories, active }: { categories: readonly Category[]; active: string | undefined }) {
  const intl = useIntl();
  return (
    <ul className={styles.catGrid} aria-label={intl.formatMessage({ id: 'home.categories.label' })}>
      {categories.map((c) => (
        <li key={c.slug}>
          <RouterLink
            href={active === c.slug ? '/' : `/?category=${encodeURIComponent(c.slug)}`}
            className={styles.catTile}
            aria-current={active === c.slug ? 'true' : undefined}
          >
            <span className={styles.catPhoto}>
              {categoryPhoto(c.slug) && <img src={categoryPhoto(c.slug)} alt="" loading="lazy" decoding="async" />}
            </span>
            <span className={styles.catName}>{c.name}</span>
          </RouterLink>
        </li>
      ))}
    </ul>
  );
}

/** One block per top-level category with children: its subcategories as small tiles. */
export function SubcategoryGrids({ categories }: { categories: readonly Category[] }) {
  return (
    <>
      {categories
        .filter((c) => c.children.length > 0)
        .map((c) => (
          <section key={c.slug} className={styles.section} aria-labelledby={`subcats-${c.slug}`}>
            <h2 id={`subcats-${c.slug}`} className={styles.railTitle}>
              {c.name}
            </h2>
            <ul className={styles.subGrid}>
              {c.children.map((k) => (
                <li key={k.slug}>
                  <RouterLink href={`/c/${encodeURIComponent(k.slug)}`} className={styles.subTile}>
                    <span className={styles.subPhoto}>
                      {(categoryPhoto(k.slug) ?? categoryPhoto(c.slug)) && (
                        <img src={categoryPhoto(k.slug) ?? categoryPhoto(c.slug)} alt="" loading="lazy" decoding="async" />
                      )}
                    </span>
                    <span className={styles.subName}>{k.name}</span>
                  </RouterLink>
                </li>
              ))}
            </ul>
          </section>
        ))}
    </>
  );
}
