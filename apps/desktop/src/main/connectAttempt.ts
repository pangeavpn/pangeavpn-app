/**
 * Tracks the one in-flight connect attempt so the user can stop it.
 *
 * Pressing Connect starts a sequence of hub calls and then a daemon connect.
 * Before this existed, "stop" only told the daemon to disconnect: the main
 * process carried on provisioning and called connect() anyway, so the tunnel
 * came UP a second or two after the user had cancelled. The guard that
 * actually makes stop mean stopped is `isCancelled` being checked immediately
 * before the daemon connect — everything else here is just plumbing to abort
 * in-flight work sooner.
 *
 * Deliberately a tiny, dependency-free module: it holds the whole
 * cancellation rule in one testable place, with no Electron or network
 * imports.
 */

export interface ConnectAttempt {
  /** Monotonic id. Cancelling by id can't kill a newer attempt. */
  readonly id: number;
  /** Aborts the attempt's in-flight HTTP requests. */
  readonly controller: AbortController;
  cancelled: boolean;
}

let current: ConnectAttempt | null = null;
let nextId = 1;

/**
 * Open a new attempt, replacing (and cancelling) any previous one — a second
 * Connect press supersedes the first rather than racing it.
 */
export const beginAttempt = (): ConnectAttempt => {
  if (current && !current.cancelled) {
    cancelAttempt();
  }
  current = { id: nextId++, controller: new AbortController(), cancelled: false };
  return current;
};

/**
 * Cancel the in-flight attempt, if any. Returns the attempt that was
 * cancelled so the caller knows whether there was anything to stop (and can
 * decide whether to tear a tunnel down).
 */
export const cancelAttempt = (): ConnectAttempt | null => {
  if (!current || current.cancelled) return null;
  current.cancelled = true;
  // Abort AFTER marking cancelled: an abort listener that re-reads the flag
  // must never observe a live attempt whose requests are already dead.
  current.controller.abort();
  return current;
};

/**
 * True once this attempt has been cancelled, or superseded by a newer one.
 * Checked between steps, and above all immediately before the daemon connect.
 */
export const isCancelled = (attempt: ConnectAttempt): boolean =>
  attempt.cancelled || current === null || current.id !== attempt.id;

/**
 * Clear the attempt when it finishes normally, so a later cancel press is a
 * no-op instead of tearing down a connection the user wants to keep. Only the
 * attempt that owns the slot may clear it.
 */
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
