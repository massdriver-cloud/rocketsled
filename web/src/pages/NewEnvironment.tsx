import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, ApiError } from "../api";
import type { ProjectView } from "../types";
import { Loading, Notice } from "../components/ui";

const slugPattern = /^[a-z0-9]{1,20}$/;

function summarize(params: Record<string, unknown>): string {
  return Object.entries(params)
    .filter(([k, v]) => k !== "__name" && v !== "" && (typeof v === "string" || typeof v === "number" || typeof v === "boolean"))
    .slice(0, 4)
    .map(([k, v]) => `${k}: ${String(v)}`)
    .join(" · ");
}

export function NewEnvironment() {
  const navigate = useNavigate();
  const [view, setView] = useState<ProjectView | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugTouched, setSlugTouched] = useState(false);
  const [presets, setPresets] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  useEffect(() => {
    api
      .project()
      .then((v) => {
        setView(v);
        const first: Record<string, string> = {};
        for (const c of v.project.components) if (c.bundle.presets[0]) first[c.slug] = c.bundle.presets[0].name;
        setPresets(first);
      })
      .catch((e: ApiError) => setLoadError(e.message));
  }, []);

  function onName(v: string) {
    setName(v);
    if (!slugTouched) setSlug(v.toLowerCase().replace(/[^a-z0-9]/g, "").slice(0, 20));
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const env = await api.createEnvironment({ slug, name: name || slug, presets });
      navigate(`/environments/${env.id}`);
    } catch (err) {
      setError(err as ApiError);
      setBusy(false);
    }
  }

  if (loadError) return <Notice tone="bad">{loadError}</Notice>;
  if (!view) return <Loading />;
  const slugOk = slugPattern.test(slug);

  return (
    <>
      <div className="crumbs">
        <Link to="/environments">Environments</Link>
        <span>/</span>
        <span>New</span>
      </div>
      <div className="head">
        <div className="head__text">
          <h1>New environment</h1>
          <p>Pick a size for each part of the platform. You can redeploy with different sizes later.</p>
        </div>
      </div>

      <form className="form" onSubmit={submit} style={{ maxWidth: 680 }}>
        <div className="card card__body" style={{ display: "grid", gap: 18 }}>
          <div className="field">
            <label htmlFor="name">Name</label>
            <input id="name" value={name} onChange={(e) => onName(e.target.value)} placeholder="Staging" autoFocus />
          </div>
          <div className={`field ${error?.field === "slug" ? "field--error" : ""}`}>
            <label htmlFor="slug">Identifier</label>
            <input
              id="slug"
              className="mono"
              value={slug}
              onChange={(e) => {
                setSlugTouched(true);
                setSlug(e.target.value.toLowerCase());
              }}
              placeholder="staging"
              autoComplete="off"
            />
            <span className="hint">
              Lowercase letters and digits, up to 20. Becomes <span className="mono">{view.project.id}-{slug || "…"}</span>.
            </span>
            {error?.field === "slug" && <span className="field__error">{error.message}</span>}
          </div>
        </div>

        {view.project.components.map((c) => (
          <div key={c.slug} className="card">
            <div className="card__head">
              <div>
                <h2>{c.name}</h2>
                <span className="muted small mono">{c.bundle.name}</span>
              </div>
            </div>
            <div className="card__body">
              {c.bundle.presets.length === 0 ? (
                <p className="muted">This component has one configuration.</p>
              ) : (
                <div className="choices" role="radiogroup" aria-label={`Size for ${c.name}`}>
                  {c.bundle.presets.map((p) => (
                    <label key={p.name} className="choice" data-checked={presets[c.slug] === p.name}>
                      <input type="radio" name={`preset-${c.slug}`} checked={presets[c.slug] === p.name} onChange={() => setPresets({ ...presets, [c.slug]: p.name })} />
                      <span className="choice__text">
                        <span className="choice__title">{p.name}</span>
                        <span className="choice__meta">{summarize(p.params)}</span>
                      </span>
                    </label>
                  ))}
                </div>
              )}
              {error?.field === `presets.${c.slug}` && <span className="field__error">{error.message}</span>}
            </div>
          </div>
        ))}

        {error && !error.field && <Notice tone="bad">{error.message}</Notice>}
        <div className="btn-row">
          <button className="btn btn--primary btn--lg" disabled={busy || !slugOk}>
            {busy ? "Creating…" : "Create and deploy"}
          </button>
          <Link to="/environments" className="btn">
            Cancel
          </Link>
        </div>
      </form>
    </>
  );
}
