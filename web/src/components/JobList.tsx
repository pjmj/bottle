import type { Job } from "../api";

interface Props {
  jobs: Job[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}

export function JobList({ jobs, selectedId, onSelect }: Props) {
  if (jobs.length === 0) {
    return <p className="empty">No jobs yet — submit one above.</p>;
  }

  return (
    <table className="job-list">
      <thead>
        <tr>
          <th>Status</th>
          <th>Command</th>
          <th>Exit</th>
          <th>Created</th>
        </tr>
      </thead>
      <tbody>
        {jobs.map((j) => (
          <tr
            key={j.id}
            className={j.id === selectedId ? "selected" : ""}
            onClick={() => onSelect(j.id)}
          >
            <td>
              <span className={`badge ${j.status}`}>{j.status}</span>
            </td>
            <td className="cmd" title={j.command}>
              {j.command}
            </td>
            {/* exit_code is optional: show "-" until the job has finished,
                mirroring the server's nil-vs-zero distinction. */}
            <td>{j.exit_code ?? "-"}</td>
            <td>{new Date(j.created_at).toLocaleTimeString()}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
