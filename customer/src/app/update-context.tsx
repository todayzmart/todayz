import type { UpdateController } from '@todayz/auth';
import { createContext, useContext } from 'react';

export const UpdateContext = createContext<UpdateController | null>(null);

/** C-11 checkout calls `useHoldUpdates(useUpdateController(), open)` so the update toast waits (G9, D-98). */
export function useUpdateController(): UpdateController {
  const v = useContext(UpdateContext);
  if (!v) throw new Error('useUpdateController must be used inside <App>');
  return v;
}
