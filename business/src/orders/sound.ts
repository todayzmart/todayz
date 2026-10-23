import { useCallback, useEffect, useState } from 'react';

/**
 * New-order sound alert. No audio asset and no dependency: a two-note chime from the Web Audio API,
 * repeated every few seconds until nothing is left to alert on. Browsers only start audio after a
 * tap, so the page shows "Allow sound" until the person has tapped once in this page load.
 */
const PREF_KEY = 'todayz.biz.orders.sound';
export const REPEAT_MS = 4000;

type Ctx = AudioContext;
let ctx: Ctx | null = null;

function getCtx(): Ctx | null {
  if (ctx) return ctx;
  const Ctor = (globalThis as { AudioContext?: new () => Ctx; webkitAudioContext?: new () => Ctx }).AudioContext;
  if (!Ctor) return null;
  try {
    ctx = new Ctor();
  } catch {
    return null;
  }
  return ctx;
}

function readMuted(): boolean {
  try {
    return localStorage.getItem(PREF_KEY) === 'off';
  } catch {
    return false;
  }
}

function writeMuted(muted: boolean): void {
  try {
    localStorage.setItem(PREF_KEY, muted ? 'off' : 'on');
  } catch {
    // The choice just is not remembered.
  }
}

/** One short two-note chime. */
export function playChime(): void {
  const c = getCtx();
  if (!c || c.state !== 'running') return;
  const t0 = c.currentTime;
  [660, 880].forEach((freq, i) => {
    const osc = c.createOscillator();
    const gain = c.createGain();
    osc.type = 'sine';
    osc.frequency.value = freq;
    const start = t0 + i * 0.22;
    gain.gain.setValueAtTime(0.0001, start);
    gain.gain.exponentialRampToValueAtTime(0.35, start + 0.02);
    gain.gain.exponentialRampToValueAtTime(0.0001, start + 0.3);
    osc.connect(gain).connect(c.destination);
    osc.start(start);
    osc.stop(start + 0.32);
  });
}

export interface SoundAlert {
  /** True once the browser lets this page play sound. */
  unlocked: boolean;
  muted: boolean;
  /** Call from a tap: unlocks audio and plays one chime as proof. */
  allow(): void;
  setMuted(m: boolean): void;
}

/** Plays the chime every REPEAT_MS while `active` is true, the sound is unlocked and not muted. */
export function useSoundAlert(active: boolean): SoundAlert {
  const [unlocked, setUnlocked] = useState(() => getCtx()?.state === 'running');
  const [muted, setMutedState] = useState(readMuted);

  const allow = useCallback(() => {
    const c = getCtx();
    if (!c) return;
    void c.resume().then(() => {
      setUnlocked(c.state === 'running');
      if (!readMuted()) playChime();
    });
  }, []);

  // A person who already allowed sound before only has to tap anywhere once per page load.
  useEffect(() => {
    if (unlocked || muted) return undefined;
    const on = () => {
      const c = getCtx();
      if (!c) return;
      void c.resume().then(() => setUnlocked(c.state === 'running'));
    };
    document.addEventListener('pointerdown', on, { once: true });
    return () => document.removeEventListener('pointerdown', on);
  }, [unlocked, muted]);

  useEffect(() => {
    if (!active || !unlocked || muted) return undefined;
    playChime();
    const t = setInterval(playChime, REPEAT_MS);
    return () => clearInterval(t);
  }, [active, unlocked, muted]);

  const setMuted = useCallback((m: boolean) => {
    writeMuted(m);
    setMutedState(m);
  }, []);

  return { unlocked, muted, allow, setMuted };
}
