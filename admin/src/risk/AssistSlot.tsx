import type { RiskFlag } from '@todayz/api-client/gen/model/riskFlag';

/**
 * Where the risk explanation (AI-A3, D-82) sits in the A-13 flag detail. W3-13 renders
 * `admin/src/ai-assist/risk` inside this mount; until then it is an empty box. The page mounts it
 * only while `ai_riskexplain_enabled` is on (the detail's `assistEnabled`). What it explains is a
 * draft: the reviewer still clears or confirms with the buttons above.
 */
export function AssistSlot({ flag }: { flag: RiskFlag }) {
  return <div data-slot="risk-assist" data-flag={flag.id} />;
}
