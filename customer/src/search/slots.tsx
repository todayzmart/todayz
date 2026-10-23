// Empty slots for features that arrive later, so the screens keep their layout when they land.
// They render nothing today (AI features stay empty in this slice; D-101).

/** AI-C2 search assist: suggestion chips under the summary line when the response says `refineHint`. */
export function SearchAssistSlot(_props: { query: string; refineHint: boolean }) {
  // TODO(W3): search assist chips (04 §2.8), signed-in customers only, after results have rendered.
  return null;
}

/** AI-C5 voice search: the mic inside the search field. Hidden until the speech provider is wired. */
export function VoiceSearchSlot(_props: { onTranscript: (text: string) => void }) {
  // TODO(W3): mic button; the transcript fills the box and never submits by itself (01 §11.1).
  return null;
}

/** §11.2 "Ask shops nearby": zero results, and every product page. `shopSlug` limits it to one shop ("Ask this shop"). */
export function AskNearbySlot(_props: { query: string; shopSlug?: string }) {
  // TODO(W3): the ask flow (04 §2.x asks); needs the asks contract.
  return null;
}
