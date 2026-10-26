// C-18 profile photo (D-259): the upload and remove calls and their state; the pieces that show it
// are in ProfilePhoto.tsx.
import { ApiError } from '@todayz/api-client';
import { confirmMyPhotoUpload, createMyPhotoUpload, deleteMyPhoto, type getMe } from '@todayz/api-client/gen/me/me';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

export type Me = Awaited<ReturnType<typeof getMe>>;

/** What the contract accepts (ProfilePhotoCreate). */
export const PHOTO_TYPES = ['image/jpeg', 'image/png', 'image/webp'] as const;
type PhotoType = (typeof PHOTO_TYPES)[number];
export const PHOTO_MAX_BYTES = 5 * 1024 * 1024;
/** Longest side sent: the server keeps 512 px, so more is only upload time. */
const SEND_SIDE = 1024;

type Busy = 'upload' | 'remove' | null;
export const ERROR_IDS = {
  type: 'account.photo.error.type',
  size: 'account.photo.error.size',
  unreadable: 'account.photo.error.unreadable',
  rateLimited: 'account.photo.error.rateLimited',
  generic: 'account.photo.error.generic',
} as const;
type PhotoError = keyof typeof ERROR_IDS;

/**
 * Shrinks the picked photo to a JPEG of at most SEND_SIDE px in the browser (which also drops its
 * metadata before it leaves the phone). Returns the file as it is when the browser cannot decode it
 * here; the server still checks and re-encodes it.
 */
export async function shrinkPhoto(file: File): Promise<Blob> {
  if (typeof createImageBitmap !== 'function') return file;
  let bmp: ImageBitmap;
  try {
    bmp = await createImageBitmap(file, { imageOrientation: 'from-image' });
  } catch {
    return file;
  }
  try {
    const scale = Math.min(1, SEND_SIDE / Math.max(bmp.width, bmp.height));
    const w = Math.max(1, Math.round(bmp.width * scale));
    const h = Math.max(1, Math.round(bmp.height * scale));
    const canvas = document.createElement('canvas');
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext('2d');
    if (!ctx) return file;
    ctx.fillStyle = 'white'; // transparent PNG areas become white, as on the server
    ctx.fillRect(0, 0, w, h);
    ctx.drawImage(bmp, 0, 0, w, h);
    const blob = await new Promise<Blob | null>((done) => canvas.toBlob(done, 'image/jpeg', 0.85));
    return blob && blob.size < file.size ? blob : file;
  } finally {
    bmp.close();
  }
}

/** Register, PUT straight to storage, confirm. Returns the account with the new photo. */
export async function uploadPhoto(body: Blob): Promise<Me> {
  const made = await createMyPhotoUpload({ contentType: body.type as PhotoType, sizeBytes: body.size });
  const res = await fetch(made.upload.url, {
    method: made.upload.method,
    headers: { 'Content-Type': made.upload.headers['Content-Type'] },
    body,
  });
  if (!res.ok) throw new Error(`upload ${res.status}`);
  return confirmMyPhotoUpload(made.id);
}

function errorOf(err: unknown): PhotoError {
  if (err instanceof ApiError) {
    if (err.code === 'rate_limited') return 'rateLimited';
    if (err.code === 'validation_failed') return 'unreadable';
  }
  return 'generic';
}

/** State and actions shared by the avatar and the actions row, so each can sit where the card wants it. */
export function useProfilePhoto() {
  const client = useQueryClient();
  const [busy, setBusy] = useState<Busy>(null);
  const [error, setError] = useState<PhotoError | null>(null);

  // The photo routes answer with the account but not its ETag for /v1/me: reload it, so the
  // next name change sends the current version instead of hitting 412.
  const done = async () => client.invalidateQueries({ queryKey: ['me'] });

  const pick = async (file: File) => {
    setError(null);
    if (!(PHOTO_TYPES as readonly string[]).includes(file.type)) {
      setError('type');
      return;
    }
    setBusy('upload');
    try {
      const body = await shrinkPhoto(file);
      if (body.size > PHOTO_MAX_BYTES) {
        setError('size');
        return;
      }
      client.setQueryData(['me'], await uploadPhoto(body));
      await done();
    } catch (err) {
      setError(errorOf(err));
    } finally {
      setBusy(null);
    }
  };

  const remove = async () => {
    setError(null);
    setBusy('remove');
    try {
      client.setQueryData(['me'], await deleteMyPhoto());
      await done();
    } catch (err) {
      setError(errorOf(err));
    } finally {
      setBusy(null);
    }
  };

  return { busy, error, pick, remove };
}

export type PhotoState = ReturnType<typeof useProfilePhoto>;
