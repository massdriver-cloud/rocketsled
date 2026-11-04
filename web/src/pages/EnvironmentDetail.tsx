import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, ApiError, timeAgo } from "../api";
import type { EnvironmentView, InstanceResource } from "../types";
import { Loading, Notice, Pill } from "../components/ui";

const FAST = 4000;
const SLOW = 20000;

// Fields worth showing from a resource payload: addresses and names, never secrets.
function highlights(r: InstanceResource): [string, string][] {
  const out: [string, string][] = [];
  for (const [k, v] of Object.entries(r.payload ?? {})) {
    if (typeof v !== "string" && typeof v !== "number" && typeof v !== "boolean") continue;
    if (/secret|password|token|key/i.test(k)) continue;
    out.push([k, String(v)]);
  }
  return out.slice(0, 5);
}

export function EnvironmentDetail() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [view, setView] = useState<EnvironmentView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const timer = useRef<number | null>(null);

  const refresh = useCallback(async () => {
    try {
      const v = await api.environment(id);
      setView(v);
      setError(null);
      if (v.gone) navigate("/environments", { replace: true });
      return v;
    } catch (e) {
      setError((e as ApiError).message);
      return null;
    }
  }, [id, navigate]);

  useEffect(() => {
    let cancelled = false;
    async function tick() {
      const v = await refresh();
      if (cancelled) return;
      const live = !v || v.environment.state === "deploying" || v.environment.state === "decommissioning";
      timer.current = window.setTimeout(tick, live ? FAST : SLOW);
    }
    tick();
    return () => {
      cancelled = true;
      if (timer.current) window.clearTimeout(timer.current);
    };
  }, [refresh]);

  async function deploy() {
    setBusy("deploy");
    setActionError(null);
    try {
      await api.deploy(id);
      await refresh();
    } catch (e) {
      setActionError((e as ApiError).message);
    } finally {
      setBusy(null);
    }
  }

  async function destroy() {
    if (!view || !window.confirm(`Destroy ${view.environment.name}? Everything in it is removed from your cloud account.`)) return;
    setBusy("destroy");
    setActionError(null);
    try {
      await api.destroy(id);
      await refresh();
    } catch (e) {
      setActionError((e as ApiError).message);
    } finally {
      setBusy(null);
    }
  }

  if (error && !view) return <Notice tone="bad">{error}</Notice>;
  if (!view) return <Loading />;
  const { environment, platform, runs } = view;
  const tearingDown = environment.state === "decommissioning";

  return (
    <>
      <div className="crumbs">
        <Link to="/environments">Environments</Link>
        <span>/</span>
        <span>{environment.name}</span>
      </div>
      <div className="head">
        <div className="head__text">
          <h1>{environment.name}</h1>
          <p className="mono small">{environment.environmentId}</p>
        </div>
        <div className="btn-row">
          <Pill status={environment.state} />
          <button className="btn" onClick={deploy} disabled={busy !== null || tearingDown}>
            {busy === "deploy" ? "Deploying…" : "Redeploy"}
          </button>
          <button className="btn btn--danger" onClick={destroy} disabled={busy !== null || tearingDown}>
            Destroy
          </button>
        </div>
      </div>

      {actionError && <Notice tone="bad">{actionError}</Notice>}
      {error && <Notice tone="warn">Live status is stale: {error}</Notice>}
      {tearingDown && <Notice tone="warn">Removing everything in this environment. It disappears from the list when done.</Notice>}
      {runs[0]?.status === "failed" && runs[0].message && (
        <div style={{ marginBottom: 16 }}>
          <Notice tone="bad">
            <b>The last deploy failed.</b>
            <span>{runs[0].message}</span>
          </Notice>
        </div>
      )}
      {platform && platform.unfulfilled.length > 0 && (
        <div style={{ marginBottom: 16 }}>
          <Notice tone="warn">
            <b>Waiting on a credential.</b>
            {platform.unfulfilled.map((u) => (
              <span key={u.instanceId + u.field} className="mono small">
                {u.instanceId} needs {u.resourceType.replace(/@.*/, "")}
              </span>
            ))}
            <Link to="/credentials">Add it under Credentials.</Link>
          </Notice>
        </div>
      )}

      <div className="section">
        <div className="section__title">
          <h2>Components</h2>
        </div>
        <div className="card">
          <table className="table">
            <thead>
              <tr>
                <th>Component</th>
                <th>Size</th>
                <th>Status</th>
                <th>Details</th>
              </tr>
            </thead>
            <tbody>
              {platform?.instances.map((pi) => (
                <tr key={pi.id}>
                  <td>
                    <div className="cell--strong">{pi.componentName || pi.componentSlug}</div>
                    <div className="muted small mono">{pi.bundleName}</div>
                  </td>
                  <td>{environment.presets[pi.componentSlug] ?? <span className="muted">—</span>}</td>
                  <td>
                    <Pill status={pi.status} live={false} />
                  </td>
                  <td>
                    {pi.resources.length === 0 ? (
                      <span className="muted">—</span>
                    ) : (
                      <dl className="kv small">
                        {pi.resources.flatMap((r) =>
                          highlights(r).map(([k, v]) => (
                            <>
                              <dt key={r.field + k + "k"}>{k}</dt>
                              <dd key={r.field + k + "v"} className="mono">
                                {v}
                              </dd>
                            </>
                          )),
                        )}
                      </dl>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="section">
        <div className="section__title">
          <h2>Deploys</h2>
        </div>
        <div className="card">
          <table className="table">
            <thead>
              <tr>
                <th>#</th>
                <th>Status</th>
                <th>Trigger</th>
                <th>Started</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((r) => (
                <tr key={r.id}>
                  <td className="mono">{r.number}</td>
                  <td>
                    <Pill status={r.status} live={r.status === "running" || r.status === "queued"} />
                  </td>
                  <td>{r.trigger === "create" ? "Environment created" : "Redeploy"}</td>
                  <td className="muted" title={r.createdAt}>
                    {timeAgo(r.createdAt)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </>
  );
}
