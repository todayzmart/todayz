import { createContext, useContext, type ReactNode } from 'react';
import type { LucideIcon } from '../internal/Icon';

export interface ToastAction {
  label: string;
  onClick?: (() => void) | undefined;
  href?: string | undefined;
}

export interface ToastProps {
  message: ReactNode;
  icon?: LucideIcon | undefined;
  action?: ToastAction | undefined;
  className?: string | undefined;
}

export interface ShowToast extends ToastProps {
  /**
   * Milliseconds before it hides; null keeps it until dismissed (the app-update toast, G9).
   * Defaults to the provider's duration.
   */
  duration?: number | null | undefined;
}

export interface ToastEntry extends ShowToast {
  id: number;
}

export interface ToastApi {
  show: (toast: ShowToast) => number;
  dismiss: (id: number) => void;
}

export const ToastContext = createContext<ToastApi | null>(null);

/** Shows toasts from anywhere under ToastProvider. */
export function useToast(): ToastApi {
  const api = useContext(ToastContext);
  if (!api) throw new Error('useToast must be used inside <ToastProvider>');
  return api;
}
