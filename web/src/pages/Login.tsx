import { useEffect, useState } from "react";
import { useLocation } from "react-router-dom";
import { api, loginUrl } from "../api";
import { Notice } from "../components/ui";

const labels: Record<string, string> = { github: "Continue with GitHub", gitlab: "Continue with GitLab", dev: "Continue as the local dev user" };

export function Login() {
  const [providers, setProviders] = useState<string[] | null>(null);
  const params = new URLSearchParams(useLocation().search);
  const error = params.get("error");
  const next = params.get("next") ?? "/";

  useEffect(() => {
    api
      .meta()
      .then((m) => setProviders(m.providers))
      .catch(() => setProviders([]));
  }, []);

  return (
    <div className="login">
      <div className="login__card">
        <div className="brand" style={{ fontSize: 20 }}>
          <span className="brand__mark" aria-hidden="true">
            <svg width="14" height="14" viewBox="0 0 14 14">
              <path d="M2 11.5 7 2.5l5 9z" fill="#fff" />
            </svg>
          </span>
          Rocketsled
        </div>
        <div>
          <h1>Your own platform, in an afternoon.</h1>
          <p className="muted" style={{ marginTop: 10 }}>
            Pick a platform, connect your cloud account, and get production-shaped environments without building any of it yourself.
          </p>
        </div>
        {error && <Notice tone="bad">{error}</Notice>}
        <div className="login__providers">
          {providers === null && <span className="loading">Loading…</span>}
          {providers?.map((p) => (
            <a key={p} className="btn btn--lg" href={loginUrl(p, next)}>
              {labels[p] ?? `Continue with ${p}`}
            </a>
          ))}
          {providers?.length === 0 && <Notice tone="warn">No sign-in providers are configured.</Notice>}
        </div>
      </div>
    </div>
  );
}
