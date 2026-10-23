// The app-update toast (G9, D-98, SC-GS-08). A new service worker is waiting: the toast offers
// "Update"; the page reloads only when the person taps it, never by itself, and the toast stays
// quiet while a sale (B-05) or a checkout (C-11) is open. The service worker precaches every chunk
// of its own version (SC-GS-09), so the running page keeps working until the tap.
import { useToast } from '@todayz/ui';
import { useEffect, useRef, useSyncExternalStore } from 'react';

export interface UpdateState {
  ready: boolean;
  holds: number;
}

export interface UpdateController {
  getState(): UpdateState;
  subscribe(l: () => void): () => void;
  /** vite-plugin-pwa onNeedRefresh: a new version is waiting. */
  notifyReady(): void;
  /** vite-plugin-pwa's updateSW: skip waiting and reload. */
  setUpdater(fn: (reloadPage?: boolean) => Promise<void>): void;
  /** Hold the toast back (a sale or checkout is open); returns the release function. */
  hold(): () => void;
  /** Only the Update tap calls this. */
  apply(): Promise<void>;
}

export function createUpdateController(): UpdateController {
  let state: UpdateState = { ready: false, holds: 0 };
  let updater: ((reloadPage?: boolean) => Promise<void>) | null = null;
  const listeners = new Set<() => void>();
  const set = (next: Partial<UpdateState>) => {
    state = { ...state, ...next };
    listeners.forEach((l) => l());
  };
  return {
    getState: () => state,
    subscribe(l) {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    notifyReady: () => set({ ready: true }),
    setUpdater(fn) {
      updater = fn;
    },
    hold() {
      set({ holds: state.holds + 1 });
      let released = false;
      return () => {
        if (released) return;
        released = true;
        set({ holds: Math.max(0, state.holds - 1) });
      };
    },
    async apply() {
      await updater?.(true);
    },
  };
}

export interface UpdateLabels {
  message: string;
  action: string;
}

/** Renders nothing; shows the persistent toast when an update is ready and nothing holds it back. */
export function UpdateToastHost({ controller, labels }: { controller: UpdateController; labels: UpdateLabels }) {
  const toast = useToast();
  const state = useSyncExternalStore(controller.subscribe, controller.getState, controller.getState);
  const shown = useRef<number | null>(null);
  const show = state.ready && state.holds === 0;
  const { message, action } = labels;
  useEffect(() => {
    if (show && shown.current === null) {
      shown.current = toast.show({
        message,
        duration: null,
        action: { label: action, onClick: () => void controller.apply() },
      });
    } else if (!show && shown.current !== null) {
      toast.dismiss(shown.current);
      shown.current = null;
    }
  }, [show, toast, controller, message, action]);
  return null;
}

/** B-05 and C-11 call this while a sale or checkout is open: `useHoldUpdates(controller, open)`. */
export function useHoldUpdates(controller: UpdateController, active: boolean): void {
  useEffect(() => {
    if (!active) return undefined;
    return controller.hold();
  }, [controller, active]);
}
