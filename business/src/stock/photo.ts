// A photo of a product the store created (D-252): shrunk in the browser, registered, PUT to the
// presigned URL with only Content-Type, then confirmed. TodayZ reviews it before customers see it.
import { confirmProductPhoto, createProductPhoto } from '@todayz/api-client/gen/catalogue/catalogue';
import type { ProductPhoto } from '@todayz/api-client/gen/model/productPhoto';
import { ProductPhotoContentType } from '@todayz/api-client/gen/model/productPhotoContentType';
import type { CurrentStore } from '../orders/store';

/** The longest side of a shrunk photo, in pixels. */
export const PHOTO_SIDE = 1024;
/** The server's limit (ProductPhotoCreate.sizeBytes). */
export const PHOTO_MAX_BYTES = 5 * 1024 * 1024;

const TYPES: readonly string[] = Object.values(ProductPhotoContentType);

export class PhotoError extends Error {
  constructor(readonly reason: 'type' | 'size' | 'upload') {
    super(`photo ${reason}`);
  }
}

/**
 * The photo as a JPEG at most PHOTO_SIDE on its longest side. A browser that cannot draw it
 * (no createImageBitmap, an odd format) keeps the file as it is when the server takes it.
 */
export async function shrinkPhoto(file: Blob): Promise<Blob> {
  const shrunk = await draw(file).catch(() => null);
  const out = shrunk ?? file;
  if (!TYPES.includes(out.type)) throw new PhotoError('type');
  if (out.size > PHOTO_MAX_BYTES) throw new PhotoError('size');
  return out;
}

async function draw(file: Blob): Promise<Blob | null> {
  if (typeof createImageBitmap !== 'function') return null;
  const bmp = await createImageBitmap(file);
  try {
    const scale = Math.min(1, PHOTO_SIDE / Math.max(bmp.width, bmp.height));
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(bmp.width * scale));
    canvas.height = Math.max(1, Math.round(bmp.height * scale));
    const ctx = canvas.getContext('2d');
    if (!ctx) return null;
    ctx.fillStyle = 'white'; // a transparent PNG gets a white page, not black
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(bmp, 0, 0, canvas.width, canvas.height);
    return await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/jpeg', 0.82));
  } finally {
    bmp.close();
  }
}

/** Registers, uploads and confirms the photo; the product's photo then waits for review. */
export async function uploadProductPhoto(store: CurrentStore, productId: string, photo: Blob): Promise<ProductPhoto> {
  const made = await createProductPhoto(store.bid, store.sid, productId, {
    contentType: photo.type as ProductPhotoContentType,
    sizeBytes: photo.size,
  });
  const res = await fetch(made.upload.url, {
    method: made.upload.method,
    headers: { 'Content-Type': made.upload.headers['Content-Type'] },
    body: photo,
  });
  if (!res.ok) throw new PhotoError('upload');
  return confirmProductPhoto(store.bid, store.sid, productId, made.photo.id);
}
