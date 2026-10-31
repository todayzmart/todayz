import type { IconName } from '../Icon.astro';

const icons: Readonly<Record<string, IconName>> = {
  milk: 'milk',
  chocolate: 'candy',
  paracetamol: 'pill',
  'usb c cable': 'cable',
  'brown bread': 'bread',
  earphones: 'headphones',
  'fabric conditioner': 'drops',
  noodles: 'soup',
};

export const productIcon = (query: string): IconName => icons[query] ?? 'package';
