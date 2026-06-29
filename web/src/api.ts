// Typed client for the bottle API. Mirrors the Go client: it centralizes every
// fetch call and the response shapes, so components deal in typed Job objects,
// not raw HTTP.

export type JobStatus = "queued" | "running" | "succeeded" | "failed";

// Job mirrors the JSON the Go server emits (internal/job.Job). Optional fields
// use `?` to match the server's omitempty: a queued job has no exit_code yet.
export interface Job {
  id: string;
  command: string;
  status: JobStatus;
  exit_code?: number;
  created_at: string;
  started_at?: string;
  finished_at?: string;
}

// The API base URL is configurable via a Vite env var (VITE_API_URL) so the
// same build can point at different backends; it defaults to the local server.
const env = import.meta.env as Record<string, string | undefined>;
const API_BASE = env.VITE_API_URL ?? "http://localhost:8080";

export async function listJobs(): Promise<Job[]> {
  const res = await fetch(`${API_BASE}/jobs`);
  if (!res.ok) {
    throw new Error(`failed to list jobs (status ${res.status})`);
  }
  return res.json();
}

export async function submitJob(command: string): Promise<Job> {
  const res = await fetch(`${API_BASE}/jobs`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ command }),
  });
  if (!res.ok) {
    // Surface the server's {"error": "..."} message when present.
    const body = (await res.json().catch(() => ({}))) as { error?: string };
    throw new Error(body.error ?? `submit failed (status ${res.status})`);
  }
  return res.json();
}

// logStreamURL returns the SSE endpoint for a job's logs. The browser's
// EventSource consumes this directly.
export function logStreamURL(id: string): string {
  return `${API_BASE}/jobs/${id}/logs`;
}
