import { ApiError } from '@todayz/api-client';
import { useCallback } from 'react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';

/** What a `409 conflict` means on each catalogue change. */
export type Taken = 'barcode' | 'slug' | 'synonym';

/**
 * A failed catalogue change in words: a taken barcode, slug or pair, and the reference fields the
 * API refuses by name, read here; anything else falls back to the console's sentence.
 */
export function useCatalogueProblem() {
  const intl = useIntl();
  const problemText = useProblemText();
  return useCallback(
    (err: unknown, taken?: Taken): string => {
      if (err instanceof ApiError && err.code === 'conflict' && taken) {
        switch (taken) {
          case 'barcode':
            return intl.formatMessage({ id: 'catalogue.error.barcodeTaken' });
          case 'slug':
            return intl.formatMessage({ id: 'catalogue.error.slugTaken' });
          case 'synonym':
            return intl.formatMessage({ id: 'catalogue.error.synonymTaken' });
        }
      }
      if (err instanceof ApiError && err.code === 'validation_failed') {
        const f = err.fields;
        if ('body.categoryId' in f) return intl.formatMessage({ id: 'catalogue.error.category' });
        if ('body.intoProductId' in f) return intl.formatMessage({ id: 'catalogue.error.into' });
        if ('body.productId' in f) return intl.formatMessage({ id: 'catalogue.error.product' });
        if ('body.parentId' in f) return intl.formatMessage({ id: 'catalogue.error.parent' });
        if ('body.synonym' in f) return intl.formatMessage({ id: 'catalogue.error.same' });
      }
      return problemText(err);
    },
    [intl, problemText],
  );
}
