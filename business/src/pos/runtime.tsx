// The POS runtime for one store on this device: storage, the sync engine, the loaded catalogue and
// the sales history, shared with every part of the Sell screen. Sync runs by itself: at start, when
// the network comes back, when a backoff ends, and every 30 seconds while sales are waiting.
import { getDeviceId, newUlid, useOnline } from '@todayz/auth';
import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { createSyncEngine, type SyncEngine, type SyncState } from '../sync/engine';
import { openStorage } from '../sync/idb';
import type { PosStorage } from '../sync/storage';
import { toRecords } from './history';
import { httpPosApi, type PosApi } from './api';
import type { CatalogueItem, PosSettings, SaleRecord } from './types';

/** Used until the first snapshot arrives; the server's numbers replace them (platform_settings, 01 §17). */
export const DEFAULT_SETTINGS: PosSettings = { creditMinOrderValuePaise: 10000, reversalWindowHours: 24, offlineLinkMaxHours: 12 };
// TODO(lead): the defaults above mirror 01 §17 for the first run offline; confirm the snapshot carries `settings`.

export const AUTO_SYNC_MS = 30_000;

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

const Ctx = createContext<PosRuntime | null>(null);

export function usePos(): PosRuntime {
  const v = useContext(Ctx);
  if (!v) throw new Error('usePos must be used inside <PosProvider>');
  return v;
}

export interface PosProviderProps {
  store: PosStoreRef;
  children: ReactNode;
  /** Shown while storage opens. */
  fallback?: ReactNode;
  /** Tests give a fake server and storage. */
  api?: PosApi;
  storage?: PosStorage;
  now?: () => number;
}

export function PosProvider({ store, children, fallback = null, api: apiIn, storage: storageIn, now = Date.now }: PosProviderProps) {
  const online = useOnline();
  const api = useMemo(() => apiIn ?? httpPosApi(store.bid, store.sid), [apiIn, store.bid, store.sid]);
  const [opened, setOpened] = useState<{ engine: SyncEngine; durable: boolean; sid: string } | null>(null);
  const [sync, setSync] = useState<SyncState | null>(null);
  const [items, setItems] = useState<CatalogueItem[]>([]);
  const [settings, setSettings] = useState<PosSettings>(DEFAULT_SETTINGS);
  const [catalogueLoaded, setCatalogueLoaded] = useState(false);
  const [records, setRecords] = useState<SaleRecord[]>([]);
  const onlineRef = useRef(online);

  useEffect(() => {
    onlineRef.current = online;
  }, [online]);

  // Open storage and build the engine for this store.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const { storage, durable } = storageIn ? { storage: storageIn, durable: true } : await openStorage(store.sid);
      if (cancelled) return;
      const engine = createSyncEngine({
        storage,
        api,
        deviceId: getDeviceId('business'),
        newId: () => newUlid(),
        now,
        isOnline: () => onlineRef.current,
      });
      setSync(engine.state());
      setOpened({ engine, durable, sid: store.sid });
    })();
    return () => {
      cancelled = true;
      setOpened(null);
      setSync(null);
      setItems([]);
      setCatalogueLoaded(false);
      setRecords([]);
    };
  }, [api, storageIn, store.sid, now]);

  const engine = opened?.engine ?? null;

  const reloadRecords = useMemo(
    () => async () => {
      if (engine) setRecords(toRecords(await engine.entries()));
    },
    [engine],
  );

  useEffect(() => {
    if (!engine) return;
    const off = engine.subscribe((s) => {
      setSync(s);
      void reloadRecords();
    });
    void reloadRecords();
    return off;
  }, [engine, reloadRecords]);

  // Reload the catalogue from storage whenever a snapshot or delta lands.
  const version = sync?.catalogueVersion ?? 0;
  useEffect(() => {
    if (!engine) return;
    let cancelled = false;
    void engine.storage.loadCatalogue().then(({ items: next, meta }) => {
      if (cancelled) return;
      setItems(next);
      if (meta) setSettings(meta.settings);
      setCatalogueLoaded(true);
    });
    return () => {
      cancelled = true;
    };
  }, [engine, version]);

  // Automatic sync.
  useEffect(() => {
    if (!engine) return;
    void engine.flush();
    const id = setInterval(() => void engine.flush(), AUTO_SYNC_MS);
    return () => clearInterval(id);
  }, [engine]);
  useEffect(() => {
    if (engine && online) void engine.flush();
  }, [engine, online]);
  const nextAttemptAt = sync?.nextAttemptAt ?? null;
  useEffect(() => {
    if (!engine || nextAttemptAt === null) return;
    const id = setTimeout(() => void engine.flush(), Math.max(0, nextAttemptAt - now()) + 50);
    return () => clearTimeout(id);
  }, [engine, nextAttemptAt, now]);

  const value = useMemo<PosRuntime | null>(
    () =>
      opened && sync
        ? { store, engine: opened.engine, api, durable: opened.durable, sync, online, items, catalogueLoaded, settings, records, reloadRecords, now }
        : null,
    [opened, sync, store, api, online, items, catalogueLoaded, settings, records, reloadRecords, now],
  );

  if (!value) return <>{fallback}</>;
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
