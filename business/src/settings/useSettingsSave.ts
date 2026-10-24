import type { StoreSettingsUpdate } from '@todayz/api-client/gen/model/storeSettingsUpdate';
import { getStoreSettings, updateStoreSettings } from '@todayz/api-client/gen/business/business';
import { useQueryClient } from '@tanstack/react-query';
import { settingsKey, settingsPath, useAction, useIfMatch } from '../stores/queries';

/**
 * One save of some of a store's settings: PATCH with only the fields that changed and the
 * `If-Match` of the version this device read. Returns true when the server took it.
 */
export function useSettingsSave(bid: string, sid: string) {
  const qc = useQueryClient();
  const action = useAction();
  const ifMatch = useIfMatch();
  const save = async (name: string, body: StoreSettingsUpdate): Promise<boolean> => {
    if (Object.keys(body).length === 0) return true;
    const out = await action.run(`settings-${name}`, async () => {
      const tag = await ifMatch(settingsPath(bid, sid), () => getStoreSettings(bid, sid));
      return updateStoreSettings(bid, sid, body, tag ? { ifMatch: tag } : {});
    });
    if (!out) return false;
    await qc.invalidateQueries({ queryKey: settingsKey(bid, sid) });
    return true;
  };
  return { save, pending: action.pending, error: action.error, clearError: action.clearError };
}
