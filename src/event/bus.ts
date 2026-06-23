// Event represents a single proxy request lifecycle observation.
//
// The proxy emits one "start" event when it receives a request, and a
// matching "end" event when the upstream response completes (or fails).
// Start events have Status=0, Err="" and are considered InFlight.
// End events have Status and tokens populated, and are no longer InFlight.
export interface Event {
  id: number;
  traceId: number;
  time: Date;
  provider: string;
  model: string;
  endpoint: string;
  method: string;
  status: number;
  streaming: boolean;
  durationMs: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheWriteTokens: number;
  costUSD: number;
  promptPreview: string;
  responsePreview: string;
  err: string;
}

export function isInFlight(e: Event): boolean {
  return e.status === 0 && e.err === "";
}

// Bus is a fan-out pub/sub for events. Subscribers receive a buffered
// channel; slow subscribers drop events (non-blocking send) so the
// proxy never stalls on TUI lag.
export class Bus {
  private subs: Set<(e: Event) => void> = new Set();

  subscribe(fn: (e: Event) => void): () => void {
    this.subs.add(fn);
    return () => {
      this.subs.delete(fn);
    };
  }

  emit(e: Event): void {
    for (const fn of this.subs) {
      try {
        fn(e);
      } catch {
        // TUI bugs must not crash the proxy.
      }
    }
  }
}
