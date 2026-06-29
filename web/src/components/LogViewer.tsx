import { useEffect, useRef, useState } from "react";
import { logStreamURL } from "../api";

interface Props {
  jobId: string;
}

export function LogViewer({ jobId }: Props) {
  const [lines, setLines] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // Reset state whenever the selected job changes.
    setLines([]);
    setDone(false);

    // EventSource is the browser's built-in SSE client — the reason we chose
    // SSE for the logs endpoint back on the server. It opens a GET stream,
    // delivers each "data:" frame to onmessage, and (for plain HTTP errors)
    // would auto-reconnect, which we suppress once the job is done.
    const es = new EventSource(logStreamURL(jobId));

    es.onmessage = (e) => {
      setLines((prev) => [...prev, e.data]);
    };

    // The server emits a custom "end" event when the job finishes.
    es.addEventListener("end", () => {
      setDone(true);
      es.close();
    });

    es.onerror = () => {
      // Stream ended or dropped; close to stop reconnect attempts.
      es.close();
    };

    // Cleanup on unmount or when jobId changes: always close the connection so
    // we don't leak an open stream per job we ever viewed.
    return () => es.close();
  }, [jobId]);

  // Keep the newest line in view as logs stream in.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [lines]);

  return (
    <div className="log-viewer">
      <div className="log-header">
        <code>{jobId}</code>
        <span className={done ? "tag done" : "tag live"}>
          {done ? "finished" : "streaming…"}
        </span>
      </div>
      <pre className="log-body">
        {lines.length === 0 ? "Waiting for output…" : lines.join("\n")}
        <div ref={bottomRef} />
      </pre>
    </div>
  );
}
