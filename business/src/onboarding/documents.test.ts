import { describe, expect, it } from 'vitest';
import { checkFile, MAX_DOCUMENT_BYTES } from './documents';

describe('checkFile', () => {
  it('takes the four types up to 10 MB', () => {
    for (const type of ['image/jpeg', 'image/png', 'image/webp', 'application/pdf']) expect(checkFile({ type, size: 1000 })).toBeNull();
    expect(checkFile({ type: 'image/png', size: MAX_DOCUMENT_BYTES })).toBeNull();
  });
  it('refuses other types, empty files and big files', () => {
    expect(checkFile({ type: 'image/gif', size: 1000 })).toBe('type');
    expect(checkFile({ type: 'application/zip', size: 1000 })).toBe('type');
    expect(checkFile({ type: 'image/png', size: 0 })).toBe('empty');
    expect(checkFile({ type: 'image/png', size: MAX_DOCUMENT_BYTES + 1 })).toBe('size');
  });
});
