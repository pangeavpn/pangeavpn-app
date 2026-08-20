const MESSAGE_DELAYS_MS = [3000, 8000, 15000] as const;

type ScheduleTimer<Timer> = (callback: () => void, delayMs: number) => Timer;
type ClearTimer<Timer> = (timer: Timer) => void;

/** Schedules reassurance copy at absolute offsets and returns one cleanup function.
 *  `Timer` is inferred from `schedule`, so `cancel` is checked against the same
 *  handle type instead of collapsing to `unknown`. */
export function scheduleConnectionMessages<Timer = ReturnType<typeof setTimeout>>(
  messages: readonly [string, string, string],
  showMessage: (message: string) => void,
  schedule: ScheduleTimer<Timer> = (callback, delayMs) => setTimeout(callback, delayMs) as Timer,
  cancel: ClearTimer<Timer> = (timer) => clearTimeout(timer as ReturnType<typeof setTimeout>)
): () => void {
  const timers = MESSAGE_DELAYS_MS.map((delayMs, index) =>
    schedule(() => showMessage(messages[index]), delayMs)
  );
  return () => timers.forEach(cancel);
}
