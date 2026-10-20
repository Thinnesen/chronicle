import { Download } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { useLogStream } from "../lib/useLogStream";
import "./LogView.css";

// LogView shows the log of one node. While the node runs, new lines arrive live.
export function LogView({ namespace, workflow, node, live }: { namespace: string; workflow: string; node: string; live: boolean }) {
  const { lines, dropped, error, notice, done, archived, reconnecting } = useLogStream(namespace, workflow, node, live);
  const [filter, setFilter] = useState("");
  const box = useRef<HTMLPreElement>(null);
  const atBottom = useRef(true);
  useEffect(() => {
    const el = box.current;
    if (el && atBottom.current) el.scrollTop = el.scrollHeight;
  }, [lines]);
  const shown = filter ? lines.filter((l) => l.text.toLowerCase().includes(filter.toLowerCase())) : lines;
  return (
    <div className="logview">
      <div className="logview-bar">
        <label className="logview-filter">
          <span className="visually-hidden">Filter the log</span>
          <input type="search" placeholder="Filter lines" value={filter} onChange={(e) => setFilter(e.target.value)} />
        </label>
        <span className="quiet" aria-live="polite">
          {live && !done ? "Following new lines" : `${lines.length} lines`}
          {dropped > 0 && `, the first ${dropped} are not shown`}
        </span>
        <button type="button" className="button button-ghost" disabled={lines.length === 0} onClick={() => download(`${workflow}-${node}.log`, lines.map((l) => l.text))}>
          <Download aria-hidden="true" />
          Download
        </button>
      </div>
      {archived && <p className="logview-note">The Pod was deleted. This log comes from the log archive.</p>}
      {error && <p role="alert">Could not read the log: {error}</p>}
      {notice && <p className="quiet">{notice}</p>}
      {reconnecting && <p className="logview-note" role="status">The log stream stopped. Connecting again…</p>}
      <pre
        ref={box}
        className="logview-lines"
        tabIndex={0}
        aria-label="Log"
        onScroll={(e) => {
          const el = e.currentTarget;
          atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
        }}
      >
        {shown.length === 0 && done && !error && !notice && <span className="quiet">The log is empty.</span>}
        {shown.map((l) => (
          <span key={l.n} className="logline">
            <span className="logline-n" aria-hidden="true">
              {l.n}
            </span>
            <span className="logline-text">{l.text}</span>
          </span>
        ))}
      </pre>
    </div>
  );
}

// download saves the lines of the log as a text file.
function download(name: string, lines: string[]) {
  const url = URL.createObjectURL(new Blob([lines.join("\n") + "\n"], { type: "text/plain" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
