import { ApiError } from '@todayz/api-client';
import type { MyTicketAttachment } from '@todayz/api-client/gen/model/myTicketAttachment';
import { useCallback, useState, type ChangeEvent } from 'react';
import { useIntl } from 'react-intl';
import { ACCEPT, MAX_BYTES, MAX_FILES } from './limits';
import { uploadFile } from './queries';

export interface Attachments {
  files: MyTicketAttachment[];
  uploading: boolean;
  error: string | null;
  pick(e: ChangeEvent<HTMLInputElement>): Promise<void>;
  remove(id: string): void;
  clear(): void;
}

/**
 * Files for one message. Each is checked here (type, size, count), registered, and sent straight to
 * storage as soon as it is picked; the message then claims the uploads by id.
 */
export function useAttachments(): Attachments {
  const intl = useIntl();
  const [files, setFiles] = useState<MyTicketAttachment[]>([]);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const pick = useCallback(
    async (e: ChangeEvent<HTMLInputElement>) => {
      const chosen = Array.from(e.target.files ?? []);
      e.target.value = '';
      setError(null);
      let count = files.length;
      setUploading(true);
      try {
        for (const f of chosen) {
          if (count >= MAX_FILES) {
            setError(intl.formatMessage({ id: 'help.files.tooMany' }, { max: MAX_FILES }));
            return;
          }
          if (!(ACCEPT as readonly string[]).includes(f.type)) {
            setError(intl.formatMessage({ id: 'help.files.type' }, { name: f.name }));
            continue;
          }
          if (f.size < 1) {
            setError(intl.formatMessage({ id: 'help.files.empty' }, { name: f.name }));
            continue;
          }
          if (f.size > MAX_BYTES) {
            setError(intl.formatMessage({ id: 'help.files.size' }, { name: f.name, mb: MAX_BYTES / 1024 / 1024 }));
            continue;
          }
          try {
            const a = await uploadFile(f);
            count += 1;
            setFiles((list) => [...list, a]);
          } catch (err) {
            if (err instanceof ApiError && err.code === 'rate_limited') {
              setError(intl.formatMessage({ id: 'auth.error.rateLimited' }));
              return;
            }
            setError(intl.formatMessage({ id: 'help.files.failed' }, { name: f.name }));
          }
        }
      } finally {
        setUploading(false);
      }
    },
    [files.length, intl],
  );

  return {
    files,
    uploading,
    error,
    pick,
    remove: (id) => setFiles((list) => list.filter((x) => x.id !== id)),
    clear: () => {
      setFiles([]);
      setError(null);
    },
  };
}
