// C-05 search assist (AI-C2, D-101): after the results of a search with `refineHint` have rendered,
// a signed-in customer with search assist on may see suggestion chips and "Search for '{query}'"
// under the summary line. Nothing applies until tapped. On the 3 s timeout, a 204, the cap or any
// failure nothing shows and the results stay as they are. Nothing is stored on this device.
import { searchAssist } from '@todayz/api-client/gen/ai/ai';
import type { AiSearchChip } from '@todayz/api-client/gen/model/aiSearchChip';
import type { SearchAssistResult } from '@todayz/api-client/gen/model/searchAssistResult';
import { useQuery } from '@tanstack/react-query';
import { Chip } from '@todayz/ui';
import { Plus, Search } from 'lucide-react';
import { useIntl } from 'react-intl';
import { clampInt, track } from '../ai/events';
import { isCapReached, useAiFeatures } from '../ai/features';
import { useFmt } from '../home/format';

/** The call's budget as the app sees it (the server gives the model the same 3 s). */
export const ASSIST_TIMEOUT_MS = 3000;
const MAX_LATENCY_MS = 10_000;

/** A tap: search the cleaned words, or add one filter. */
export type AssistPick = { kind: 'query'; query: string } | { kind: 'chip'; chip: AiSearchChip };

export interface SearchAssistProps {
  query: string;
  refineHint: boolean;
  onPick: (pick: AssistPick) => void;
}

type Outcome = 'shown' | 'empty' | 'timeout' | 'failed' | 'cap_reached';

async function ask(q: string, signal: AbortSignal): Promise<SearchAssistResult | null> {
  const ctl = new AbortController();
  const stop = () => ctl.abort();
  signal.addEventListener('abort', stop);
  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    ctl.abort();
  }, ASSIST_TIMEOUT_MS);
  const started = performance.now();
  let outcome: Outcome = 'failed';
  try {
    const r = await searchAssist({ q }, { signal: ctl.signal });
    const usable = r && (r.chips.length > 0 || (r.query && r.query !== q)) ? r : null;
    outcome = usable ? 'shown' : 'empty';
    return usable;
  } catch (err) {
    outcome = timedOut ? 'timeout' : isCapReached(err) ? 'cap_reached' : 'failed';
    return null;
  } finally {
    clearTimeout(timer);
    signal.removeEventListener('abort', stop);
    // A search the customer left before the answer is not an assist call they saw.
    if (!signal.aborted || timedOut) {
      track('search_assist_used', { outcome, latency_ms: clampInt(performance.now() - started, MAX_LATENCY_MS) });
    }
  }
}

export function SearchAssist({ query, refineHint, onPick }: SearchAssistProps) {
  const features = useAiFeatures();
  const on = Boolean(features?.searchAssist) && refineHint && query.trim().length >= 2;
  const assist = useQuery({
    queryKey: ['me', 'ai', 'search-assist', query],
    enabled: on,
    retry: false,
    staleTime: Infinity,
    gcTime: 60_000,
    refetchOnWindowFocus: false,
    queryFn: ({ signal }) => ask(query, signal),
  });
  if (!on || !assist.data) return null;
  return <Suggestions result={assist.data} query={query} onPick={onPick} />;
}

function Suggestions({ result, query, onPick }: { result: SearchAssistResult; query: string; onPick: (p: AssistPick) => void }) {
  const intl = useIntl();
  const { money } = useFmt();
  const label = (c: AiSearchChip): string => {
    switch (c.kind) {
      case 'maxPrice':
        return intl.formatMessage({ id: 'search.assist.maxPrice' }, { price: money(Number(c.value)) });
      case 'openNow':
        return intl.formatMessage({ id: 'search.filter.open' });
      case 'pickup':
        return intl.formatMessage({ id: 'search.filter.pickup' });
      case 'delivery':
        return intl.formatMessage({ id: 'search.filter.delivery' });
      case 'offers':
        return intl.formatMessage({ id: 'search.filter.offers' });
      case 'nearest':
        return intl.formatMessage({ id: 'search.sort.nearest' });
      case 'brand':
        return c.text;
    }
  };
  const pick = (p: AssistPick) => {
    track('search_assist_used', { outcome: 'tapped', latency_ms: 0 });
    onPick(p);
  };
  const chips = result.chips.filter((c) => c.kind !== 'maxPrice' || Number(c.value) > 0);
  return (
    <div className="tz-chips" role="group" aria-label={intl.formatMessage({ id: 'search.assist.label' })}>
      {result.query && result.query !== query && (
        <Chip pressed={false} icon={Search} onPressedChange={() => pick({ kind: 'query', query: result.query as string })}>
          {intl.formatMessage({ id: 'search.assist.query' }, { query: result.query })}
        </Chip>
      )}
      {chips.map((c, i) => (
        <Chip key={`${c.kind}-${i}`} pressed={false} icon={Plus} onPressedChange={() => pick({ kind: 'chip', chip: c })}>
          {label(c)}
        </Chip>
      ))}
    </div>
  );
}
