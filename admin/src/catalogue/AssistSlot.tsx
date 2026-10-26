import type { AdminPendingProduct } from '@todayz/api-client/gen/model/adminPendingProduct';
import { CatalogueAssist } from '../ai-assist/catalogue/CatalogueAssist';

/**
 * Where the catalogue assistant (AI-A1, D-82) sits in the review detail. W3-13 renders
 * `admin/src/ai-assist/catalogue` inside this mount; until then it is an empty box. The page mounts
 * it only while `ai_catalogue_enabled` is on (the queue's `assistEnabled`). What the assistant
 * suggests is a draft: the reviewer still approves, merges or rejects with the buttons above.
 */
export function AssistSlot({ pending }: { pending: AdminPendingProduct }) {
  return (
    <div data-slot="catalogue-assist" data-product={pending.product.id}>
      <CatalogueAssist key={pending.product.id} pending={pending} />
    </div>
  );
}
