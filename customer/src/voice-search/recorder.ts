// C-23 recording: tap the mic to start, tap again to stop, and a stop of its own at the longest
// recording the server takes (features.maxAudioSeconds). Nothing is kept: the recording goes to
// `onDone` once and the microphone is released.
import { useCallback, useEffect, useRef, useState } from 'react';

/** A recording shorter than this is a tap, not speech. */
export const MIN_SECONDS = 0.5;

export interface Recording {
  blob: Blob;
  seconds: number;
}

export type RecorderStatus = 'idle' | 'starting' | 'recording';
export type RecorderError = 'denied' | 'short';

/** What the browser is asked for, in order; only those whose container the server takes are used. */
const TYPES = ['audio/webm;codecs=opus', 'audio/webm', 'audio/ogg;codecs=opus', 'audio/ogg', 'audio/mp4'];

/** "audio/webm;codecs=opus" → "audio/webm": the upload's part type, without parameters. */
export function baseType(t: string): string {
  return t.split(';')[0]?.trim().toLowerCase() || '';
}

/** The type to record in: one the browser makes and the server takes; undefined hides the mic. */
export function pickType(accepted: readonly string[]): string | undefined {
  if (typeof MediaRecorder === 'undefined' || typeof navigator === 'undefined' || typeof navigator.mediaDevices?.getUserMedia !== 'function') {
    return undefined;
  }
  if (typeof MediaRecorder.isTypeSupported !== 'function') return undefined;
  const ok = new Set(accepted.map(baseType));
  return TYPES.find((t) => ok.has(baseType(t)) && MediaRecorder.isTypeSupported(t));
}

interface Parts {
  stream: MediaStream | null;
  rec: MediaRecorder | null;
  timer: ReturnType<typeof setTimeout> | undefined;
  tick: ReturnType<typeof setInterval> | undefined;
  startedAt: number;
  phase: RecorderStatus;
  stopWanted: boolean;
}

const none = (): Parts => ({ stream: null, rec: null, timer: undefined, tick: undefined, startedAt: 0, phase: 'idle', stopWanted: false });

export function useRecorder({ type, maxSeconds, onDone }: { type: string; maxSeconds: number; onDone: (r: Recording) => void }) {
  const [status, setStatus] = useState<RecorderStatus>('idle');
  const [error, setError] = useState<RecorderError | null>(null);
  const [elapsed, setElapsed] = useState(0);
  const parts = useRef<Parts>(none());
  const done = useRef(onDone);
  useEffect(() => {
    done.current = onDone;
  }, [onDone]);

  const release = useCallback(() => {
    const p = parts.current;
    clearTimeout(p.timer);
    clearInterval(p.tick);
    p.stream?.getTracks().forEach((t) => t.stop());
    parts.current = none();
  }, []);

  const stop = useCallback(() => {
    const p = parts.current;
    if (p.phase === 'recording' && p.rec?.state === 'recording') p.rec.stop();
    else if (p.phase === 'starting') p.stopWanted = true;
  }, []);

  const start = useCallback(async () => {
    if (parts.current.phase !== 'idle') return;
    setError(null);
    parts.current.phase = 'starting';
    setStatus('starting');
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch {
      parts.current = none();
      setStatus('idle');
      setError('denied');
      return;
    }
    const p = parts.current;
    p.stream = stream;
    if (p.stopWanted) {
      release();
      setStatus('idle');
      setError('short');
      return;
    }
    const rec = new MediaRecorder(stream, { mimeType: type });
    const chunks: Blob[] = [];
    rec.ondataavailable = (e) => {
      if (e.data.size > 0) chunks.push(e.data);
    };
    rec.onstop = () => {
      const seconds = (performance.now() - p.startedAt) / 1000;
      const blob = new Blob(chunks, { type: baseType(rec.mimeType || type) });
      chunks.length = 0;
      release();
      setStatus('idle');
      setElapsed(0);
      if (seconds < MIN_SECONDS || blob.size === 0) setError('short');
      else done.current({ blob, seconds: Math.min(seconds, maxSeconds) });
    };
    p.rec = rec;
    p.phase = 'recording';
    rec.start();
    p.startedAt = performance.now();
    setStatus('recording');
    setElapsed(0);
    p.timer = setTimeout(stop, maxSeconds * 1000);
    p.tick = setInterval(() => setElapsed(Math.min(maxSeconds, (performance.now() - p.startedAt) / 1000)), 250);
  }, [type, maxSeconds, release, stop]);

  // Leaving the screen mid-recording drops the recording and frees the microphone.
  useEffect(
    () => () => {
      const rec = parts.current.rec;
      if (rec) rec.onstop = null;
      if (rec && rec.state === 'recording') rec.stop();
      release();
    },
    [release],
  );

  return { status, error, elapsed, start, stop };
}
