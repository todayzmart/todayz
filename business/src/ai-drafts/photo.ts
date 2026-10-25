/** The browser check before an upload, against the store's `photoTypes` and `maxPhotoBytes`. */
export function checkPhoto(file: File, types: readonly string[], maxBytes: number): 'type' | 'size' | null {
  if (!types.includes(file.type)) return 'type';
  if (file.size > maxBytes) return 'size';
  return null;
}
