import { mkdirSync, openSync, closeSync, appendFileSync } from "node:fs";
import { dirname } from "node:path";
import { Event, isInFlight } from "../event/bus.js";

// Store keeps the recent event ring buffer and the running totals
// (cost, in/out tokens, request count, in-flight trace count).
// All public methods are safe to call from multiple async tasks
// concurrently; the in-memory state is guarded by a single lock.
export class Store {
  private events: Event[] = [];
  private cap: number;
  private nextId = 0;
  private logPath: string;

  private totalCost = 0;
  private totalIn = 0;
  private totalOut = 0;
  private totalReqs = 0;
  private inFlight: Set<number> = new Set();

  private lock: Promise<void> = Promise.resolve();

  constructor(logPath: string, cap: number) {
    this.cap = cap;
    this.logPath = logPath;
    if (logPath) {
      mkdirSync(dirname(logPath), { recursive: true });
      // Touch the file so the directory is created even if we never write.
      const fd = openSync(logPath, "a");
      closeSync(fd);
    }
  }

  // withLock serializes all state-mutating operations. JavaScript is
  // single-threaded, but we use a chain of promises to make multi-step
  // read-modify-write sequences atomic (e.g. emit + log + emit end).
  private async withLock<T>(fn: () => T | Promise<T>): Promise<T> {
    const prev = this.lock;
    let resolve!: () => void;
    this.lock = new Promise<void>((r) => (resolve = r));
    try {
      await prev;
      return await fn();
    } finally {
      resolve();
    }
  }

  async append(e: Event): Promise<Event> {
    return this.withLock(() => {
      this.nextId++;
      e.id = this.nextId;
      if (!e.time || isNaN(e.time.getTime())) {
        e.time = new Date();
      }

      this.events.push(e);
      if (this.events.length > this.cap) {
        this.events = this.events.slice(this.events.length - this.cap);
      }

      if (isInFlight(e)) {
        this.inFlight.add(e.traceId);
      } else {
        this.inFlight.delete(e.traceId);
        this.totalReqs++;
        this.totalCost += e.costUSD;
        this.totalIn += e.inputTokens;
        this.totalOut += e.outputTokens;
      }

      if (this.logPath) {
        try {
          appendFileSync(this.logPath, JSON.stringify(e) + "\n");
        } catch {
          // Logging must never break the proxy.
        }
      }
      return e;
    });
  }

  async recent(n: number): Promise<Event[]> {
    return this.withLock(() => {
      const k = Math.min(Math.max(n, 0), this.events.length);
      return this.events.slice(this.events.length - k);
    });
  }

  async stats(): Promise<{
    cost: number;
    in: number;
    out: number;
    reqs: number;
    inFlight: number;
  }> {
    return this.withLock(() => ({
      cost: this.totalCost,
      in: this.totalIn,
      out: this.totalOut,
      reqs: this.totalReqs,
      inFlight: this.inFlight.size,
    }));
  }
}
