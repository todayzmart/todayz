import { useSession } from '@todayz/auth';
import { useQueryClient } from '@tanstack/react-query';
import { clearCachedPosStore } from '../pos/store';

/** Ends this device's session and drops everything cached for the person who was signed in. */
export function useSignOut() {
  const session = useSession();
  const client = useQueryClient();
  return () => {
    // The counter's cached shop and role go first, even when the network call fails (D-179).
    // Unsent sales stay in the outbox: they belong to the shop, not to the person.
    clearCachedPosStore();
    void session.signOut().then(() => client.clear());
  };
}
