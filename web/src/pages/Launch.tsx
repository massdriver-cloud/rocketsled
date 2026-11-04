import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, ApiError } from "../api";
import type { LinkableProject, StackResponse } from "../types";
import { Eyebrow, Loading, Notice } from "../components/ui";

const slugPattern = /^[a-z0-9]{1,20}$/;

export function Launch() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [data, setData] = useState<StackResponse | null>(null);
  const [project, setProject] = useState<LinkableProject | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [branch, setBranch] = useState("");
  const [presets, setPresets] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  useEffect(() => {
    api
      .stack(id)
      .then(async (res) => {
        setData(res);
        setBranch(res.stack.repo.defaultBranch);
        setProject(await api.project(res.stack.projectId));
      })
      .catch((e: ApiError) => setLoadError(e.message));
  }, [id]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await api.launch(id, { slug, name: name || slug, branch, presets });
      navigate(`/instances/${res.instance.id}`);
    } catch (err) {
      setError(err as ApiError);
      setBusy(false);
    }
  }

  if (loadError) return <Notice tone="error">{loadError}</Notice>;
  if (!data || !project) return <Loading label="Loading the blueprint" />;
  const slugOk = slugPattern.test(slug);
  const envId = slug ? `${project.link.id}-${slug}` : `${project.link.id}-<slug>`;

  return (
    <>
      <div className="crumbs">
        <Link to="/">Stacks</Link>
        <span>/</span>
        <Link to={`/stacks/${id}`}>{data.stack.name}</Link>
        <span>/</span>
        <span>Launch</span>
      </div>
      <div className="page__head">
        <div className="stack">
          <Eyebrow red>Launch</Eyebrow>
          <h1>New instance of {data.stack.name}</h1>
          <p className="muted">
            Forks <code>{project.link.id}-{project.link.baseEnvironment}</code> into <code>{envId}</code>, applies your presets, then deploys.
          </p>
        </div>
      </div>

      <form className="form grid grid--2" onSubmit={submit}>
        <div className="form">
          <div className={`field ${error?.field === "slug" || (slug && !slugOk) ? "field--error" : ""}`}>
            <label htmlFor="slug">Instance ID</label>
            <input
              id="slug"
              className="mono"
              value={slug}
              onChange={(e) => setSlug(e.target.value.toLowerCase())}
              placeholder="pr42, staging, alicedev"
              autoComplete="off"
              autoFocus
            />
            <span className="hint">1–20 lowercase letters or digits. Becomes the Massdriver environment {envId}.</span>
            {error?.field === "slug" && <span className="field__error">{error.message}</span>}
          </div>

          <div className="field">
            <label htmlFor="name">Display name</label>
            <input id="name" value={name} onChange={(e) => setName(e.target.value)} placeholder={slug || "Defaults to the instance ID"} />
          </div>

          <div className={`field ${error?.field === "branch" ? "field--error" : ""}`}>
            <label htmlFor="branch">Branch to follow</label>
            <input id="branch" className="mono" value={branch} onChange={(e) => setBranch(e.target.value)} autoComplete="off" />
            <span className="hint">Every push to this branch redeploys the app component with the new commit.</span>
            {error?.field === "branch" && <span className="field__error">{error.message}</span>}
          </div>

          <div className="field">
            <span className="label">Presets</span>
            <span className="hint">
              One choice per component, from the bundle's own presets. Leave a component on “inherit” to keep the base environment's configuration.
            </span>
            <div className="presets" style={{ marginTop: 6 }}>
              {project.project.components.map((c) => {
                const isApp = c.slug === project.appComponent;
                const fieldErr = error?.field === `presets.${c.slug}` ? error.message : null;
                return (
                  <div key={c.slug} className={`preset ${fieldErr ? "field--error" : ""}`}>
                    <div>
                      <div style={{ fontWeight: 700, fontStretch: "108%" }}>
                        {c.slug}
                        {isApp && (
                          <span className="eyebrow eyebrow--red" style={{ marginLeft: 8 }}>
                            app · {project.link.imageTagParam} follows {branch || "branch"}
                          </span>
                        )}
                      </div>
                      <div className="mono muted" style={{ fontSize: 12 }}>
                        {c.bundle.name}@{c.bundle.version}
                      </div>
                    </div>
                    <div className="field">
                      <label className="sr-only" htmlFor={`preset-${c.slug}`}>
                        Preset for {c.slug}
                      </label>
                      <select
                        id={`preset-${c.slug}`}
                        value={presets[c.slug] ?? ""}
                        onChange={(e) => setPresets({ ...presets, [c.slug]: e.target.value })}
                        disabled={c.bundle.presets.length === 0}
                      >
                        <option value="">{c.bundle.presets.length ? "Inherit from base environment" : "No presets in this bundle"}</option>
                        {c.bundle.presets.map((p) => (
                          <option key={p.name} value={p.name}>
                            {p.name}
                          </option>
                        ))}
                      </select>
                      {fieldErr && <span className="field__error">{fieldErr}</span>}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          {error && !error.field && <Notice tone="error">{error.message}</Notice>}

          <div className="btn-row">
            <button className="btn btn--primary" disabled={busy || !slugOk || !branch}>
              {busy ? "Launching…" : "Launch"}
            </button>
            <Link to={`/stacks/${id}`} className="btn btn--quiet">
              Cancel
            </Link>
            {busy && <span className="loading">Forking the environment</span>}
          </div>
        </div>

        <aside className="card" style={{ alignSelf: "start" }}>
          <Eyebrow>Launch sequence</Eyebrow>
          <ol style={{ margin: "10px 0 0", paddingLeft: 18, display: "grid", gap: 8, fontSize: 14 }}>
            <li>
              Fork <code>{project.link.id}-{project.link.baseEnvironment}</code>, copying its environment defaults
              {project.link.copySecrets ? " and secrets" : ""}.
            </li>
            {project.link.imageRegistry && (
              <li>
                Build <code>{data.stack.path || "/"}</code> at the head of {branch || "the branch"} and push it to <code>{project.link.imageRegistry}</code>.
              </li>
            )}
            <li>Merge each chosen preset over the seeded parameters.</li>
            <li>
              Set <code>{project.link.imageTagParam}</code> on <code>{project.appComponent}</code> to the commit.
            </li>
            <li>Deploy every package in dependency order.</li>
          </ol>
        </aside>
      </form>
    </>
  );
}
