import type { AdminSettingGroup } from '@todayz/api-client/gen/model/adminSettingGroup';
import { AdminSettingGroup as Groups } from '@todayz/api-client/gen/model/adminSettingGroup';
import { text } from '../app/search';

export const GROUPS = Object.values(Groups) as readonly AdminSettingGroup[];

/** A-17 filters, kept in the URL: one group, and the setting whose changes the side panel shows. */
export interface SettingsSearch {
  group?: AdminSettingGroup;
  key?: string;
}

const KEY = /^[a-z][a-z0-9]*(_[a-z0-9]+)*$/;

export function validateSettingsSearch(raw: Record<string, unknown>): SettingsSearch {
  const out: SettingsSearch = {};
  const group = text(raw.group);
  if ((GROUPS as readonly string[]).includes(group)) out.group = group as AdminSettingGroup;
  const key = text(raw.key);
  if (key.length <= 64 && KEY.test(key)) out.key = key;
  return out;
}
