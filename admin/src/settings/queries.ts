import { listAdminSettingHistory, listAdminSettings, updateAdminSetting } from '@todayz/api-client/gen/admin-settings/admin-settings';
import type { AdminSetting } from '@todayz/api-client/gen/model/adminSetting';
import type { AdminSettingList } from '@todayz/api-client/gen/model/adminSettingList';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { PAGE, refreshAudit } from '../audit/queries';
import type { SettingValue } from './value';

export const settingsKey = ['admin', 'settings'] as const;
export const settingHistoryKey = (key: string) => ['admin', 'settings', 'history', key] as const;

/** Every setting (at most a few hundred, one answer) with the provider key states. */
export function useSettings() {
  return useQuery({ queryKey: settingsKey, queryFn: () => listAdminSettings() });
}

/** One setting's changes, newest first, paged on the server's cursor. */
export function useSettingHistory(key: string) {
  return useInfiniteQuery({
    queryKey: settingHistoryKey(key),
    queryFn: ({ pageParam }) => listAdminSettingHistory(key, { limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export interface SettingChange {
  key: string;
  value: SettingValue;
  reason: string;
}

/** Changes one setting with a reason; the list shows the answer, the history and audit refresh. */
export function useUpdateSetting() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ key, value, reason }: SettingChange): Promise<AdminSetting> =>
      updateAdminSetting(key, { value, reason }).then((d) => d.setting),
    onSuccess: (s) =>
      qc.setQueryData<AdminSettingList>(settingsKey, (list) =>
        list ? { ...list, items: list.items.map((i) => (i.key === s.key ? s : i)) } : list,
      ),
    onSettled: (_d, _e, { key }) => {
      void qc.invalidateQueries({ queryKey: settingHistoryKey(key) });
      refreshAudit(qc, 'platform_setting', key);
    },
  });
}
