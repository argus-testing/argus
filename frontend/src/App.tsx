import { FormEvent, useEffect, useState } from "react";

type Status = "queued" | "running" | "passed" | "failed" | "cancelled";
type Event = { id: number; type: string; created_at: string; data: Record<string, unknown> };
type Finding = { severity: string; title: string; detail: string };
type Report = { verdict: string; summary: string; plan?: string; findings: Finding[]; recommendations: string[] };
type Run = { id: string; url: string; instructions: string; status: Status; created_at: string; updated_at: string; error: string | null; report: Report | null; events?: Event[] };

async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { headers: { "Content-Type": "application/json" }, ...init });
  if (!response.ok) throw new Error((await response.json()).detail || "Request failed");
  return response.json() as Promise<T>;
}

function navigate(path: string) {
  history.pushState({}, "", path);
  dispatchEvent(new PopStateEvent("popstate"));
}

function time(value: string) {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}

function StatusBadge({ status }: { status: Status }) {
  return <span className={`status ${status}`}><i />{status}</span>;
}

function NavLink({ to, icon, children }: { to: string; icon: string; children: React.ReactNode }) {
  return <a href={to} className={location.pathname === to ? "active" : ""} onClick={(event) => { event.preventDefault(); navigate(to); }}><span>{icon}</span>{children}</a>;
}

function Sidebar() {
  return <aside><div className="brand"><div className="eye">◉</div><strong>ARGUS</strong></div><nav><small>WORKSPACE</small><NavLink to="/" icon="⌂">Dashboard</NavLink><NavLink to="/history" icon="◷">Run history</NavLink><small>SYSTEM</small><NavLink to="/settings" icon="⚙">Settings</NavLink></nav><footer><span className="local-dot" /> Local workspace</footer></aside>;
}

function Layout({ children, title, subtitle }: { children: React.ReactNode; title: string; subtitle: string }) {
  return <div className="app"><Sidebar /><main><header><div><h1>{title}</h1><p>{subtitle}</p></div><span className="local"><i />LOCAL</span></header>{children}</main></div>;
}

function RunRows({ runs, empty = "No runs yet." }: { runs: Run[]; empty?: string }) {
  if (!runs.length) return <div className="empty">{empty}</div>;
  return <div className="run-list">{runs.map((run) => <button className="run-row" key={run.id} onClick={() => navigate(`/runs/${run.id}`)}><div className="run-icon">↗</div><div className="run-copy"><strong>{run.instructions}</strong><span>{run.url}</span></div><StatusBadge status={run.status} /><time>{time(run.created_at)}</time><b>›</b></button>)}</div>;
}

