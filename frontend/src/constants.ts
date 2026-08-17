export const RunStatus = {
  QUEUED: "queued",
  RUNNING: "running",
  PASSED: "passed",
  FAILED: "failed",
  CANCELLED: "cancelled",
} as const;

export type RunStatusValue = (typeof RunStatus)[keyof typeof RunStatus];

export const TERMINAL_STATUSES: RunStatusValue[] = [
  RunStatus.PASSED,
  RunStatus.FAILED,
  RunStatus.CANCELLED,
];

export const EventType = {
  RUN_QUEUED: "run.queued",
  RUN_STARTED: "run.started",
  RUN_COMPLETED: "run.completed",
  RUN_FAILED: "run.failed",
  RUN_CANCELLED: "run.cancelled",
  PLAN_COMPLETED: "plan.completed",
  BROWSER_ACTION: "browser.action",
  BROWSER_OBSERVATION: "browser.observation",
  BROWSER_SCREENSHOT: "browser.screenshot",
} as const;

export const TERMINAL_EVENTS: string[] = [
  EventType.RUN_COMPLETED,
  EventType.RUN_FAILED,
  EventType.RUN_CANCELLED,
];
