import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, ApiError } from "../api";
import type { InstanceView, LinkableProject } from "../types";
import { Eyebrow, Loading, Notice, Pill, Sha } from "../components/ui";
import { runNumber, timeAgo } from "../format";
import { Track } from "../components/Track";

const FAST = 4000;
const SLOW = 20000;

export function InstancePage() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [view, setView] = useState<InstanceView | null>(null);
  const [project, setProject] = useState<LinkableProject | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"deploy" | "destroy" | null>(null);
  const timer = useRef<number | null>(null);

  const refresh = useCallback(async () => {
    try {
      const v = await api.instance(id);
      setView(v);
      setError(null);
      if (v.gone) navigate(`/stacks/${v.instance.stackId}`, { replace: true });
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
      const live =
        !v ||
        v.instance.state === "launching" ||
        v.instance.state === "decommissioning" ||
        (v.deploys ?? []).some((d) => d.status === "queued" || d.status === "building" || d.status === "running");
      timer.current = window.setTimeout(tick, live ? FAST : SLOW);
    }
    tick();
    return () => {
      cancelled = true;
      if (timer.current) window.clearTimeout(timer.current);
    };
  }, [refresh]);

  useEffect(() => {
    if (!view || project) return;
    api
      .stack(view.instance.stackId)
      .then((s) => api.project(s.stack.projectId))
      .then(setProject)
      .catch(() => setProject(null));
  }, [view, project]);

  async function deployNow() {
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
    if (!view) return;
    if (!window.confirm(`Destroy ${view.instance.name}? Every package in ${view.instance.environmentId} is decommissioned.`)) return;
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

  if (error && !view) return <Notice tone="error">{error}</Notice>;
  if (!view) return <Loading />;
  const { instance, environment } = view;
  const deploys = view.deploys ?? [];
  const unfulfilled = environment?.unfulfilled ?? [];
  const live = deploys.some((d) => d.status === "running" || d.status === "queued" || d.status === "building") || instance.state === "launching";
  const decommissioning = instance.state === "decommissioning";
  const latest = deploys[0];

  return (
    <>
      <div className="crumbs">
        <Link to="/">Stacks</Link>
        <span>/</span>
        <Link to={`/stacks/${instance.stackId}`}>{project?.project.name ?? "stack"}</Link>
        <span>/</span>
        <span>{instance.slug}</span>
      </div>
      <div className="page__head">
        <div className="stack">
          <Eyebrow>Instance</Eyebrow>
          <h1>{instance.name}</h1>
          <div className="card__meta" style={{ marginTop: 2 }}>
            <Pill status={instance.state} />
            <span>env {instance.environmentId}</span>
            <span>following {instance.branch}</span>
            {latest && (
              <span>
                at <Sha sha={latest.sha} />
              </span>
            )}
          </div>
        </div>
        <div className="btn-row">
          <button className="btn btn--primary" onClick={deployNow} disabled={busy !== null || decommissioning}>
            {busy === "deploy" ? "Deploying…" : "Deploy now"}
          </button>
          <button className="btn btn--danger" onClick={destroy} disabled={busy !== null || decommissioning}>
            {decommissioning ? "Decommissioning…" : "Destroy instance"}
          </button>
        </div>
      </div>

      {actionError && <Notice tone="error">{actionError}</Notice>}
      {error && <Notice tone="warn">Live status is stale: {error}</Notice>}
      {decommissioning && (
        <Notice tone="warn">
          Packages are being torn down in reverse dependency order. The environment is deleted and this page disappears once every package is gone.
        </Notice>
      )}

      <section className="card" style={{ marginTop: 18 }}>
        <div className="card__row">
          <Eyebrow>Track · {environment?.instances.length ?? 0} packages</Eyebrow>
          {live && <Eyebrow red>Run in progress</Eyebrow>}
        </div>
        {environment ? (
          <Track environment={environment} live={live} appComponent={project?.appComponent} />
        ) : (
          <Loading label="Waiting for the environment" />
        )}
        {unfulfilled.length > 0 && (
          <Notice tone="error">
            <strong>Deploys will block on unfilled connections.</strong>
            {unfulfilled.map((u) => (
              <span key={`${u.instanceId}-${u.field}`} className="mono">
                {u.instanceId} · {u.field} needs a {u.resourceType}
              </span>
            ))}
            <span className="muted">Set an environment default of that type on the base environment, or wire the slot in Massdriver.</span>
          </Notice>
        )}
      </section>

      <div className="grid grid--2" style={{ marginTop: 24 }}>
        <section>
          <h2 style={{ marginBottom: 14 }}>Runs</h2>
          {deploys.length === 0 && <p className="muted">No runs recorded yet.</p>}
          {deploys.length > 0 && (
            <div style={{ overflowX: "auto" }}>
              <table className="runs">
                <thead>
                  <tr>
                    <th>Run</th>
                    <th>Status</th>
                    <th>Commit</th>
                    <th>Trigger</th>
                    <th>When</th>
                  </tr>
                </thead>
                <tbody>
                  {deploys.map((d) => (
                    <tr key={d.id}>
                      <td className="runs__num">{runNumber(d.number)}</td>
                      <td>
                        <Pill status={d.status} live={d.status === "running" || d.status === "queued" || d.status === "building"} />
                      </td>
                      <td>
                        <div className="mono">
                          <Sha sha={d.sha} /> <span className="muted">{d.ref}</span>
                        </div>
                        {d.imageTag !== d.sha && <div className="runs__msg mono">tag {d.imageTag}</div>}
                        {d.image && <div className="runs__msg mono">{d.image}</div>}
                        {d.message && d.status === "failed" && <div className="runs__msg">{d.message}</div>}
                        {d.log && (
                          <details className="buildlog">
                            <summary>build log</summary>
                            <pre className="snippet">{d.log}</pre>
                          </details>
                        )}
                      </td>
                      <td className="mono">{d.trigger}</td>
                      <td className="mono muted" title={d.createdAt}>
                        {timeAgo(d.createdAt)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>

        <aside className="card" style={{ alignSelf: "start" }}>
          <Eyebrow>Configuration</Eyebrow>
          <dl className="kv" style={{ marginTop: 10 }}>
            <dt>Environment</dt>
            <dd className="mono">{instance.environmentId}</dd>
            <dt>Branch</dt>
            <dd className="mono">{instance.branch}</dd>
            <dt>Presets</dt>
            <dd className="mono" style={{ display: "grid", gap: 4 }}>
              {Object.keys(instance.presets ?? {}).length === 0 && <span className="muted">inherited from base environment</span>}
              {Object.entries(instance.presets ?? {}).map(([slug, preset]) => (
                <span key={slug}>
                  {slug} <span className="muted">→</span> {preset}
                </span>
              ))}
            </dd>
            <dt>Launched</dt>
            <dd className="mono" title={instance.createdAt}>
              {timeAgo(instance.createdAt)}
            </dd>
          </dl>
        </aside>
      </div>
    </>
  );
}
