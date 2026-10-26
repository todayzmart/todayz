// Slots for features that land from other work packages, so the screens keep their layout. The ask
// flow is W3-16; search assist and voice search are W3-12 (D-101).
import { AskNearby } from '../asks/AskNearby';
import { SearchAssist, type SearchAssistProps } from '../search-assist/SearchAssist';
import { VoiceSearch, type Heard } from '../voice-search/VoiceSearch';

/** AI-C2 search assist: suggestion chips under the summary line when the response says `refineHint`. */
export function SearchAssistSlot(props: SearchAssistProps) {
  return <SearchAssist {...props} />;
}

/** AI-C5 voice search: the mic inside the search field; the transcript fills the box and never submits by itself (01 §11.1). */
export function VoiceSearchSlot(props: { onTranscript: (heard: Heard) => void; onNotice: (text: string | null) => void }) {
  return <VoiceSearch {...props} />;
}

/** §11.2 "Ask shops nearby": zero results, and every product page. `shopSlug` limits it to one shop ("Ask this shop"). */
export function AskNearbySlot(props: { query: string; productId?: string; shopSlug?: string }) {
  return <AskNearby {...props} />;
}
