// The customer AI product events (shared/telemetry/events/ai.yaml). main.tsx hands in the telemetry
// emitter; until then (and in tests that set none) an event goes nowhere. Props are enums and numbers
// only: never audio, a transcript, the query or a suggestion's words.
import type { EventProps } from '@todayz/telemetry';

export type AiEvent = 'voice_search_used' | 'search_assist_used' | 'list_parse_used';
type Sink = (name: AiEvent, props: EventProps[AiEvent]) => void;

let sink: Sink | null = null;

/** Where events go: `initTelemetry(...).emit` in the app, a recorder in tests. */
export function setAiEventSink(next: Sink | null): void {
  sink = next;
}

export function track<N extends AiEvent>(name: N, props: EventProps[N]): void {
  try {
    sink?.(name, props);
  } catch {
    // Telemetry never breaks a screen.
  }
}

/** The app locale as an event `lang` (en-XA and anything else count as en). */
export function eventLang(locale: string): EventProps['voice_search_used']['lang'] {
  const l = locale.slice(0, 2);
  return l === 'hi' || l === 'kn' || l === 'te' || l === 'ta' ? l : 'en';
}

/** A number of milliseconds or seconds clamped into an event prop's range. */
export function clampInt(v: number, max: number): number {
  return Math.max(0, Math.min(max, Math.round(Number.isFinite(v) ? v : 0)));
}
