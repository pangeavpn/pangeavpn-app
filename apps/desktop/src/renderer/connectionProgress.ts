const MESSAGE_DELAYS_MS = [3000, 8000, 15000] as const;

type Timer = ReturnType<typeof setTimeout> | unknown;
type ScheduleTimer = (callback: () => void, delayMs: number) => Timer;
type ClearTimer = (timer: Timer) => void;

/** Schedules reassurance copy at absolute offsets and returns one cleanup function. */
export function scheduleConnectionMessages(
  messages: readonly [string, string, string],
  showMessage: (message: string) => void,
  schedule: ScheduleTimer = (callback, delayMs) => setTimeout(callback, delayMs),
  cancel: ClearTimer = (timer) => clearTimeout(timer as ReturnType<typeof setTimeout>)
): () => void {
  const timers = MESSAGE_DELAYS_MS.map((delayMs, index) =>
    schedule(() => showMessage(messages[index]), delayMs)
  );
  return () => timers.forEach(cancel);
}
