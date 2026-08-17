import { AlertTriangle, Check, ExternalLink, Image, Loader, RotateCcw, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { EventType, RunStatus, TERMINAL_EVENTS, TERMINAL_STATUSES } from "../../constants";
import { api, formatDate } from "../../lib/api";
import type { Run, RunEvent } from "../../types";
import { Button } from "../ui/Button";
import { Spinner } from "../ui/Spinner";
import { EvidenceGallery } from "./EvidenceGallery";
import { SessionHeader } from "./SessionHeader";
import { Timeline } from "./Timeline";

export function SessionView() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [run, setRun] = useState<Run | null>(null);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [loadingError, setLoadingError] = useState("");
  const [reconnecting, setReconnecting] = useState(false);
  const [stopping, setStopping] = useState(false);

  useEffect(() => {
    const protocol = location.protocol === "https:" ? "wss" : "ws";
    let stopped = false;
    let terminal = false;
    let attempts = 0;
    let socket: WebSocket | null = null;
    let reconnectTimer: number | undefined;

    const refresh = async () => {
      const value = await api<Run>(`/api/runs/${id}`);
      if (stopped) return false;
      setRun(value);
      setEvents(value.events ?? []);
      terminal = TERMINAL_STATUSES.includes(value.status);
      return terminal;
    };

    const connect = () => {
      if (stopped || terminal) return;
      socket = new WebSocket(`${protocol}://${location.host}/ws/runs/${id}`);
      socket.onopen = () => { attempts = 0; setReconnecting(false); };
      socket.onmessage = (message) => {
        const event = JSON.parse(message.data) as RunEvent;
        setEvents((current) => current.some((item) => item.id === event.id) ? current : [...current, event]);
        if (event.type === EventType.RUN_STARTED || TERMINAL_EVENTS.includes(event.type)) {
          terminal = TERMINAL_EVENTS.includes(event.type);
          void refresh();
        }
      };
      socket.onerror = () => socket?.close();
      socket.onclose = () => {
        socket = null;
        if (stopped || terminal || attempts >= 5) return;
        setReconnecting(true);
        reconnectTimer = window.setTimeout(() => { attempts += 1; connect(); }, Math.min(500 * 2 ** attempts, 8000));
      };
    };

    void refresh().then((finished) => { if (!finished) connect(); }).catch((reason: unknown) => {
      if (!stopped) setLoadingError(reason instanceof Error ? reason.message : "Could not load run");
    });
    return () => {
      stopped = true;
      if (reconnectTimer !== undefined) clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, [id]);

  const screenshots = useMemo(() => events.filter((event) => event.type === EventType.BROWSER_SCREENSHOT), [events]);
  const isRunning = run?.status === RunStatus.RUNNING || run?.status === RunStatus.QUEUED;

  const stop = async () => {
    if (!run) return;
    setStopping(true);
    try { setRun(await api<Run>(`/api/runs/${run.id}/cancel`, { method: "POST" })); }
    finally { setStopping(false); }
  };

  const close = () => navigate("/");

  return <div className="session-overlay">
    <div className="session-window">
      {run && <SessionHeader status={run.status} reconnecting={reconnecting} stopping={stopping} onStop={isRunning ? () => void stop() : undefined} onClose={close} />}
      {!run ? <div className="session-loading">{loadingError ? <><AlertTriangle size={20} /><p>{loadingError}</p><Button variant="secondary" onClick={close}>Back to dashboard</Button></> : <Spinner label="Loading session" />}</div>
        : <div className={`session-console ${isRunning ? "live" : "done"}`}>
          <section className="spec-block">
            <div className="eyebrow"><span>Specification</span><i /></div>
            <div className="spec-copy"><h1>{run.instructions}</h1><div className="spec-meta"><span>local</span><a href={run.url} target="_blank" rel="noreferrer">{run.url.replace(/^https?:\/\//, "")}<ExternalLink size={10} /></a><time>· {formatDate(run.created_at)}</time></div></div>
          </section>

          {isRunning ? <>
            <div className="progress-strip"><span>{run.status === RunStatus.QUEUED ? "Preparing run" : "Visual test in progress"}</span><i /><small>{events.length} events</small></div>
            <div className="live-console-grid">
              <section className="browser-stage">
                {screenshots.length ? <img src={String(screenshots[screenshots.length - 1].data.path)} alt="Latest browser capture" /> : <div className="thinking-stage"><span><Loader size={24} className="spin" /></span><strong>{run.status === RunStatus.QUEUED ? "Preparing the run" : "Inspecting the experience"}</strong><p>The browser view will appear when evidence is captured.</p></div>}
              </section>
              <aside className="agent-rail"><div className="rail-heading"><span>Agent</span><i /><em><b />running</em></div><Timeline events={events} running /></aside>
            </div>
          </> : <Report run={run} events={events} />}
        </div>}
    </div>
  </div>;
}

function Report({ run, events }: { run: Run; events: RunEvent[] }) {
  const report = run.report;
  const cancelled = run.status === RunStatus.CANCELLED;
  return <div className="report-document">
    {run.error && <section className="failure-card"><AlertTriangle size={16} /><div><h2>{cancelled ? "Run cancelled" : "Run failed"}</h2><p>{run.error}</p></div></section>}
    {report && <section className="finding-summary"><span className="eyebrow">Finding</span><div className={`verdict-line ${report.verdict}`}><i />{report.verdict === "passed" ? <Check size={24} /> : <X size={24} />}<h2>{report.verdict === "inconclusive" ? "Inconclusive" : report.verdict === "passed" ? "Passed" : "Failed"}</h2></div><p>{report.summary}</p></section>}
    <section className="report-section"><div className="eyebrow"><span>Evidence</span><i /></div><EvidenceGallery events={events} /></section>
    {report?.findings.length ? <section className="report-section"><div className="eyebrow"><span>Findings</span><i /></div><div className="findings-list">{report.findings.map((finding, index) => <article key={`${finding.title}-${index}`}><span className={`severity ${finding.severity.toLowerCase()}`}>{finding.severity}</span><div><h3>{finding.title}</h3><p>{finding.detail}</p></div></article>)}</div></section> : null}
    {report?.recommendations.length ? <section className="report-section"><div className="eyebrow"><span>Recommendations</span><i /></div><ol className="recommendations">{report.recommendations.map((item) => <li key={item}>{item}</li>)}</ol></section> : null}
    <details className="method"><summary><RotateCcw size={13} />Method — what the agent did</summary><div>{report?.plan && <pre>{report.plan}</pre>}<Timeline events={events} running={false} compact /></div></details>
    {!report && !run.error && <div className="empty-state"><Image size={20} /><p>This run has no final report.</p></div>}
  </div>;
}
