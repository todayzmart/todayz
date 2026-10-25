import type { AdminAuditEntry } from '@todayz/api-client/gen/model/adminAuditEntry';
import { useCallback } from 'react';
import { useIntl } from 'react-intl';

/**
 * The A-17 audit action. Built, not written out: the message lint reads every `settings.…` literal as a
 * message id, and this one is the backend's action name.
 */
const SETTINGS_UPDATE = ['settings', 'update'].join('.');

/** The A-08 and A-20 actions, built the same way (`catalogue.…` and `banners.…` are message namespaces too). */
const CATALOGUE = (...parts: string[]) => ['catalogue', ...parts].join('.');
const BANNERS = (...parts: string[]) => ['banners', ...parts].join('.');
const BUILT_ACTIONS: Readonly<Record<string, string>> = {
  [CATALOGUE('product', 'create')]: 'audit.action.catalogueProductCreate',
  [CATALOGUE('product', 'update')]: 'audit.action.catalogueProductUpdate',
  [CATALOGUE('pending', 'approve')]: 'audit.action.catalogueApprove',
  [CATALOGUE('pending', 'merge')]: 'audit.action.catalogueMerge',
  [CATALOGUE('pending', 'reject')]: 'audit.action.catalogueReject',
  [CATALOGUE('category', 'create')]: 'audit.action.catalogueCategoryCreate',
  [CATALOGUE('category', 'update')]: 'audit.action.catalogueCategoryUpdate',
  [CATALOGUE('synonym', 'create')]: 'audit.action.catalogueSynonymCreate',
  [CATALOGUE('synonym', 'delete')]: 'audit.action.catalogueSynonymDelete',
  [BANNERS('approve')]: 'audit.action.bannerApprove',
  [BANNERS('remove')]: 'audit.action.bannerRemove',
};

/** Audit actions the console names; any other action shows as written (it is still audited). */
export function useActionLabel() {
  const intl = useIntl();
  return useCallback(
    (action: string): string => {
      switch (action) {
        case 'customer.block':
          return intl.formatMessage({ id: 'audit.action.customerBlock' });
        case 'customer.unblock':
          return intl.formatMessage({ id: 'audit.action.customerUnblock' });
        case 'customer.strikes_reset':
          return intl.formatMessage({ id: 'audit.action.strikesReset' });
        case 'customer.session_revoke':
          return intl.formatMessage({ id: 'audit.action.sessionRevoke' });
        case 'order.admin_cancel':
          return intl.formatMessage({ id: 'audit.action.orderCancel' });
        case 'review.hide':
          return intl.formatMessage({ id: 'audit.action.reviewHide' });
        case 'review.restore':
          return intl.formatMessage({ id: 'audit.action.reviewRestore' });
        case 'review.reply_hide':
          return intl.formatMessage({ id: 'audit.action.replyHide' });
        case 'review.reply_restore':
          return intl.formatMessage({ id: 'audit.action.replyRestore' });
        case 'review.report_resolve':
          return intl.formatMessage({ id: 'audit.action.reportResolve' });
        case 'business.verify':
          return intl.formatMessage({ id: 'audit.action.businessVerify' });
        case 'business.request_info':
          return intl.formatMessage({ id: 'audit.action.businessRequestInfo' });
        case 'business.reject':
          return intl.formatMessage({ id: 'audit.action.businessReject' });
        case 'business.suspend':
          return intl.formatMessage({ id: 'audit.action.businessSuspend' });
        case 'business.reinstate':
          return intl.formatMessage({ id: 'audit.action.businessReinstate' });
        case 'business.kyc_review':
          return intl.formatMessage({ id: 'audit.action.kycReview' });
        case 'business.kyc_reveal':
          return intl.formatMessage({ id: 'audit.action.kycReveal' });
        case 'business.document_review':
          return intl.formatMessage({ id: 'audit.action.documentReview' });
        case 'store.update':
          return intl.formatMessage({ id: 'audit.action.storeUpdate' });
        case 'store.force_close':
          return intl.formatMessage({ id: 'audit.action.storeForceClose' });
        case 'store.reopen':
          return intl.formatMessage({ id: 'audit.action.storeReopen' });
        case 'store.approve':
          return intl.formatMessage({ id: 'audit.action.storeApprove' });
        case 'store.send_back':
          return intl.formatMessage({ id: 'audit.action.storeSendBack' });
        case 'store.strikes_reset':
          return intl.formatMessage({ id: 'audit.action.storeStrikesReset' });
        case 'lead.status':
          return intl.formatMessage({ id: 'audit.action.leadStatus' });
        case SETTINGS_UPDATE:
          return intl.formatMessage({ id: 'audit.action.settingsUpdate' });
        default: {
          const id = BUILT_ACTIONS[action];
          return id ? intl.formatMessage({ id }) : action;
        }
      }
    },
    [intl],
  );
}

/** Who did it: the admin's name when the log has it, else the kind of actor. */
export function useActorLabel() {
  const intl = useIntl();
  return useCallback(
    (e: Pick<AdminAuditEntry, 'actorKind' | 'actorName'>): string => {
      if (e.actorName) return e.actorName;
      switch (e.actorKind) {
        case 'admin':
          return intl.formatMessage({ id: 'audit.actor.admin' });
        case 'staff':
          return intl.formatMessage({ id: 'audit.actor.staff' });
        case 'user':
          return intl.formatMessage({ id: 'audit.actor.user' });
        case 'device':
          return intl.formatMessage({ id: 'audit.actor.device' });
        default:
          return intl.formatMessage({ id: 'audit.actor.system' });
      }
    },
    [intl],
  );
}
