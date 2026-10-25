import { useCallback, useEffect, useRef, useState } from 'react';

/** A hold shorter than this is a tap, not a recording. */
export const MIN_SECONDS = 0.5;
/** Bars in the live waveform. */
export const BARS = 28;

export interface Recording {
  blob: Blob;
  seconds: number;
}

export type RecorderStatus = 'idle' | 'starting' | 'recording';
export type RecorderError = 'unsupported' | 'denied' | 'short';

/** Containers the contract takes, in the order the browser is asked for them. */
const TYPES = ['audio/webm;codecs=opus', 'audio/webm', 'audio/ogg;codecs=opus', 'audio/ogg', 'audio/mp4'];

export function recorderSupported(): boolean {
  return typeof MediaRecorder !== 'undefined' && typeof navigator !== 'undefined' && typeof navigator.mediaDevices?.getUserMedia === 'function';
}

function pickType(): string | undefined {
  if (typeof MediaRecorder.isTypeSupported !== 'function') return undefined;
  return TYPES.find((t) => MediaRecorder.isTypeSupported(t));
}

/** "audio/webm;codecs=opus" → "audio/webm": the upload's part type, without parameters. */
export function baseType(t: string): string {
  return t.split(';')[0]?.trim() || 'audio/webm';
}

/** Root mean square of one frame of 8-bit samples centred on 128, scaled to 0…1. */
export function level(frame: Uint8Array): number {
  if (frame.length === 0) return 0;
  let sum = 0;
  for (const v of frame) {
    const x = (v - 128) / 128;
    sum += x * x;
  }
  return Math.min(1, Math.sqrt(sum / frame.length) * 3);
}

interface Parts {
  stream: MediaStream | null;
  rec: MediaRecorder | null;
  ctx: AudioContext | null;
  raf: number;
  timer: ReturnType<typeof setTimeout> | undefined;
  tick: ReturnType<typeof setInterval> | undefined;
  startedAt: number;
  phase: RecorderStatus;
  stopWanted: boolean;
}

const none = (): Parts => ({ stream: null, rec: null, ctx: null, raf: 0, timer: undefined, tick: undefined, startedAt: 0, phase: 'idle', stopWanted: false });

/**
 * Hold-to-talk recording: `start` on press, `stop` on release, and a stop of its own at
 * `maxSeconds`. Nothing is kept: the recording goes to `onDone` and the microphone is released.
 */
export function useRecorder({ maxSeconds, onDone }: { maxSeconds: number; onDone: (r: Recording) => void }) {
  const [status, setStatus] = useState<RecorderStatus>('idle');
  const [error, setError] = useState<RecorderError | null>(null);
  const [elapsed, setElapsed] = useState(0);
  const [levels, setLevels] = useState<number[]>(() => Array<number>(BARS).fill(0));
  const parts = useRef<Parts>(none());
  const done = useRef(onDone);
  useEffect(() => {
    done.current = onDone;
  }, [onDone]);

  const release = useCallback(() => {
    const p = parts.current;
    clearTimeout(p.timer);
    clearInterval(p.tick);
    if (p.raf) cancelAnimationFrame(p.raf);
    p.stream?.getTracks().forEach((t) => t.stop());
    void p.ctx?.close().catch(() => undefined);
    parts.current = none();
    setLevels(Array<number>(BARS).fill(0));
  }, []);

  const stop = useCallback(() => {
    const p = parts.current;
    if (p.phase === 'recording' && p.rec?.state === 'recording') p.rec.stop();
    else if (p.phase === 'starting') p.stopWanted = true;
  }, []);

  const start = useCallback(async () => {
    if (parts.current.phase !== 'idle') return;
    setError(null);
    if (!recorderSupported()) {
      setError('unsupported');
      return;
    }
    parts.current.phase = 'starting';
    parts.current.stopWanted = false;
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
      // Let go before the microphone opened: a tap.
      release();
      setStatus('idle');
      setError('short');
      return;
    }
    const type = pickType();
    const rec = new MediaRecorder(stream, type ? { mimeType: type } : undefined);
    const chunks: Blob[] = [];
    rec.ondataavailable = (e) => {
      if (e.data.size > 0) chunks.push(e.data);
    };
    rec.onstop = () => {
      const seconds = (performance.now() - p.startedAt) / 1000;
      const blob = new Blob(chunks, { type: baseType(rec.mimeType || type || '') });
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
    p.tick = setInterval(() => setElapsed(Math.min(maxSeconds, (performance.now() - p.startedAt) / 1000)), 200);

    // The live waveform; a browser without Web Audio records without it.
    const Ctx = typeof AudioContext !== 'undefined' ? AudioContext : undefined;
    if (Ctx) {
      try {
        const ctx = new Ctx();
        p.ctx = ctx;
        const an = ctx.createAnalyser();
        an.fftSize = 256;
        ctx.createMediaStreamSource(stream).connect(an);
        const frame = new Uint8Array(an.fftSize);
        let n = 0;
        const draw = () => {
          // Every third frame is enough for bars and keeps re-renders down.
          if (n++ % 3 === 0) {
            an.getByteTimeDomainData(frame);
            const v = level(frame);
            setLevels((old) => [...old.slice(1), v]);
          }
          p.raf = requestAnimationFrame(draw);
        };
        p.raf = requestAnimationFrame(draw);
      } catch {
        p.ctx = null;
      }
    }
  }, [maxSeconds, release, stop]);

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

  const reset = useCallback(() => setError(null), []);
  return { status, error, elapsed, levels, start, stop, reset };
}
