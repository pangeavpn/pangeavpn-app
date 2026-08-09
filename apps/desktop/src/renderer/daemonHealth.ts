export interface DaemonHealth {
  consecutiveFailures: number;
  recoveryRequired: boolean;
}

const RECOVERY_FAILURE_THRESHOLD = 3;

export function initialDaemonHealth(): DaemonHealth {
  return { consecutiveFailures: 0, recoveryRequired: false };
}

export function daemonHealthAfterFailure(health: DaemonHealth): DaemonHealth {
  const consecutiveFailures = health.consecutiveFailures + 1;
  return {
    consecutiveFailures,
    recoveryRequired: health.recoveryRequired || consecutiveFailures >= RECOVERY_FAILURE_THRESHOLD
  };
}

export function daemonHealthAfterSuccess(_health: DaemonHealth): DaemonHealth {
  return initialDaemonHealth();
}
