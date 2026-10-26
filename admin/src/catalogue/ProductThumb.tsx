import { initials } from './image';
import css from './images.module.css';

/** A product's picture, or its initials while it has none; decorative beside the name. */
export function ProductThumb({ name, src, large = false, alt }: { name: string; src: string | undefined; large?: boolean; alt?: string }) {
  return (
    <span className={large ? `${css.thumb} ${css.thumbLarge}` : css.thumb} aria-hidden={alt ? undefined : 'true'}>
      {src ? <img src={src} alt={alt ?? ''} loading="lazy" decoding="async" /> : initials(name)}
    </span>
  );
}
