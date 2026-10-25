import type { StaffRole } from '@todayz/api-client/gen/model/staffRole';

/**
 * B-20 role limits (06-screens B-20, D-101). Every staff role sees and answers asks (`asks.view`,
 * `asks.reply`). The API is the guard; the UI hides what a role cannot do.
 */

/** Changing the prefilled price, or pricing an item the shop does not list: `item.price.edit`, owner and manager. */
export const canEditAskPrice = (role: StaffRole): boolean => role === 'owner' || role === 'manager';

/** "Add it to your stock" opens the B-07 add form: `item.manage`, owner and manager. */
export const canAddItem = (role: StaffRole): boolean => role === 'owner' || role === 'manager';

/** The muted note links to B-14 and the settings are read: `asks.manage`, owner and manager. */
export const canSeeAskSettings = (role: StaffRole): boolean => role === 'owner' || role === 'manager';
