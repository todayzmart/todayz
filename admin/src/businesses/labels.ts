import type { AdminBusinessDocumentKind } from '@todayz/api-client/gen/model/adminBusinessDocumentKind';
import type { AdminBusinessDocumentStatus } from '@todayz/api-client/gen/model/adminBusinessDocumentStatus';
import type { AdminBusinessStaffRole } from '@todayz/api-client/gen/model/adminBusinessStaffRole';
import type { AdminBusinessStaffStatus } from '@todayz/api-client/gen/model/adminBusinessStaffStatus';
import type { AdminBusinessStatus } from '@todayz/api-client/gen/model/adminBusinessStatus';
import type { AdminCheckStatus } from '@todayz/api-client/gen/model/adminCheckStatus';
import type { AdminInfoField } from '@todayz/api-client/gen/model/adminInfoField';
import type { AdminKycField } from '@todayz/api-client/gen/model/adminKycField';
import type { AdminKycStatus } from '@todayz/api-client/gen/model/adminKycStatus';
import type { AdminLeadStatus } from '@todayz/api-client/gen/model/adminLeadStatus';
import type { PillTone } from '@todayz/ui';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';

const STATUS: Record<AdminBusinessStatus, string> = {
  draft: 'businesses.status.draft',
  submitted: 'businesses.status.submitted',
  needs_info: 'businesses.status.needsInfo',
  verified: 'businesses.status.verified',
  rejected: 'businesses.status.rejected',
  suspended: 'businesses.status.suspended',
};

/** D-274: a live shop waits for a field agent or admin to check it. */
const CHECK: Record<AdminCheckStatus, string> = {
  unchecked: 'businesses.check.unchecked',
  checked: 'businesses.check.checked',
  rejected: 'businesses.check.rejected',
};

const KYC: Record<AdminKycStatus, string> = {
  missing: 'businesses.kyc.status.missing',
  submitted: 'businesses.kyc.status.submitted',
  verified: 'businesses.kyc.status.verified',
  rejected: 'businesses.kyc.status.rejected',
};

const KYC_FIELD: Record<AdminKycField, string> = {
  pan: 'businesses.kyc.pan',
  gstin: 'businesses.kyc.gstin',
  upi: 'businesses.kyc.upi',
  bank: 'businesses.kyc.bank',
};

const DOC_KIND: Record<AdminBusinessDocumentKind, string> = {
  pan: 'businesses.doc.pan',
  gst: 'businesses.doc.gst',
  shop_photo: 'businesses.doc.shopPhoto',
  storefront: 'businesses.doc.storefront',
  license: 'businesses.doc.license',
  other: 'businesses.doc.other',
};

const DOC_STATUS: Record<AdminBusinessDocumentStatus, string> = {
  pending: 'businesses.doc.status.pending',
  uploaded: 'businesses.doc.status.uploaded',
  accepted: 'businesses.doc.status.accepted',
  rejected: 'businesses.doc.status.rejected',
};

/** The fields request info can name (AdminInfoField), in the order the form lists them. */
export const INFO_FIELDS = [
  'brandName',
  'legalName',
  'categoryId',
  'pan',
  'gstin',
  'documents',
  'stores',
  'city',
  'pincode',
  'location',
  'hours',
  'phone',
] as const satisfies readonly AdminInfoField[];

const INFO: Record<AdminInfoField, string> = {
  brandName: 'businesses.info.brandName',
  legalName: 'businesses.info.legalName',
  categoryId: 'businesses.info.category',
  pan: 'businesses.info.pan',
  gstin: 'businesses.info.gstin',
  documents: 'businesses.info.documents',
  stores: 'businesses.info.stores',
  city: 'businesses.info.city',
  pincode: 'businesses.info.pincode',
  location: 'businesses.info.location',
  hours: 'businesses.info.hours',
  phone: 'businesses.info.phone',
};

const ROLE: Record<AdminBusinessStaffRole, string> = {
  owner: 'businesses.staff.owner',
  manager: 'businesses.staff.manager',
  cashier: 'businesses.staff.cashier',
};

const STAFF_STATUS: Record<AdminBusinessStaffStatus, string> = {
  invited: 'businesses.staff.status.invited',
  active: 'businesses.staff.status.active',
};

const LEAD: Record<AdminLeadStatus, string> = {
  new: 'businesses.leads.status.new',
  contacted: 'businesses.leads.status.contacted',
  closed: 'businesses.leads.status.closed',
};

export const statusTone = (s: AdminBusinessStatus): PillTone =>
  s === 'verified' ? 'completed' : s === 'submitted' || s === 'needs_info' ? 'pending' : s === 'draft' ? 'neutral' : 'rejected';

export const checkTone = (s: AdminCheckStatus): PillTone => (s === 'checked' ? 'completed' : s === 'rejected' ? 'rejected' : 'pending');

export const kycTone = (s: AdminKycStatus): PillTone => (s === 'verified' ? 'completed' : s === 'submitted' ? 'pending' : s === 'rejected' ? 'rejected' : 'neutral');

export const docTone = (s: AdminBusinessDocumentStatus): PillTone =>
  s === 'accepted' ? 'completed' : s === 'rejected' ? 'rejected' : s === 'uploaded' ? 'pending' : 'neutral';

export const leadTone = (s: AdminLeadStatus): PillTone => (s === 'new' ? 'pending' : s === 'contacted' ? 'accepted' : 'neutral');

export function useBusinessLabels() {
  const intl = useIntl();
  return useMemo(() => {
    const t = (id: string) => intl.formatMessage({ id });
    return {
      status: (s: AdminBusinessStatus) => t(STATUS[s]),
      check: (s: AdminCheckStatus) => t(CHECK[s]),
      kyc: (s: AdminKycStatus) => t(KYC[s]),
      kycField: (f: AdminKycField) => t(KYC_FIELD[f]),
      docKind: (k: AdminBusinessDocumentKind) => t(DOC_KIND[k]),
      docStatus: (s: AdminBusinessDocumentStatus) => t(DOC_STATUS[s]),
      info: (f: AdminInfoField) => t(INFO[f]),
      role: (r: AdminBusinessStaffRole) => t(ROLE[r]),
      staffStatus: (s: AdminBusinessStaffStatus) => t(STAFF_STATUS[s]),
      /** A field the console does not know yet shows as written. */
      infoAny: (f: string) => (f in INFO ? t(INFO[f as AdminInfoField]) : f),
      lead: (s: AdminLeadStatus) => t(LEAD[s]),
    };
  }, [intl]);
}
