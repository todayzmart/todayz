import { Component, type ErrorInfo, type ReactNode } from 'react';

export interface ErrorBoundaryProps {
  /** What to show after a render error; `reset` clears it. */
  fallback: (reset: () => void) => ReactNode;
  /** For telemetry; receives the error and component stack. Never log props or user data here. */
  onError?: ((error: Error, info: ErrorInfo) => void) | undefined;
  /** Changing this value clears a shown error (e.g. the route path). */
  resetKey?: unknown;
  children?: ReactNode;
}

interface State {
  failed: boolean;
}

/** The app-level error boundary: one fallback screen instead of a white page. */
export class ErrorBoundary extends Component<ErrorBoundaryProps, State> {
  override state: State = { failed: false };

  static getDerivedStateFromError(): State {
    return { failed: true };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    this.props.onError?.(error, info);
  }

  override componentDidUpdate(prev: ErrorBoundaryProps): void {
    if (this.state.failed && prev.resetKey !== this.props.resetKey) this.setState({ failed: false });
  }

  private reset = () => this.setState({ failed: false });

  override render(): ReactNode {
    return this.state.failed ? this.props.fallback(this.reset) : this.props.children;
  }
}
