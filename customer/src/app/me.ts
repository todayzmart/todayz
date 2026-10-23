import { getMe, updateMe } from '@todayz/api-client/gen/me/me';
import type { Session } from '@todayz/auth';

type MeBody = Parameters<typeof updateMe>[0];

/** PATCH /v1/me with If-Match (428 without it, 412 on a mismatch). Reads the ETag first when none is cached. */
export async function patchMe(session: Session, body: MeBody) {
  let tag = session.etag('/v1/me');
  if (!tag) {
    await getMe();
    tag = session.etag('/v1/me');
  }
  return updateMe(body, tag ? { ifMatch: tag } : {});
}
