import { ChevronLeft, Square, X } from "lucide-react";
import type { RunStatusValue } from "../../constants";
import { Badge } from "../ui/Badge";

export function SessionHeader({ status, reconnecting, stopping, onStop, onClose }: {
  status: RunStatusValue;
  reconnecting: boolean;
  stopping: boolean;
  onStop?: () => void;
  onClose: () => void;
}) {
  return <header className="session-header">
    <button type="button" className="back-button" onClick={onClose}><ChevronLeft size={16} /><span>Back</span></button>
    <Badge status={status} />
    <div className="session-actions">
      {onStop && <button type="button" className="stop-button" onClick={onStop} disabled={stopping}><Square size={12} fill="currentColor" />{stopping ? "Stopping..." : "Stop"}</button>}
      {reconnecting && <span className="reconnecting"><i />Reconnecting…</span>}
      <button type="button" className="icon-button" onClick={onClose} title="Close"><X size={16} /></button>
    </div>
  </header>;
}
