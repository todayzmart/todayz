import type { Plugin } from 'vite';

export declare const iconsDir: string;
export declare function tokenValue(name: string): string;
export declare function iconFiles(): string[];
export declare function iconHeadTags(): {
  tag: 'link' | 'meta';
  attrs: Record<string, string>;
  injectTo: 'head';
}[];
export declare function todayzIcons(): Plugin;

export interface PwaManifestOptions {
  name: string;
  shortName: string;
  description: string;
  startUrl?: string;
  scope?: string;
  display?: 'standalone' | 'fullscreen' | 'minimal-ui' | 'browser';
  orientation?: 'any' | 'portrait' | 'landscape';
}

export interface PwaManifest {
  name: string;
  short_name: string;
  description: string;
  lang: string;
  dir: 'ltr';
  start_url: string;
  scope: string;
  display: 'standalone' | 'fullscreen' | 'minimal-ui' | 'browser';
  orientation?: 'any' | 'portrait' | 'landscape';
  background_color: string;
  theme_color: string;
  icons: { src: string; sizes: string; type: string; purpose: 'any' | 'maskable' }[];
}

export declare function pwaManifest(options: PwaManifestOptions): PwaManifest;
