import { useCallback, useEffect, useState } from "react";
import { listJobs, type Job } from "./api";
import { SubmitForm } from "./components/SubmitForm";
import { JobList } from "./components/JobList";
import { LogViewer } from "./components/LogViewer";
import "./App.css";

function App() {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setJobs(await listJobs());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to load jobs");
    }
  }, []);

  // Poll the job list every 2s so statuses (queued -> running -> done) update
  // on their own. Polling is the simplest fit for a REST list endpoint; a
  // production app might push list updates over a WebSocket or a second SSE
  // feed instead of re-fetching.
  useEffect(() => {
    refresh();
    const id = setInterval(refresh, 2000);
    return () => clearInterval(id);
  }, [refresh]);

  return (
    <div className="app">
      <header className="app-header">
        <h1>bottle</h1>
        <span className="subtitle">job platform</span>
      </header>

      <SubmitForm onSubmitted={refresh} />
      {error && <p className="error banner">{error}</p>}

      <div className="layout">
        <section className="pane">
          <h2>Jobs</h2>
          <JobList jobs={jobs} selectedId={selectedId} onSelect={setSelectedId} />
        </section>
        <section className="pane">
          <h2>Logs</h2>
          {selectedId ? (
            <LogViewer jobId={selectedId} />
          ) : (
            <p className="empty">Select a job to view its logs.</p>
          )}
        </section>
      </div>
    </div>
  );
}

export default App;
