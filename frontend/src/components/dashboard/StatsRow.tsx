import { Activity, CheckCircle, Loader, XCircle } from "lucide-react";

export function StatsRow({ total, passed, failed, running }: { total: number; passed: number; failed: number; running: number }) {
  const items = [
    { label: "Total", value: total, icon: Activity, tone: "" },
    { label: "Passed", value: passed, icon: CheckCircle, tone: "success" },
    { label: "Failed", value: failed, icon: XCircle, tone: "error" },
    { label: "Running", value: running, icon: Loader, tone: "warning" },
  ];
  return <div className="stats-row">{items.map(({ label, value, icon: Icon, tone }) => <div key={label}>
    <Icon size={15} className={tone} /><span>{label}</span><strong>{value}</strong>
  </div>)}</div>;
}
