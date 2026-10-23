import { createContext, useContext, useEffect, useSyncExternalStore, type ReactNode } from 'react';
import type { Session, SessionState } from './session';

const Ctx = createContext<Session | null>(null);

/** Provides the session and starts it (tries the refresh cookie once on mount). */
export function SessionProvider({ session, children }: { session: Session; children: ReactNode }) {
  useEffect(() => {
    void session.start();
  }, [session]);
  return <Ctx.Provider value={session}>{children}</Ctx.Provider>;
}

export function useSession(): Session {
  const s = useContext(Ctx);
  if (!s) throw new Error('useSession must be used inside <SessionProvider>');
  return s;
}

export function useSessionState(): SessionState {
  const s = useSession();
  return useSyncExternalStore(s.subscribe, s.getState, s.getState);
}
