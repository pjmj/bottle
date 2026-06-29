import { useState, type FormEvent } from "react";
import { submitJob } from "../api";

interface Props {
  // Called after a successful submit so the parent can refresh the job list.
  onSubmitted: () => void;
}

export function SubmitForm({ onSubmitted }: Props) {
  const [command, setCommand] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!command.trim()) return;

    setBusy(true);
    setError(null);
    try {
      await submitJob(command);
      setCommand(""); // clear on success
      onSubmitted();
    } catch (err) {
      setError(err instanceof Error ? err.message : "submit failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="submit-form" onSubmit={handleSubmit}>
      <input
        value={command}
        onChange={(e) => setCommand(e.target.value)}
        placeholder="command to run, e.g. echo hello"
        aria-label="command"
      />
      <button type="submit" disabled={busy}>
        {busy ? "Submitting…" : "Submit job"}
      </button>
      {error && <span className="error">{error}</span>}
    </form>
  );
}
