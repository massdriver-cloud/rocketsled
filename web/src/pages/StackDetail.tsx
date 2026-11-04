import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, ApiError } from "../api";
import type { LinkableProject, StackResponse } from "../types";
import { Empty, Eyebrow, Loading, Marker, Notice, Pill } from "../components/ui";
import { timeAgo } from "../format";
import { CISnippet } from "../components/CISnippet";

export function StackDetail() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [data, setData] = useState<StackResponse | null>(null);
  const [project, setProject] = useState<LinkableProject | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    api
      .stack(id)
      .then((res) => {
        setData(res);
        return api.project(res.stack.projectId).then(setProject).catch(() => setProject(null));
      })
      .catch((e: ApiError) => setError(e.message));
  }, [id]);

  useEffect(load, [load]);

  async function remove() {
    if (!data) return;
    if (!window.confirm(`Unlink ${data.stack.name}? The repository webhook is removed. Massdriver environments are untouched.`)) return;
    setBusy(true);
    try {
      await api.deleteStack(id);
      navigate("/");
    } catch (e) {
      setError((e as ApiError).message);
      setBusy(false);
    }
  }

  if (error && !data) return <Notice tone="error">{error}</Notice>;
  if (!data) return <Loading />;
  const { stack } = data;
  const instances = data.instances ?? [];

  return (
    <>
      <div className="crumbs">
        <Link to="/">Stacks</Link>
        <span>/</span>
        <span>{stack.name}</span>
      </div>
      <div className="page__head">
        <div className="stack">
          <Eyebrow>Stack</Eyebrow>
          <h1>{stack.name}</h1>
          <p className="muted mono" style={{ fontSize: 13 }}>
            <a href={stack.repo.url} target="_blank" rel="noreferrer">
              {stack.repo.fullName}
            </a>{" "}
            → {stack.projectId}
          </p>
        </div>
        <div className="btn-row">
          <Link to={`/stacks/${stack.id}/launch`} className="btn btn--primary">
            Launch instance
          </Link>
          <button className="btn btn--danger" onClick={remove} disabled={busy || instances.length > 0} title={instances.length ? "Destroy every instance first" : undefined}>
            Unlink stack
          </button>
        </div>
      </div>

      {error && <Notice tone="error">{error}</Notice>}

      <div className="grid grid--2">
        <section>
          <h2 style={{ marginBottom: 16 }}>Instances</h2>
          {instances.length === 0 && (
            <Empty title="No instances yet">
              <p className="muted">Each instance is a forked Massdriver environment following a branch of {stack.repo.fullName}.</p>
              <Link to={`/stacks/${stack.id}/launch`} className="btn btn--primary">
                Launch the first one
              </Link>
            </Empty>
          )}
          {instances.length > 0 && (
            <ul className="rail">
              {instances.map((i) => (
                <li key={i.id}>
                  <Marker status={i.state} live={i.state === "launching" || i.state === "decommissioning"} />
                  <Link to={`/instances/${i.id}`} className="card card--link">
                    <div className="card__row">
                      <span className="card__title">{i.name}</span>
                      <Pill status={i.state} />
                    </div>
                    <div className="card__meta">
                      <span>env {i.environmentId}</span>
                      <span>branch {i.branch}</span>
                      <span>launched {timeAgo(i.createdAt)}</span>
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </section>

        <aside style={{ display: "grid", gap: 18, alignSelf: "start" }}>
          <div className="card">
            <Eyebrow>Project</Eyebrow>
            {project ? (
              <dl className="kv" style={{ marginTop: 10 }}>
                <dt>Name</dt>
                <dd>{project.project.name}</dd>
                <dt>Base env</dt>
                <dd className="mono">{project.link.baseEnvironment}</dd>
                <dt>App</dt>
                <dd className="mono">
                  {project.appComponent} · {project.link.imageTagParam}
                </dd>
                <dt>Blueprint</dt>
                <dd className="mono" style={{ display: "grid", gap: 4 }}>
                  {project.project.components.map((c) => (
                    <span key={c.slug}>
                      {c.slug} <span className="muted">{c.bundle.name}@{c.bundle.version}</span>
                    </span>
                  ))}
                </dd>
              </dl>
            ) : (
              <p className="muted" style={{ marginTop: 8 }}>
                Project {stack.projectId} could not be loaded from Massdriver.
              </p>
            )}
          </div>
          <div className="card">
            <Eyebrow>Repository</Eyebrow>
            <dl className="kv" style={{ marginTop: 10 }}>
              <dt>Host</dt>
              <dd className="mono">{stack.repo.provider}</dd>
              <dt>Default</dt>
              <dd className="mono">{stack.repo.defaultBranch}</dd>
              <dt>Path</dt>
              <dd className="mono">{stack.path || "/"}</dd>
              <dt>Access</dt>
              <dd className="mono">{stack.repo.private ? "private" : "public"}</dd>
            </dl>
          </div>
          <CISnippet stackId={stack.id} />
        </aside>
      </div>
    </>
  );
}