function Dashboard() {
  const [runs, setRuns] = useState<Run[]>([]);
  const [url, setUrl] = useState("");
  const [instructions, setInstructions] = useState("");
  const [error, setError] = useState("");
  useEffect(() => { void api<Run[]>("/api/runs?limit=5").then(setRuns); }, []);
  async function submit(event: FormEvent) {
    event.preventDefault(); setError("");
    try {
      const run = await api<Run>("/api/runs", { method: "POST", body: JSON.stringify({ url, instructions }) });
      navigate(`/runs/${run.id}`);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Could not start run"); }
  }
  const completed = runs.filter((run) => run.status === "passed").length;
  const failed = runs.filter((run) => run.status === "failed").length;
  return <Layout title="Dashboard" subtitle="Compose and monitor visual UI tests"><section className="composer"><div className="section-label">NEW VISUAL TEST</div><h2>What should Argus test?</h2><p>Describe the experience to verify. Argus will inspect, interact, and capture evidence.</p><form onSubmit={submit}><label>Target URL<input type="url" required placeholder="https://your-app.example" value={url} onChange={(event) => setUrl(event.target.value)} /></label><label>Test objective<textarea required rows={4} placeholder="Check the navigation, responsive layout, and key interactions…" value={instructions} onChange={(event) => setInstructions(event.target.value)} /></label>{error && <div className="error">{error}</div>}<button className="primary">▶ Start test run</button></form></section><div className="stats"><article><small>RECENT RUNS</small><strong>{runs.length}</strong><span>Saved locally</span></article><article><small>PASSED</small><strong className="green">{completed}</strong><span>Successful checks</span></article><article><small>FAILED</small><strong className="red">{failed}</strong><span>Needs attention</span></article></div><section className="panel"><div className="panel-title"><div><h2>Recent runs</h2><p>Your latest visual test sessions</p></div><button className="secondary" onClick={() => navigate("/history")}>View all</button></div><RunRows runs={runs} /></section></Layout>;
}

function HistoryPage() {
  const [runs, setRuns] = useState<Run[]>([]);
  useEffect(() => { void api<Run[]>("/api/runs?limit=500").then(setRuns); }, []);
  return <Layout title="Run history" subtitle="Every visual test and its evidence"><section className="panel history"><div className="panel-title"><div><h2>All runs</h2><p>{runs.length} saved in your local workspace</p></div><button className="primary small" onClick={() => navigate("/")}>＋ New run</button></div><RunRows runs={runs} empty="Start your first visual test from the dashboard." /></section></Layout>;
}

function LiveRun({ id }: { id: string }) {
  const [run, setRun] = useState<Run | null>(null);
  const [events, setEvents] = useState<Event[]>([]);
  useEffect(() => {
    const terminalStatuses: Status[] = ["passed", "failed", "cancelled"];
    const terminalEvents = ["run.completed", "run.failed", "run.cancelled"];
    const protocol = location.protocol === "https:" ? "wss" : "ws";
    let stopped = false;
    let terminal = false;
    let attempts = 0;
    let socket: WebSocket | null = null;
    let reconnectTimer: number | undefined;

    async function refresh() {
      const value = await api<Run>(`/api/runs/${id}`);
      if (stopped) return true;
      setRun(value);
      setEvents(value.events || []);
      terminal = terminalStatuses.includes(value.status);
      return terminal;
    }

    function connect() {
      if (stopped || terminal) return;
      socket = new WebSocket(`${protocol}://${location.host}/ws/runs/${id}`);
      socket.onmessage = (message) => {
        const event = JSON.parse(message.data) as Event;
        setEvents((current) => current.some((item) => item.id === event.id) ? current : [...current, event]);
        if (event.type === "run.started" || terminalEvents.includes(event.type)) {
          terminal = terminalEvents.includes(event.type);
          void refresh().catch(() => false);
        }
      };
      socket.onerror = () => socket?.close();
      socket.onclose = () => {
        socket = null;
        if (stopped || terminal || attempts >= 5) return;
        void refresh().catch(() => false).then((finished) => {
          if (finished || stopped) return;
          const delay = Math.min(500 * 2 ** attempts, 8000);
          attempts += 1;
          reconnectTimer = window.setTimeout(connect, delay);
        });
      };
    }

    void refresh().catch(() => false);
    connect();
    return () => {
      stopped = true;
      if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, [id]);
  if (!run) return <Layout title="Live session" subtitle="Loading run…"><div className="loading" /></Layout>;
  const screenshots = events.filter((event) => event.type === "browser.screenshot");
  return <Layout title="Live session" subtitle={run.url}><section className="session-head"><div><StatusBadge status={run.status} /><h2>{run.instructions}</h2><span>Started {time(run.created_at)}</span></div><div>{run.status === "running" && <button className="danger" onClick={() => void api(`/api/runs/${id}/cancel`, { method: "POST" }).then(() => api<Run>(`/api/runs/${id}`).then(setRun))}>Stop run</button>}{run.report && <button className="primary" onClick={() => navigate(`/runs/${id}/report`)}>Open report →</button>}</div></section>{run.error && <div className="failure"><strong>Run failed</strong><p>{run.error}</p></div>}<div className="session-grid"><section className="panel timeline"><div className="panel-title"><div><h2>Agent timeline</h2><p>Persisted events update in real time</p></div><span className="event-count">{events.length}</span></div>{events.map((event) => <div className="event" key={event.id}><span className="event-mark" /><div><strong>{event.type.replaceAll(".", " ")}</strong><pre>{Object.keys(event.data).length ? JSON.stringify(event.data, null, 2) : "Event recorded"}</pre></div><time>{new Date(event.created_at).toLocaleTimeString()}</time></div>)}</section><section className="panel evidence"><div className="panel-title"><div><h2>Evidence</h2><p>Browser screenshots</p></div></div>{screenshots.length ? screenshots.map((event) => <figure key={event.id}><img src={String(event.data.path)} alt={String(event.data.label)} /><figcaption>{String(event.data.label)}</figcaption></figure>) : <div className="empty">Screenshots will appear here.</div>}</section></div></Layout>;
}

function ReportPage({ id }: { id: string }) {
  const [run, setRun] = useState<Run | null>(null);
  useEffect(() => { void api<Run>(`/api/runs/${id}`).then(setRun); }, [id]);
  if (!run) return <Layout title="Test report" subtitle="Loading…"><div className="loading" /></Layout>;
  if (!run.report) return <Layout title="Test report" subtitle={run.url}><section className="panel empty">This run has no final report yet.</section></Layout>;
  const report = run.report;
  const shots = (run.events || []).filter((event) => event.type === "browser.screenshot");
  return <Layout title="Test report" subtitle={run.url}><section className={`verdict ${report.verdict}`}><small>FINAL VERDICT</small><h2>{report.verdict}</h2><p>{report.summary}</p><button className="secondary" onClick={() => navigate(`/runs/${id}`)}>View session timeline</button></section><div className="report-grid"><section className="panel report-section"><h2>Test plan</h2><p className="preserve">{report.plan}</p></section><section className="panel report-section"><h2>Findings</h2>{report.findings.length ? report.findings.map((finding, index) => <article className="finding" key={index}><span className={`severity ${finding.severity}`}>{finding.severity}</span><div><strong>{finding.title}</strong><p>{finding.detail}</p></div></article>) : <div className="empty">No findings recorded.</div>}</section><section className="panel report-section"><h2>Recommendations</h2>{report.recommendations.length ? <ol>{report.recommendations.map((item) => <li key={item}>{item}</li>)}</ol> : <div className="empty">No recommendations.</div>}</section><section className="panel report-section"><h2>Evidence gallery</h2><div className="gallery">{shots.map((shot) => <img key={shot.id} src={String(shot.data.path)} alt={String(shot.data.label)} />)}</div></section></div></Layout>;
}

function SettingsPage() {
  const [settings, setSettings] = useState<{ gemini_configured: boolean; model: string } | null>(null);
  useEffect(() => { void api<{ gemini_configured: boolean; model: string }>("/api/settings").then(setSettings); }, []);
  return <Layout title="Settings" subtitle="Local runtime configuration"><section className="panel settings"><div className="setting-row"><div><h2>Gemini provider</h2><p>Argus reads the API key from your environment. Secrets are never stored by this app.</p></div><span className={`configured ${settings?.gemini_configured ? "yes" : "no"}`}>{settings?.gemini_configured ? "● Configured" : "○ Not configured"}</span></div><div className="code"><small>ENVIRONMENT SETUP</small><code>GEMINI_API_KEY=your-api-key</code><p>Add this value to <b>.env</b> or your shell, then restart Argus.</p></div><div className="setting-row"><div><h3>Model</h3><p>Override with <code>GEMINI_MODEL</code>.</p></div><code>{settings?.model || "…"}</code></div><div className="notice">Target-site credentials are not stored or managed in this release.</div></section></Layout>;
}

export default function App() {
  const [path, setPath] = useState(location.pathname);
  useEffect(() => { const update = () => setPath(location.pathname); addEventListener("popstate", update); return () => removeEventListener("popstate", update); }, []);
  const report = path.match(/^\/runs\/([^/]+)\/report$/);
  const run = path.match(/^\/runs\/([^/]+)$/);
  if (report) return <ReportPage id={report[1]} />;
  if (run) return <LiveRun id={run[1]} />;
  if (path === "/history") return <HistoryPage />;
  if (path === "/settings") return <SettingsPage />;
  return <Dashboard />;
}
