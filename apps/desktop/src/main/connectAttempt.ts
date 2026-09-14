/** Tracks the one in-flight connect attempt so the user can stop it: `isCancelled`
 *  checked immediately before the daemon connect is what makes stop mean stopped. */

export interface ConnectAttempt {
  /** Monotonic id. Cancelling by id can't kill a newer attempt. */
  readonly id: number;
  /** Aborts the attempt's in-flight HTTP requests. */
  readonly controller: AbortController;
  cancelled: boolean;
  /** Past the point where tearing the tunnel down would be wrong. */
  committed: boolean;
}

let current: ConnectAttempt | null = null;
let nextId = 1;

/** Open a new attempt, replacing (and cancelling) any previous one — a second
 *  Connect press supersedes the first rather than racing it. */
export const beginAttempt = (): ConnectAttempt => {
  if (current && !current.cancelled) {
    cancelAttempt();
  }
  current = { id: nextId++, controller: new AbortController(), cancelled: false, committed: false };
  return current;
};

/** Marks the attempt past its point of no return: once the tunnel is up, Stop
 *  must disconnect explicitly instead of racing a cancel. */
export const commitAttempt = (attempt: ConnectAttempt): void => {
  if (current && current.id === attempt.id) {
    current.committed = true;
  }
};

/** Cancels the in-flight attempt, if any, and returns it so the caller knows
 *  whether to tear a tunnel down. A committed attempt cannot be cancelled here. */
export const cancelAttempt = (): ConnectAttempt | null => {
  if (!current || current.cancelled || current.committed) return null;
  current.cancelled = true;
  // Abort AFTER marking cancelled: an abort listener that re-reads the flag
  // must never observe a live attempt whose requests are already dead.
  current.controller.abort();
  return current;
};

/** True once this attempt has been cancelled, or superseded by a newer one.
 *  Checked above all immediately before the daemon connect. */
export const isCancelled = (attempt: ConnectAttempt): boolean =>
  attempt.cancelled || current === null || current.id !== attempt.id;

/** Clears the attempt when it finishes normally, so a later cancel press is a
 *  no-op. Only the attempt that owns the slot may clear it. */
export const endAttempt = (attempt: ConnectAttempt): void => {
  if (current && current.id === attempt.id) {
    current = null;
  }
};

/** Is a connect attempt in flight right now? */
export const hasActiveAttempt = (): boolean => current !== null && !current.cancelled;

/** Test seam — drops any attempt state between cases. */
export const resetAttemptsForTest = (): void => {
  current = null;
  nextId = 1;
};
