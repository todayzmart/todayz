import { Banner } from '@todayz/ui';
import { Mic } from 'lucide-react';
import type { KeyboardEvent, PointerEvent } from 'react';
import { useIntl } from 'react-intl';
import { useRecorder, type Recording } from './recorder';
import styles from './voice.module.css';

export interface HoldToTalkProps {
  maxSeconds: number;
  disabled?: boolean | undefined;
  onRecorded: (r: Recording) => void;
}

const clock = (s: number) => `0:${String(Math.floor(s)).padStart(2, '0')}`;

/**
 * Hold to talk: press and hold (or hold Space or Enter) while speaking, let go to send. Stops by
 * itself at `maxSeconds`. A live waveform shows the microphone is hearing something.
 */
export function HoldToTalk({ maxSeconds, disabled, onRecorded }: HoldToTalkProps) {
  const intl = useIntl();
  const r = useRecorder({ maxSeconds, onDone: onRecorded });
  const live = r.status !== 'idle';

  const down = (e: PointerEvent<HTMLButtonElement>) => {
    if (disabled || e.button !== 0) return;
    e.preventDefault();
    e.currentTarget.setPointerCapture?.(e.pointerId);
    void r.start();
  };
  const keyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (disabled || e.repeat || (e.key !== ' ' && e.key !== 'Enter')) return;
    e.preventDefault();
    void r.start();
  };
  const keyUp = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (e.key !== ' ' && e.key !== 'Enter') return;
    e.preventDefault();
    r.stop();
  };

  return (
    <div className={styles.talkBox}>
      <button
        type="button"
        className={`tz-btn tz-btn-primary ${styles.talk}`}
        data-live={live || undefined}
        aria-pressed={live}
        disabled={disabled}
        onPointerDown={down}
        onPointerUp={r.stop}
        onPointerCancel={r.stop}
        onKeyDown={keyDown}
        onKeyUp={keyUp}
        onContextMenu={(e) => e.preventDefault()}
      >
        <Mic aria-hidden="true" size={28} />
        <span>{intl.formatMessage({ id: live ? 'voice.talk.release' : 'voice.talk.hold' })}</span>
      </button>
      <div className={styles.wave} aria-hidden="true" data-live={live || undefined}>
        {r.levels.map((v, i) => (
          <i key={i} style={{ height: `${Math.max(8, Math.round(v * 100))}%` }} />
        ))}
      </div>
      <p className={styles.timer} aria-live="off">
        {intl.formatMessage({ id: 'voice.talk.timer' }, { now: clock(r.elapsed), max: clock(maxSeconds) })}
      </p>
      {r.status === 'recording' && (
        <p className="tz-ui-sr-only" role="status">
          {intl.formatMessage({ id: 'voice.talk.listening' })}
        </p>
      )}
      {r.error && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({ id: `voice.talk.error.${r.error}` })}
        </Banner>
      )}
    </div>
  );
}
