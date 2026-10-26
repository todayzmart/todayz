// C-23 voice search (AI-C5, D-101): the mic in the C-05 search field. Shown only when voice search
// is on for this customer (flag and speech key, GET /v1/me/ai/features), they are signed in and
// online, and the browser can record a type the server takes. The transcript fills the box for
// editing and never searches by itself. At the daily cap the mic is disabled; a speech provider
// failure hides it for the rest of this session. No audio and no transcript is kept: the
// recording lives in memory until the upload, the transcript only in the search box.
import { ApiError } from '@todayz/api-client';
import { voiceSearch } from '@todayz/api-client/gen/ai/ai';
import { useOnline } from '@todayz/auth';
import { IconButton } from '@todayz/ui';
import { Mic, Square } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { clampInt, eventLang, track } from '../ai/events';
import { isCapReached, useAiFeatures } from '../ai/features';
import { pickType, useRecorder, type Recording } from './recorder';
import { MAX_EVENT_SECONDS, getVoiceBlock, setVoiceBlock, type Heard } from './session';

export type { Heard } from './session';

export interface VoiceSearchProps {
  onTranscript: (heard: Heard) => void;
  /** A line for under the search box (listening, the cap, a failure); null clears it. */
  onNotice: (text: string | null) => void;
}

export function VoiceSearch(props: VoiceSearchProps) {
  const features = useAiFeatures();
  const online = useOnline();
  const [state, setState] = useState(getVoiceBlock);
  if (!features?.voiceSearch || !online || state === 'failed') return null;
  const type = pickType(features.audioTypes);
  if (!type) return null;
  const block = (b: 'failed' | 'capped') => {
    setVoiceBlock(b);
    setState(b);
  };
  return (
    <MicButton
      {...props}
      type={type}
      maxSeconds={features.maxAudioSeconds}
      maxBytes={features.maxAudioBytes}
      capped={state === 'capped'}
      onBlock={block}
    />
  );
}

interface MicProps extends VoiceSearchProps {
  type: string;
  maxSeconds: number;
  maxBytes: number;
  capped: boolean;
  onBlock: (b: 'failed' | 'capped') => void;
}

function MicButton({ onTranscript, onNotice, type, maxSeconds, maxBytes, capped, onBlock }: MicProps) {
  const intl = useIntl();
  const [sending, setSending] = useState(false);
  const abort = useRef<AbortController | null>(null);
  const notice = useRef(onNotice);
  useEffect(() => {
    notice.current = onNotice;
  }, [onNotice]);
  useEffect(() => () => abort.current?.abort(), []);

  const fail = (seconds: number, outcome: 'failed' | 'cap_reached') =>
    track('voice_search_used', { lang: eventLang(intl.locale), seconds: clampInt(seconds, MAX_EVENT_SECONDS), outcome });

  const send = async ({ blob, seconds }: Recording) => {
    if (blob.size > maxBytes) {
      fail(seconds, 'failed');
      notice.current(intl.formatMessage({ id: 'search.voice.failed' }));
      return;
    }
    const ctl = new AbortController();
    abort.current = ctl;
    setSending(true);
    notice.current(intl.formatMessage({ id: 'search.voice.working' }));
    try {
      const r = await voiceSearch({ audio: blob }, { signal: ctl.signal });
      const transcript = r.transcript.trim();
      if (!transcript) {
        notice.current(intl.formatMessage({ id: 'search.voice.short' }));
        return;
      }
      onTranscript({ transcript, lang: r.language, seconds });
      notice.current(intl.formatMessage({ id: 'search.voice.heard' }));
    } catch (err) {
      if (ctl.signal.aborted) return;
      if (isCapReached(err)) {
        fail(seconds, 'cap_reached');
        onBlock('capped');
        notice.current(intl.formatMessage({ id: 'search.voice.capped' }));
        return;
      }
      fail(seconds, 'failed');
      // The provider (503), the switch turned off (404) or no answer at all: hide the mic for this
      // session. A refused upload (422) keeps it, the next recording may be fine.
      if (!(err instanceof ApiError) || err.status >= 500 || err.status === 404) onBlock('failed');
      notice.current(intl.formatMessage({ id: 'search.voice.failed' }));
    } finally {
      if (abort.current === ctl) abort.current = null;
      setSending(false);
    }
  };

  const rec = useRecorder({ type, maxSeconds, onDone: (r) => void send(r) });

  useEffect(() => {
    if (rec.status === 'recording') {
      notice.current(intl.formatMessage({ id: 'search.voice.listening' }, { seconds: String(Math.max(0, Math.ceil(maxSeconds - rec.elapsed))) }));
    }
  }, [rec.status, rec.elapsed, maxSeconds, intl]);
  useEffect(() => {
    if (rec.error) notice.current(intl.formatMessage({ id: rec.error === 'denied' ? 'search.voice.denied' : 'search.voice.short' }));
  }, [rec.error, intl]);

  if (capped) {
    return <IconButton icon={Mic} size="sm" variant="ghost" disabled label={intl.formatMessage({ id: 'search.voice.capped' })} />;
  }
  const recording = rec.status === 'recording';
  return (
    <IconButton
      icon={recording ? Square : Mic}
      size="sm"
      variant="ghost"
      disabled={sending || rec.status === 'starting'}
      aria-pressed={recording}
      label={intl.formatMessage({ id: recording ? 'search.voice.stop' : 'search.voice.start' })}
      onClick={() => (recording ? rec.stop() : void rec.start())}
    />
  );
}
