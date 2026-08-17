import { Brain, Camera, Check, Compass, Eye, Loader, MousePointer2, X } from "lucide-react";
import { EventType } from "../../constants";
import { formatTime } from "../../lib/api";
import type { RunEvent } from "../../types";

function eventMeta(type: string) {
  if (type === EventType.PLAN_COMPLETED) return { label: "Planning", icon: Compass };
  if (type === EventType.BROWSER_ACTION) return { label: "Browser action", icon: MousePointer2 };
  if (type === EventType.BROWSER_OBSERVATION) return { label: "Page observation", icon: Eye };
  if (type === EventType.BROWSER_SCREENSHOT) return { label: "Evidence captured", icon: Camera };
  if (type === EventType.RUN_STARTED) return { label: "Agent started", icon: Brain };
  if (type === EventType.RUN_COMPLETED) return { label: "Run completed", icon: Check };
  if (type === EventType.RUN_FAILED || type === EventType.RUN_CANCELLED) return { label: type === EventType.RUN_FAILED ? "Run failed" : "Run cancelled", icon: X };
  return { label: type.replaceAll(".", " "), icon: Loader };
}

function eventDetail(event: RunEvent) {
  const { data } = event;
  if (event.type === EventType.PLAN_COMPLETED) return String(data.plan ?? "Plan prepared");
  if (event.type === EventType.BROWSER_ACTION) {
    const args = data.arguments && typeof data.arguments === "object" ? ` ${JSON.stringify(data.arguments)}` : "";
    return `${String(data.tool ?? "Action")}${args}`;
  }
  if (event.type === EventType.BROWSER_OBSERVATION) return `${String(data.tool ?? "Inspection")} completed`;
  if (event.type === EventType.BROWSER_SCREENSHOT) return String(data.label ?? "Screenshot saved");
  if (event.type === EventType.RUN_FAILED) return String(data.message ?? "The run could not complete");
  return "Event recorded";
}

export function Timeline({ events, running, compact = false }: { events: RunEvent[]; running: boolean; compact?: boolean }) {
  if (!events.length) return <div className="timeline-empty">{running && <span className="live-dot" />}<p>{running ? "Waiting for the agent to begin…" : "No activity was recorded for this run."}</p></div>;
  return <ol className={`timeline ${compact ? "compact" : ""}`}>
    {events.map((event, index) => {
      const meta = eventMeta(event.type);
      const Icon = meta.icon;
      const isLastLive = running && index === events.length - 1;
      const failed = event.type === EventType.RUN_FAILED || event.type === EventType.RUN_CANCELLED;
      return <li key={event.id}>
        <span className={`timeline-icon ${isLastLive ? "active" : ""} ${failed ? "failed" : ""}`}><Icon size={14} />{isLastLive && <i />}</span>
        <div><header><strong>{meta.label}</strong><time>{formatTime(event.created_at)}</time></header><p>{eventDetail(event)}</p></div>
      </li>;
    })}
    {running && <li><span className="timeline-icon active"><Loader size={14} className="spin" /></span><div><header><strong>Working</strong></header><p>Argus is inspecting the experience…</p></div></li>}
  </ol>;
}
