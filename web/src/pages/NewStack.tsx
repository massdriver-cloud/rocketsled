import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, ApiError } from "../api";
import type { LinkableProject, Repo, StackResponse } from "../types";
import { Eyebrow, Loading, Notice } from "../components/ui";
import { CISnippet } from "../components/CISnippet";

export function NewStack() {
  const [projects, setProjects] = useState<LinkableProject[] | null>(null);
  const [repos, setRepos] = useState<Repo[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [projectId, setProjectId] = useState("");
  const [repoFullName, setRepoFullName] = useState("");
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [filter, setFilter] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [created, setCreated] = useState<StackResponse | null>(null);
  const navigate = useNavigate();

  useEffect(() => {
    Promise.all([api.projects(), api.repos()])
      .then(([p, r]) => {
        setProjects(p);
        setRepos(r);
        if (p.length === 1) setProjectId(p[0].link.id);
      })
      .catch((e: ApiError) => setLoadError(e.message));
  }, []);

  const visibleRepos = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return (repos ?? []).filter((r) => !q || r.fullName.toLowerCase().includes(q)).slice(0, 200);
  }, [repos, filter]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await api.createStack({ name, repoFullName, projectId, path });
      setCreated(res);
    } catch (err) {
      setError(err as ApiError);
    } finally {
      setBusy(false);
    }
  }

  if (created) {
    return (
      <>
        <div className="crumbs">
          <Link to="/">Stacks</Link>
          <span>/</span>
          <span>{created.stack.name}</span>
        </div>
        <div className="page__head">
          <div className="stack">
            <Eyebrow>Stack linked</Eyebrow>
            <h1>{created.stack.name}</h1>
          </div>
          <button className="btn btn--primary" onClick={() => navigate(`/stacks/${created.stack.id}/launch`)}>
            Launch first instance
          </button>
        </div>
        <div className="grid">
          <Notice tone="ok">
            <strong>Pushes to {created.stack.repo.fullName} now deploy any instance following that branch.</strong>
            <span className="muted">
              Rocketsled registered a webhook on the repository. If your build produces the container image, have CI call back
              once the image is pushed instead, using the token below.
            </span>
          </Notice>
          <Notice tone="warn">
            <strong>Deploy token, shown once</strong>
            <code>{created.deployToken}</code>
            <span className="muted">Store it as a CI secret. Rocketsled keeps only a hash.</span>
          </Notice>
          <CISnippet stackId={created.stack.id} />
          <div>
            <Link to={`/stacks/${created.stack.id}`} className="btn">
              Open stack
            </Link>
          </div>
        </div>
      </>
    );
  }

  return (
    <>
      <div className="crumbs">
        <Link to="/">Stacks</Link>
        <span>/</span>
        <span>New</span>
      </div>
      <div className="page__head">
        <div className="stack">
          <Eyebrow>New stack</Eyebrow>
          <h1>Link a repository to a project</h1>
        </div>
      </div>

      {loadError && <Notice tone="error">{loadError}</Notice>}
      {(projects === null || repos === null) && !loadError && <Loading label="Loading projects and repositories" />}

      {projects && repos && (
        <form className="form grid grid--2" onSubmit={submit}>
          <div className="form">
            <fieldset className="field" style={{ border: 0, padding: 0, margin: 0 }}>
              <legend className="label" style={{ fontWeight: 600, fontSize: 13, marginBottom: 8 }}>
                Massdriver project
              </legend>
              {projects.length === 0 && (
                <Notice tone="warn">No projects are available. Add project IDs under massdriver.projects in rocketsled.yaml.</Notice>
              )}
              {projects.map((lp) => (
                <button
                  type="button"
                  key={lp.link.id}
                  className="card card--select"
                  aria-pressed={projectId === lp.link.id}
                  onClick={() => setProjectId(lp.link.id)}
                >
                  <div className="card__row">
                    <span className="card__title">{lp.project.name}</span>
                    <span className="mono muted">{lp.link.id}</span>
                  </div>
                  {lp.project.description && <p className="muted" style={{ marginTop: 4 }}>{lp.project.description}</p>}
                  <div className="card__meta">
                    <span>base env {lp.link.baseEnvironment}</span>
                    <span>app {lp.appComponent}</span>
                    <span>
                      {lp.project.components.length} {lp.project.components.length === 1 ? "component" : "components"}
                    </span>
                  </div>
                  <div className="card__meta" style={{ marginTop: 8 }}>
                    {lp.project.components.map((c) => (
                      <span key={c.slug}>
                        {c.slug}
                        <span className="muted">
                          {" "}
                          · {c.bundle.presets.length} {c.bundle.presets.length === 1 ? "preset" : "presets"}
                        </span>
                      </span>
                    ))}
                  </div>
                </button>
              ))}
              {error?.field === "projectId" && <span className="field__error">{error.message}</span>}
            </fieldset>

            <div className={`field ${error?.field === "repoFullName" ? "field--error" : ""}`}>
              <label htmlFor="repo-filter">Repository</label>
              <input
                id="repo-filter"
                placeholder="Filter repositories"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                autoComplete="off"
              />
              <div className="repo-list" role="listbox" aria-label="Repositories">
                {visibleRepos.length === 0 && <div className="loading">No repositories match</div>}
                {visibleRepos.map((r) => (
                  <button
                    type="button"
                    key={r.fullName}
                    role="option"
                    aria-selected={repoFullName === r.fullName}
                    aria-pressed={repoFullName === r.fullName}
                    onClick={() => {
                      setRepoFullName(r.fullName);
                      if (!name) setName(r.fullName.split("/").pop() ?? "");
                    }}
                  >
                    <span>{r.fullName}</span>
                    <span className="mono muted">
                      {r.private ? "private" : "public"} · {r.defaultBranch}
                    </span>
                  </button>
                ))}
              </div>
              {error?.field === "repoFullName" && <span className="field__error">{error.message}</span>}
            </div>

            <div className="field">
              <label htmlFor="stack-name">Stack name</label>
              <input id="stack-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Defaults to the repository name" />
            </div>

            <div className={`field ${error?.field === "path" ? "field--error" : ""}`}>
              <label htmlFor="stack-path">Path to the Dockerfile</label>
              <input id="stack-path" className="mono" value={path} onChange={(e) => setPath(e.target.value)} placeholder="/ (repository root)" autoComplete="off" />
              <span className="hint">The directory Rocketsled builds on every push, for example example/hello-world in a monorepo.</span>
              {error?.field === "path" && <span className="field__error">{error.message}</span>}
            </div>

            {error && !error.field && <Notice tone="error">{error.message}</Notice>}

            <div className="btn-row">
              <button className="btn btn--primary" disabled={busy || !projectId || !repoFullName}>
                {busy ? "Linking…" : "Link stack"}
              </button>
              <Link to="/" className="btn btn--quiet">
                Cancel
              </Link>
            </div>
          </div>

          <aside className="card" style={{ alignSelf: "start" }}>
            <Eyebrow>What happens</Eyebrow>
            <ol style={{ margin: "10px 0 0", paddingLeft: 18, display: "grid", gap: 8, fontSize: 14 }}>
              <li>Rocketsled registers a push webhook on the repository.</li>
              <li>Each instance you launch forks the project's base environment, so it inherits the same cloud credentials and defaults.</li>
              <li>You pick a preset per component. The app component's image tag follows the branch.</li>
              <li>
                On every push Rocketsled builds the Dockerfile at the path you give, pushes the image tagged with the commit, and redeploys the app
                component.
              </li>
            </ol>
          </aside>
        </form>
      )}
    </>
  );
}
