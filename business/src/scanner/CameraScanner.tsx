// Camera scanning through the browser's `BarcodeDetector` (Chrome and Edge on Android, Safari 17+).
// Where it is missing the sheet says so and the keyboard scanner and the search bar still work.
// TODO(lead): a ZXing fallback for the remaining browsers needs a new dependency (D-88); not added.
import { Button } from '@todayz/ui';
import { useEffect, useRef, useState } from 'react';

interface DetectedBarcode {
  rawValue: string;
}
interface BarcodeDetectorLike {
  detect(source: CanvasImageSource): Promise<DetectedBarcode[]>;
}
interface BarcodeDetectorCtor {
  new (opts?: { formats?: string[] }): BarcodeDetectorLike;
  getSupportedFormats?: () => Promise<string[]>;
}

export interface CameraScannerLabels {
  unsupported: string;
  denied: string;
  starting: string;
  hint: string;
  close: string;
}

export interface CameraScannerProps {
  labels: CameraScannerLabels;
  onDetect: (code: string) => void;
  onClose: () => void;
}

type Phase = 'starting' | 'live' | 'unsupported' | 'denied';

const FORMATS = ['ean_13', 'ean_8', 'upc_a', 'upc_e', 'code_128', 'code_39', 'itf', 'qr_code'];
const SAME_CODE_MS = 1500;

export function cameraScanSupported(): boolean {
  return typeof window !== 'undefined' && 'BarcodeDetector' in window && !!navigator.mediaDevices?.getUserMedia;
}

export function CameraScanner({ labels, onDetect, onClose }: CameraScannerProps) {
  const video = useRef<HTMLVideoElement>(null);
  const onDetectRef = useRef(onDetect);
  const [phase, setPhase] = useState<Phase>(cameraScanSupported() ? 'starting' : 'unsupported');

  useEffect(() => {
    onDetectRef.current = onDetect;
  }, [onDetect]);

  useEffect(() => {
    if (!cameraScanSupported()) return;
    let stopped = false;
    let stream: MediaStream | null = null;
    let timer: ReturnType<typeof setInterval> | undefined;
    const Detector = (window as unknown as { BarcodeDetector: BarcodeDetectorCtor }).BarcodeDetector;
    const detector = new Detector({ formats: FORMATS });
    let lastCode = '';
    let lastAt = 0;
    navigator.mediaDevices
      .getUserMedia({ video: { facingMode: { ideal: 'environment' } }, audio: false })
      .then(async (s) => {
        if (stopped) {
          s.getTracks().forEach((t) => t.stop());
          return;
        }
        stream = s;
        const el = video.current;
        if (!el) return;
        el.srcObject = s;
        await el.play().catch(() => undefined);
        setPhase('live');
        timer = setInterval(() => {
          if (el.readyState < 2) return;
          detector
            .detect(el)
            .then((found) => {
              const code = found[0]?.rawValue;
              const t = Date.now();
              if (!code || stopped || (code === lastCode && t - lastAt < SAME_CODE_MS)) return;
              lastCode = code;
              lastAt = t;
              onDetectRef.current(code);
            })
            .catch(() => undefined);
        }, 200);
      })
      .catch(() => {
        if (!stopped) setPhase('denied');
      });
    return () => {
      stopped = true;
      if (timer) clearInterval(timer);
      stream?.getTracks().forEach((t) => t.stop());
    };
  }, []);

  return (
    <div>
      {(phase === 'starting' || phase === 'live') && (
        <>
          <video ref={video} muted playsInline aria-label={labels.hint} style={{ width: '100%', borderRadius: 'var(--radius-md)', background: 'var(--ink-1)' }} />
          <p className="tz-sub" role="status">
            {phase === 'starting' ? labels.starting : labels.hint}
          </p>
        </>
      )}
      {phase === 'unsupported' && <p role="status">{labels.unsupported}</p>}
      {phase === 'denied' && <p role="status">{labels.denied}</p>}
      <Button block onClick={onClose}>
        {labels.close}
      </Button>
    </div>
  );
}
