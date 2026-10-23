import type { UpdateController } from '@todayz/auth';
import { createContext, useContext } from 'react';

export const UpdateContext = createContext<UpdateController | null>(null);

/** Admin has no sale or checkout; a screen with unsaved work calls useHoldUpdates(useUpdateController(), dirty) so the update toast waits. */
export function useUpdateController(): UpdateController {
  const v = useContext(UpdateContext);
  if (!v) throw new Error('useUpdateController must be used inside <App>');
  return v;
}
