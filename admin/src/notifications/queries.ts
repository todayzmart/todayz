import { listAdminIntegrations, listAdminNotificationTemplates, testSendAdminNotification } from '@todayz/api-client/gen/admin-notifications/admin-notifications';
import type { AdminNotificationApp } from '@todayz/api-client/gen/model/adminNotificationApp';
import type { AdminNotificationTestSend } from '@todayz/api-client/gen/model/adminNotificationTestSend';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { refreshAudit } from '../audit/queries';

/** The catalogue is small and code-owned (D-224): one unpaged read per app filter. */
export function useTemplates(app: AdminNotificationApp | undefined) {
  return useQuery({
    queryKey: ['admin', 'notification-templates', app ?? 'all'],
    queryFn: () => listAdminNotificationTemplates(app ? { app } : undefined),
    staleTime: 5 * 60_000,
  });
}

export function useIntegrations() {
  return useQuery({ queryKey: ['admin', 'integrations'], queryFn: () => listAdminIntegrations() });
}

/** Writes one notification to the caller's own customer inbox, in-app only; never retried. */
export function useTestSend() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AdminNotificationTestSend) => testSendAdminNotification(body),
    onSuccess: (r) => refreshAudit(qc, 'notification_template', r.kind),
  });
}
