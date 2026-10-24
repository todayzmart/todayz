// Whether this browser can scan with the camera (BarcodeDetector plus getUserMedia).
export function cameraScanSupported(): boolean {
  return typeof window !== 'undefined' && 'BarcodeDetector' in window && !!navigator.mediaDevices?.getUserMedia;
}
