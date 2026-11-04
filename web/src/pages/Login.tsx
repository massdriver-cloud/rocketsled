import { useEffect, useState } from "react";
import { useLocation } from "react-router-dom";
import { api, loginUrl } from "../api";
import type { Meta } from "../types";
import { Notice, Wordmark } from "../components/ui";

const labels: Record<string, string> = { github: "GitHub", gitlab: "GitLab", dev: "the local dev account" };

export function Login() {
  const [meta, setMeta] = useState<Meta | null>(null);
  const location = useLocation();
  const params = new URLSearchParams(location.search);
  const error = params.get("error");
  const next = params.get("next") ?? "/";

  useEffect(() => {
    api.meta().then(setMeta).catch(() => setMeta({ providers: [], baseUrl: "" }));
  }, []);

  return (
    <div className="login">
      <section className="login__hero">
        <Wordmark hero to="/login" />
        <p className="login__tag">
          Link a repository to a Massdriver project. Launch a copy of production for every branch. Ship on push.
        </p>
        {error && <Notice tone="error">{error}</Notice>}
        <div className="login__providers">
          {meta === null && <span className="loading">Checking the range…</span>}
          {meta?.providers.map((p) => (
            <a key={p} className="btn btn--ink" href={loginUrl(p, next)}>
              Sign in with {labels[p] ?? p}
            </a>
          ))}
          {meta && meta.providers.length === 0 && (
            <Notice tone="warn">No sign-in providers are configured. Add GitHub or GitLab under auth.providers in rocketsled.yaml.</Notice>
          )}
        </div>
      </section>
      <footer className="login__foot">
        <div className="inner">
          <span>ROCKETSLED · a PaaS on Massdriver</span>
          <span>stack = repository + project · instance = forked environment</span>
        </div>
      </footer>
    </div>
  );
}
