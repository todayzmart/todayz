// Example data for the bright landing (D-262, from design/trials/site-home-bright). Shop and product
// names stay English in every language; every figure here is an example, and the page says so.
// Photos are CC BY 2.0 and credited on the page (PhotoCredits.astro, assets/landing/CREDITS.md).
import type { ImageMetadata } from 'astro';
import bread from '../../assets/landing/p-bread.jpg';
import cable from '../../assets/landing/p-cable.jpg';
import choc from '../../assets/landing/p-choc.jpg';
import earphones from '../../assets/landing/p-earphones.jpg';
import milk from '../../assets/landing/p-milk.jpg';
import noodles from '../../assets/landing/p-noodles.jpg';
import wash from '../../assets/landing/p-wash.jpg';
import pharmacy from '../../assets/landing/shop-pharmacy.jpg';

export type Category = 'grocery' | 'pharmacy' | 'electronics';
export const CATEGORIES: readonly Category[] = ['grocery', 'pharmacy', 'electronics'];

/** Walking speed the page assumes (home.walk.note, home.save.note): about 5 km an hour. */
export const WALK_M_PER_MIN = 5000 / 60;
export const walkMinutes = (metres: number): number => Math.max(1, Math.ceil(metres / WALK_M_PER_MIN));
export const formatDistance = (metres: number): string => (metres < 1000 ? `${metres} m` : `${(metres / 1000).toFixed(1)} km`);

export interface Shop {
  name: string;
  category: Category;
  /** Metres from "you". */
  distance: number;
  /** Bearing on the radar, degrees clockwise from north. */
  angle: number;
  /** The shop's own cashback, 0 to 20% (01-product-rules.md); many shops give none. */
  cashback: number;
  /** Minutes since the shop last confirmed its stock. */
  confirmed: number;
  item: string;
  price: number;
}

export const SHOPS: readonly Shop[] = [
  { name: 'Sharma General Store', category: 'grocery', distance: 600, angle: -40, cashback: 2, confirmed: 12, item: 'Amul Taaza Milk 1 L', price: 68 },
  { name: 'Green Kirana Market', category: 'grocery', distance: 420, angle: 30, cashback: 0, confirmed: 25, item: 'Maggi Masala 4-pack', price: 56 },
  { name: 'Sri Sai Medicals', category: 'pharmacy', distance: 380, angle: 115, cashback: 1, confirmed: 8, item: 'Dolo 650, 15 tablets', price: 32 },
  { name: 'Vijay Electronics', category: 'electronics', distance: 900, angle: 200, cashback: 5, confirmed: 40, item: 'USB-C cable 1 m', price: 349 },
  { name: 'Lakshmi Medicals', category: 'pharmacy', distance: 1200, angle: 160, cashback: 0, confirmed: 18, item: 'Dolo 650, 15 tablets', price: 32 },
  { name: 'Gupta Stores', category: 'grocery', distance: 1100, angle: -105, cashback: 3, confirmed: 30, item: 'Britannia Brown Bread', price: 45 },
  { name: 'Fresh Basket', category: 'grocery', distance: 1600, angle: -155, cashback: 0, confirmed: 55, item: 'Cadbury Dairy Milk 50 g', price: 40 },
  { name: 'Mobile Point', category: 'electronics', distance: 1800, angle: 75, cashback: 2, confirmed: 20, item: 'boAt wired earphones', price: 499 },
];

export interface ShelfItem {
  name: string;
  /** Search words sent to the customer app's /search. */
  query: string;
  category: Category;
  shop: string;
  distance: number;
  price: number;
  confirmed: number;
  photo: ImageMetadata;
}

