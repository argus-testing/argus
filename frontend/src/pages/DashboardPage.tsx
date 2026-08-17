import { useEffect, useState } from "react";
import { ArrowRight } from "lucide-react";
import { Link } from "react-router-dom";
import { NewTestInput } from "../components/dashboard/NewTestInput";
import { RunRow, RunRowHeader } from "../components/dashboard/RunRows";
import { StatsRow } from "../components/dashboard/StatsRow";
import { TopBar } from "../components/layout/TopBar";
import { RunStatus } from "../constants";
import { api } from "../lib/api";
import type { Run } from "../types";

export function DashboardPage() {
  const [runs, setRuns] = useState<Run[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    void api<Run[]>("/api/runs?limit=500")
      .then(setRuns)
      .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Could not load runs"))
      .finally(() => setLoading(false));
  }, []);

  return <>
    <TopBar title="Dashboard" showNewTest />
    <main className="page-content dashboard-page">
      <StatsRow
        total={runs.length}
        passed={runs.filter((run) => run.status === RunStatus.PASSED).length}
        failed={runs.filter((run) => run.status === RunStatus.FAILED).length}
        running={runs.filter((run) => run.status === RunStatus.RUNNING || run.status === RunStatus.QUEUED).length}
      />
      <NewTestInput />
      <section className="runs-section">
        <div className="section-heading"><h2>Recent runs</h2><Link to="/history">All runs <ArrowRight size={11} /></Link></div>
        {loading ? <div className="run-skeletons">{Array.from({ length: 5 }).map((_, index) => <span key={index} />)}</div>
          : error ? <div className="empty-state error-state">{error}</div>
          : runs.length === 0 ? <div className="empty-state"><p>No test runs yet</p><span>Describe a test above to get started</span></div>
          : <div className="runs-table"><RunRowHeader />{runs.slice(0, 6).map((run) => <RunRow key={run.id} run={run} />)}</div>}
      </section>
    </main>
  </>;
}
