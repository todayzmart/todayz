import { CoachPreview } from '../coach/CoachPreview';
import { canCoach } from '../coach/permissions';
import { useCurrentStore } from '../orders/store';

/**
 * Mount point for the shop coach (B-19, W3-11): the 1–3 newest live cards, for the owner and
 * manager. It renders nothing when there is no card, so the layout does not reserve space for it.
 */
export function CoachSlot() {
  const current = useCurrentStore();
  if (current.status !== 'ready' || !canCoach(current.store.role)) return null;
  return <CoachPreview store={current.store} />;
}
