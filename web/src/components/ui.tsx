import { Link } from "react-router-dom";
import type { DeployStatus, InstanceState, PackageStatus } from "../types";

export function Wordmark({ hero = false, to = "/" }: { hero?: boolean; to?: string }) {
  return (
    <Link to={to} className={`wordmark ${hero ? "wordmark--hero" : ""}`} aria-label="Rocketsled home">
      <span className="display wordmark__text">Rocketsled</span>
      <svg className="wordmark__rail" viewBox="0 0 100 10" preserveAspectRatio="none" aria-hidden="true">
        <rect x="0" y="0" width="100" height="2" fill="currentColor" />
        <rect x="0" y="8" width="100" height="2" fill="currentColor" />
        <rect x="0" y="2" width="1.2" height="6" fill="currentColor" opacity="0.5" />
        <rect x="20" y="2" width="1.2" height="6" fill="currentColor" opacity="0.5" />
        <rect x="40" y="2" width="1.2" height="6" fill="currentColor" opacity="0.5" />
        <rect x="60" y="2" width="1.2" height="6" fill="currentColor" opacity="0.5" />
        <rect x="80" y="2" width="1.2" height="6" fill="currentColor" opacity="0.5" />
        <rect x="98.8" y="2" width="1.2" height="6" fill="currentColor" opacity="0.5" />
      </svg>
    </Link>
  );
}

export function Eyebrow({ children, red = false }: { children: React.ReactNode; red?: boolean }) {
  return <span className={`eyebrow ${red ? "eyebrow--red" : ""}`}>{children}</span>;
}

type Tone = "go" | "flare" | "sled" | "hold";

function toneFor(status: string): { tone: Tone; live: boolean } {
  switch (status) {
    case "running":
    case "succeeded":
    case "PROVISIONED":
      return { tone: "go", live: status === "running" && false };
    case "launching":
    case "queued":
    case "building":
    case "INITIALIZED":
      return { tone: "flare", live: status !== "INITIALIZED" };
    case "failed":
    case "FAILED":
      return { tone: "sled", live: false };
    default:
      return { tone: "hold", live: status === "decommissioning" };
  }
}

export function Pill({ status, live }: { status: InstanceState | DeployStatus | PackageStatus; live?: boolean }) {
  const t = toneFor(status);
  const isLive = live ?? (t.live || status === "running");
  return <span className={`pill pill--${t.tone} ${isLive && status !== "running" ? "pill--live" : ""}`}>{status}</span>;
}

export function Marker({ status, live }: { status: string; live?: boolean }) {
  return <span className={`rail__marker rail__marker--${status} ${live ? "rail__marker--live" : ""}`} aria-hidden="true" />;
}

export function Sha({ sha }: { sha: string }) {
  return (
    <code title={sha} className="mono">
      {sha.slice(0, 7)}
    </code>
  );
}

export function Notice({
  tone = "info",
  children,
}: {
  tone?: "info" | "error" | "warn" | "ok";
  children: React.ReactNode;
}) {
  return (
    <div className={`notice ${tone === "info" ? "" : `notice--${tone}`}`} role={tone === "error" ? "alert" : undefined}>
      {children}
    </div>
  );
}

export function Loading({ label = "Reading telemetry" }: { label?: string }) {
  return (
    <div className="loading" role="status">
      {label}…
    </div>
  );
}

export function Empty({ title, children }: { title: string; children?: React.ReactNode }) {
  return (
    <div className="empty">
      <Eyebrow>Track clear</Eyebrow>
      <h3>{title}</h3>
      {children}
    </div>
  );
}