export const SHELF: readonly ShelfItem[] = [
  { name: 'Amul Taaza Milk 1 L', query: 'milk', category: 'grocery', shop: 'Sharma General Store', distance: 600, price: 68, confirmed: 12, photo: milk },
  { name: 'Cadbury Dairy Milk 50 g', query: 'chocolate', category: 'grocery', shop: 'Green Kirana Market', distance: 420, price: 40, confirmed: 25, photo: choc },
  { name: 'Dolo 650, 15 tablets', query: 'paracetamol', category: 'pharmacy', shop: 'Sri Sai Medicals', distance: 380, price: 32, confirmed: 8, photo: pharmacy },
  { name: 'USB-C cable 1 m', query: 'usb c cable', category: 'electronics', shop: 'Vijay Electronics', distance: 900, price: 349, confirmed: 40, photo: cable },
  { name: 'Britannia Brown Bread', query: 'brown bread', category: 'grocery', shop: 'Gupta Stores', distance: 1100, price: 45, confirmed: 30, photo: bread },
  { name: 'boAt wired earphones', query: 'earphones', category: 'electronics', shop: 'Mobile Point', distance: 1800, price: 499, confirmed: 20, photo: earphones },
  { name: 'Comfort fabric conditioner 860 ml', query: 'fabric conditioner', category: 'grocery', shop: 'Sharma General Store', distance: 600, price: 245, confirmed: 12, photo: wash },
  { name: 'Maggi Masala 4-pack', query: 'noodles', category: 'grocery', shop: 'Green Kirana Market', distance: 420, price: 56, confirmed: 25, photo: noodles },
];

/** The search strip (home.strip.1-10): search words and the tint of each chip's dot. */
export const STRIP: readonly { query: string; tint: 'green' | 'amber' | 'rose' | 'sky' | 'lilac' | 'peach' }[] = [
  { query: 'milk', tint: 'sky' },
  { query: 'phone charger', tint: 'lilac' },
  { query: 'paracetamol', tint: 'rose' },
  { query: 'notebook', tint: 'amber' },
  { query: 'brown bread', tint: 'peach' },
  { query: 'led bulb', tint: 'amber' },
  { query: 'face wash', tint: 'rose' },
  { query: 'earphones', tint: 'sky' },
  { query: 'noodles', tint: 'green' },
  { query: 'batteries', tint: 'lilac' },
];

/** Kilometres walked each day of the example week (home.week.*): 4.9 km in all, most on Saturday. */
export const WEEK_KM: readonly number[] = [0.5, 0.8, 0.4, 0.9, 0.6, 1.2, 0.5];

/** The savings calculator's sliders and their starting values (home.save.*). */
export const SAVINGS = {
  orders: { min: 2, max: 30, step: 1, value: 8 },
  fee: { min: 0, max: 80, step: 5, value: 35 },
  basket: { min: 100, max: 2000, step: 50, value: 400 },
  cashback: { min: 0, max: 20, step: 0.5, value: 2 },
  walk: { min: 0.4, max: 3, step: 0.1, value: 1 },
} as const;

export interface SavingsResult {
  fees: number;
  credits: number;
  total: number;
  km: number;
  hours: number;
}

/** A year of moving `orders` a month from delivery to pickup. Credits: 1 credit = ₹1. */
export function savings(orders: number, fee: number, basket: number, cashback: number, walk: number): SavingsResult {
  const year = orders * 12;
  const fees = year * fee;
  const credits = Math.round((year * basket * cashback) / 100);
  const km = year * walk;
  return { fees, credits, total: fees + credits, km, hours: km / 5 };
}

export const inr = (n: number): string => Math.round(n).toLocaleString('en-IN');

/** Photo credits (CC BY 2.0 requires them): title, creator and source of each photo the page shows. */
export const PHOTO_CREDITS: readonly { title: string; creator: string; source: string }[] = [
  { title: 'Milk Bottle', creator: 'NickPiggott', source: 'flickr.com/photos/93656595@N00/513371114' },
  { title: 'chocolate bar Ikea', creator: 'molinolin', source: 'flickr.com/photos/27938098@N05/3612036798' },
  { title: 'Pharmacy, Gangtok, India', creator: 'flowcomm', source: 'flickr.com/photos/21162417@N07/8083933798' },
  { title: 'USB Cable', creator: 'wwarby', source: 'flickr.com/photos/26782864@N00/11728892063' },
  { title: 'Bread Loaf', creator: 'Teuobk', source: 'flickr.com/photos/13261823@N05/2104039823' },
  { title: 'DIY: Shure Sound Isolating Earphone + iPhone 4 Earphone', creator: 'Yutaka Tsutano', source: 'flickr.com/photos/23208360@N07/4737693376' },
  { title: 'Fabric softener/conditioner at dm-drogeriemarkt in Berlin, Germany', creator: 'qubodup', source: 'flickr.com/photos/21051491@N02/7980493529' },
  { title: 'Instant noodles with added stuff', creator: 'Annie Mole', source: 'flickr.com/photos/21309047@N00/2416809777' },
];
