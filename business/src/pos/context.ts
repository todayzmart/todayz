// The shape the Sell screen reads from the runtime, and the hook to read it. Kept apart from the
// provider so the provider file only exports a component.
import { createContext, useContext } from 'react';
import type { SyncEngine, SyncState } from '../sync/engine';
import type { PosApi } from './api';
import type { CatalogueItem, PosSettings, SaleRecord } from './types';

export interface PosStoreRef {
  bid: string;
  sid: string;
  name: string;
  role: 'owner' | 'manager' | 'cashier' | string;
}

export interface PosRuntime {
  store: PosStoreRef;
  engine: SyncEngine;
  api: PosApi;
  /** False when IndexedDB is blocked: sales are kept in memory only and the screen says so. */
  durable: boolean;
  sync: SyncState;
  online: boolean;
  items: CatalogueItem[];
  catalogueLoaded: boolean;
  settings: PosSettings;
  records: SaleRecord[];
  reloadRecords: () => Promise<void>;
  now: () => number;
}

export const Ctx = createContext<PosRuntime | null>(null);

export function usePos(): PosRuntime {
  const v = useContext(Ctx);
  if (!v) throw new Error('usePos must be used inside <PosProvider>');
  return v;
}
