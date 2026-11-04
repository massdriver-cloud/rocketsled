import { marked } from "marked";
import { useMemo } from "react";

type Tone = "ok" | "warn" | "bad" | "neutral";

const tones: Record<string, Tone> = {
  running: "ok",
  succeeded: "ok",
  ready: "ok",
  PROVISIONED: "ok",
  deploying: "warn",
  installing: "warn",
  queued: "warn",
  needs_credentials: "warn",
  INITIALIZED: "neutral",
  failed: "bad",
  FAILED: "bad",
  decommissioning: "neutral",
  DECOMMISSIONED: "neutral",
  new: "neutral",
};

const labels: Record<string, string> = {
  needs_credentials: "needs credentials",
  PROVISIONED: "provisioned",
  INITIALIZED: "not deployed",
  FAILED: "failed",
  DECOMMISSIONED: "removed",
};

export function Pill({ status, live }: { status: string; live?: boolean }) {
  const tone = tones[status] ?? "neutral";
  const isLive = live ?? (status === "deploying" || status === "installing" || status === "queued" || status === "decommissioning");
  return <span className={`pill ${tone === "neutral" ? "" : `pill--${tone}`} ${isLive ? "pill--live" : ""}`}>{labels[status] ?? status}</span>;
}

export function Notice({ tone = "info", children }: { tone?: "info" | "bad" | "warn" | "ok"; children: React.ReactNode }) {
  return (
    <div className={`notice ${tone === "info" ? "" : `notice--${tone}`}`} role={tone === "bad" ? "alert" : undefined}>
      {children}
    </div>
  );
}

export function Loading({ label = "Loading" }: { label?: string }) {
  return (
    <div className="loading" role="status">
      {label}…
    </div>
  );
}

export function Markdown({ source }: { source: string }) {
  const html = useMemo(() => marked.parse(source, { async: false }) as string, [source]);
  return <div className="prose" dangerouslySetInnerHTML={{ __html: html }} />;
}

export function Steps({ current }: { current: 1 | 2 | 3 }) {
  const items = ["Choose a platform", "Install", "Connect your cloud"];
  return (
    <nav className="steps" aria-label="Setup progress">
      {items.map((label, i) => {
        const n = i + 1;
        const cls = n < current ? "done" : n === current ? "current" : "";
        return (
          <span key={label} className={cls}>
            <i>{n < current ? "✓" : n}</i>
            {label}
          </span>
        );
      })}
    </nav>
  );
}
