// Hero photos for the bright landing (D-262; Unsplash licence, assets/landing/CREDITS.md). Each comes as
// a portrait crop for phones (the hero is taller than wide there, so a 3:2 photo would be blown up to
// cover it), landscape AVIF for wider screens, and WebP for browsers without AVIF.
import type { ImageMetadata } from 'astro';
import kiranaPhone750 from '../../assets/landing/hero-kirana-phone-750.avif';
import kiranaPhone1170 from '../../assets/landing/hero-kirana-phone-1170.avif';
import kirana1200 from '../../assets/landing/hero-kirana-1200.avif';
import kirana1920 from '../../assets/landing/hero-kirana-1920.avif';
import kirana2560 from '../../assets/landing/hero-kirana-2560.avif';
import kiranaWebp1200 from '../../assets/landing/hero-kirana-1200.webp';
import kiranaWebp1920 from '../../assets/landing/hero-kirana-1920.webp';
import shopkeeperPhone750 from '../../assets/landing/hero-shopkeeper-phone-750.avif';
import shopkeeperPhone1170 from '../../assets/landing/hero-shopkeeper-phone-1170.avif';
import shopkeeper1200 from '../../assets/landing/hero-shopkeeper-1200.avif';
import shopkeeper1920 from '../../assets/landing/hero-shopkeeper-1920.avif';
import shopkeeper2560 from '../../assets/landing/hero-shopkeeper-2560.avif';
import shopkeeperWebp1200 from '../../assets/landing/hero-shopkeeper-1200.webp';
import shopkeeperWebp1920 from '../../assets/landing/hero-shopkeeper-1920.webp';

export interface HeroPhoto {
  /** Portrait AVIF, used up to PHONE_MAX. */
  phone: ImageMetadata[];
  /** Landscape AVIF. */
  wide: ImageMetadata[];
  /** Landscape WebP; the last one is the img's src and sets its size. */
  fallback: ImageMetadata[];
}

/** Widest viewport that gets the portrait crop. */
export const PHONE_MAX = '(max-width: 700px)';

export const srcset = (images: ImageMetadata[]) => images.map((i) => `${i.src} ${i.width}w`).join(', ');

export const kiranaPhoto: HeroPhoto = {
  phone: [kiranaPhone750, kiranaPhone1170],
  wide: [kirana1200, kirana1920, kirana2560],
  fallback: [kiranaWebp1200, kiranaWebp1920],
};

export const shopkeeperPhoto: HeroPhoto = {
  phone: [shopkeeperPhone750, shopkeeperPhone1170],
  wide: [shopkeeper1200, shopkeeper1920, shopkeeper2560],
  fallback: [shopkeeperWebp1200, shopkeeperWebp1920],
};
