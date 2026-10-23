import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach, beforeEach, vi } from 'vitest';

// Node 25 ships an experimental global `localStorage` that shadows the DOM one and has no methods
// unless started with --localstorage-file. Tests get plain in-memory Storage objects instead.
function memoryStorage(): Storage {
  const map = new Map<string, string>();
  return {
    get length() {
      return map.size;
    },
    clear: () => map.clear(),
    getItem: (k) => map.get(k) ?? null,
    key: (i) => Array.from(map.keys())[i] ?? null,
    removeItem: (k) => void map.delete(k),
    setItem: (k, v) => void map.set(k, String(v)),
  };
}
for (const name of ['localStorage', 'sessionStorage'] as const) {
  Object.defineProperty(globalThis, name, { value: memoryStorage(), configurable: true, writable: true });
}

// Unit tests never use the network. renderAt hands the API client a fake fetch, but anything on the
// global one (the orders stream, reached when a test lands on /orders) would otherwise try the real
// API URL. A test that needs the global fetch stubs its own in beforeEach, which runs after this one.
beforeEach(() => {
  vi.stubGlobal('fetch', () => Promise.reject(new TypeError('unit tests do not use the network')));
});

afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.unstubAllGlobals();
});
