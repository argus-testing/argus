import { RunStatus, type RunStatusValue } from "../../constants";

const labels: Record<RunStatusValue, string> = {
  [RunStatus.QUEUED]: "Pending",
  [RunStatus.RUNNING]: "Running",
  [RunStatus.PASSED]: "Pass",
  [RunStatus.FAILED]: "Fail",
  [RunStatus.CANCELLED]: "Cancelled",
};

export function Badge({ status }: { status: RunStatusValue }) {
  return <span className={`badge badge-${status}`}><span />{labels[status]}</span>;
}
