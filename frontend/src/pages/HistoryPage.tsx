import { useEffect, useMemo, useState } from "react";
import { Clock, Plus, RefreshCw, Search } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { RunRow, RunRowHeader } from "../components/dashboard/RunRows";
import { TopBar } from "../components/layout/TopBar";
import { Button } from "../components/ui/Button";
import { RunStatus, type RunStatusValue } from "../constants";
import { api } from "../lib/api";
import type { Run } from "../types";

function dateLabel(value: string) {
  const date = new Date(value);
  const today = new Date();
  const yesterday = new Date(Date.now() - 86_400_000);
  if (date.toDateString() === today.toDateString()) return "Today";
  if (date.toDateString() === yesterday.toDateString()) return "Yesterday";
  return date.toLocaleDateString(undefined, { month: "long", day: "numeric" });
}

export function HistoryPage() {
  const navigate = useNavigate();
  const [runs, setRuns] = useState<Run[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<RunStatusValue | "">("");
  const [error, setError] = useState("");

  useEffect(() => {
    void api<Run[]>("/api/runs?limit=500")
      .then(setRuns)
      .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Could not load runs"))
      .finally(() => setLoading(false));
  }, []);

  const refresh = async () => {
    setRefreshing(true);
    setError("");
    try { setRuns(await api<Run[]>("/api/runs?limit=500")); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Could not load runs"); }
    finally { setRefreshing(false); }
  };

  const groups = useMemo(() => {
    const query = search.trim().toLowerCase();
    const filtered = runs.filter((run) => (!status || run.status === status) && (!query || run.instructions.toLowerCase().includes(query) || run.url.toLowerCase().includes(query)));
    return filtered.reduce<Map<string, Run[]>>((result, run) => {
      const label = dateLabel(run.created_at);
      result.set(label, [...(result.get(label) ?? []), run]);
      return result;
    }, new Map());
  }, [runs, search, status]);

  return <>
    <TopBar title="History" />
    <main className="page-content history-page">
      <div className="filters">
        <label className="search-input"><Search size={14} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search by name or URL..." /></label>
        <select value={status} onChange={(event) => setStatus(event.target.value as RunStatusValue | "")}>
          <option value="">All statuses</option>
          <option value={RunStatus.PASSED}>Passed</option><option value={RunStatus.FAILED}>Failed</option>
          <option value={RunStatus.RUNNING}>Running</option><option value={RunStatus.QUEUED}>Pending</option><option value={RunStatus.CANCELLED}>Cancelled</option>
        </select>
        <Button variant="secondary" size="compact" onClick={() => void refresh()} disabled={refreshing} title="Refresh"><RefreshCw size={14} className={refreshing ? "spin" : ""} /></Button>
      </div>
      {loading ? <div className="run-skeletons">{Array.from({ length: 6 }).map((_, index) => <span key={index} />)}</div>
        : error ? <div className="empty-state error-state">{error}</div>
        : groups.size === 0 ? <div className="history-empty"><span><Clock size={24} /></span><p>No test runs yet</p><small>Run your first test from the dashboard</small><Button size="compact" onClick={() => navigate("/")}><Plus size={14} />New Test</Button></div>
        : <div className="runs-table"><RunRowHeader dated />{Array.from(groups).map(([label, items]) => <section className="date-group" key={label}><h2>{label}</h2>{items.map((run) => <RunRow key={run.id} run={run} dated />)}</section>)}</div>}
    </main>
  </>;
}
