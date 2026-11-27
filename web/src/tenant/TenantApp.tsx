import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { BrowserRouter, Link, Navigate, NavLink, Route, Routes, useLocation, useNavigate, useParams } from "react-router-dom";
import { api, ApiError, timeAgo } from "../api";
import type { Environment, EnvironmentView, Me, Meta, Offer, OfferedComponent, PlatformInstance, Skin } from "../types";
import { Loading, Notice } from "../components/ui";
import "./tenant.css";

const defaultCopy: Record<string, string> = {
  environment: "app",
  environments: "Apps",
  newEnvironment: "Create app",
  preset: "Plan",
  deploy: "Redeploy",
  destroy: "Delete",
  running: "Running",
  deploying: "Deploying",
  failed: "Failed",
  decommissioning: "Deleting",
  empty: "No apps yet.",
  create: "Create app",
  runtime: "Runtime",
  addons: "Add-ons",
  open: "Open app",
};

const SkinContext = createContext<{ skin: Skin; copy: (k: string) => string; offer?: Offer }>({
  skin: { name: "Rocketsled" },
  copy: (k) => defaultCopy[k] ?? k,
});
const useSkin = () => useContext(SkinContext);

function capitalize(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

export default function TenantApp({ meta }: { meta: Meta }) {
  const skin = meta.skin ?? { name: "Rocketsled" };
  const copy = (k: string) => skin.copy?.[k] ?? defaultCopy[k] ?? k;
  return (
    <SkinContext.Provider value={{ skin, copy }}>
      <div className={`t t--${skin.layout ?? "dashboard"}`}>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<SignIn meta={meta} />} />
            <Route path="*" element={<Session />} />
          </Routes>
        </BrowserRouter>
      </div>
    </SkinContext.Provider>
  );
}

function Brand({ big }: { big?: boolean }) {
  const { skin } = useSkin();
  return (
    <span data-rs="brand" className={`t-brand ${big ? "t-brand--big" : ""}`}>
      {skin.logo && <img src={skin.logo} alt="" />}
      <span>{skin.name}</span>
    </span>
  );
}

function SignIn({ meta }: { meta: Meta }) {
  const { skin } = useSkin();
  const navigate = useNavigate();
  const next = new URLSearchParams(useLocation().search).get("next") ?? "/";
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState(false);
  const hasPassword = meta.providers.includes("password");
  const hasOpen = meta.providers.includes("open");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      if (hasOpen) await api.signUp(login);
      else await api.passwordLogin(login, password);
      navigate(next.startsWith("/") ? next : "/", { replace: true });
    } catch (err) {
      setError(err as ApiError);
      setBusy(false);
    }
  }

  return (
    <div className="t-login">
      <header data-rs="header" className="t-header">
        <div className="t-header__in">
          <Brand />
        </div>
      </header>
      <div className="t-login__body">
        <div className="card t-login__card">
          <div className="card__head">
            <h1>Sign in to {skin.name}</h1>
          </div>
          <div className="card__body">
            {skin.tagline && <p className="t-tagline">{skin.tagline}</p>}
            {hasOpen && (
              <form className="form" onSubmit={submit}>
                <div className={`field ${error?.field === "login" ? "field--error" : ""}`}>
                  <label htmlFor="login">What should we call you?</label>
                  <input id="login" value={login} onChange={(e) => setLogin(e.target.value)} placeholder="ada" autoFocus autoComplete="nickname" />
                  {error?.field === "login" && <span className="field__error">{error.message}</span>}
                  <span className="hint">No account or password needed. Your apps stay with this browser.</span>
                </div>
                {error && !error.field && <Notice tone="bad">{error.message}</Notice>}
                <button className="btn btn--primary btn--lg" disabled={busy || !login}>
                  {busy ? "Starting…" : "Get started"}
                </button>
              </form>
            )}
            {hasPassword && !hasOpen && (
              <form className="form" onSubmit={submit}>
                <div className={`field ${error?.field === "login" ? "field--error" : ""}`}>
                  <label htmlFor="login">Your name</label>
                  <input id="login" value={login} onChange={(e) => setLogin(e.target.value)} placeholder="ada" autoFocus autoComplete="username" />
                  {error?.field === "login" && <span className="field__error">{error.message}</span>}
                </div>
                <div className={`field ${error?.field === "password" ? "field--error" : ""}`}>
                  <label htmlFor="password">Password</label>
                  <input id="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
                  {error?.field === "password" && <span className="field__error">{error.message}</span>}
                </div>
                {error && !error.field && <Notice tone="bad">{error.message}</Notice>}
                <button className="btn btn--primary btn--lg" disabled={busy || !login || !password}>
                  {busy ? "Signing in…" : "Sign in"}
                </button>
              </form>
            )}
            {meta.providers
              .filter((p) => p !== "password")
              .map((p) => (
                <a key={p} className="btn btn--lg" href={`/auth/${p}/login?next=${encodeURIComponent(next)}`}>
                  {p === "dev" ? "Sign in" : `Sign in with ${capitalize(p)}`}
                </a>
              ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function Session() {
  const [me, setMe] = useState<Me | null | undefined>(undefined);
  const [offer, setOffer] = useState<Offer | undefined>(undefined);
  const location = useLocation();
  const ctx = useSkin();

  useEffect(() => {
    api
      .me()
      .then(async (m) => {
        if (!m.account) m.account = await api.createAccount("");
        setMe(m);
        api.offer().then(setOffer).catch(() => undefined);
      })
      .catch(() => setMe(null));
  }, []);

  if (me === undefined) return <Loading />;
  if (me === null) return <Navigate to={`/login?next=${encodeURIComponent(location.pathname)}`} replace />;

  return (
    <SkinContext.Provider value={{ ...ctx, offer }}>
      <Header me={me} />
      <main className="main t-main">
        <Routes>
          <Route path="/" element={<Apps />} />
          <Route path="/new" element={<NewApp />} />
          <Route path="/apps/:id" element={<AppPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </SkinContext.Provider>
  );
}

function Header({ me }: { me: Me }) {
  const { copy } = useSkin();
  const navigate = useNavigate();
  async function signOut() {
    await api.logout();
    navigate("/login", { replace: true });
  }
  return (
    <header data-rs="header" className="t-header">
      <div className="t-header__in">
        <Link to="/">
          <Brand />
        </Link>
        <nav className="t-nav">
          <NavLink to="/" end>
            {copy("environments")}
          </NavLink>
        </nav>
        <span className="t-spacer" />
        <span className="t-user">{me.user.login}</span>
        <button className="linkbtn t-signout" onClick={signOut}>
          Sign out
        </button>
      </div>
    </header>
  );
}

function stateLabel(copy: (k: string) => string, state: string) {
  return copy(state);
}

function State({ state }: { state: string }) {
  const { copy } = useSkin();
  const tone = state === "running" ? "ok" : state === "failed" ? "bad" : "warn";
  const live = state === "deploying" || state === "decommissioning";
  return <span className={`pill pill--${tone} ${live ? "pill--live" : ""}`}>{stateLabel(copy, state)}</span>;
}

function runtimeName(offer: Offer | undefined, id?: string) {
  return offer?.runtimes.find((r) => r.id === id)?.name ?? id ?? "";
}

function Apps() {
  const { copy, offer } = useSkin();
  const [apps, setApps] = useState<Environment[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .environments()
      .then(setApps)
      .catch((e: ApiError) => setError(e.message));
  }, []);

  return (
    <>
      <div className="head">
        <div className="head__text">
          <h1>{copy("environments")}</h1>
        </div>
        <Link to="/new" className="btn btn--primary">
          {copy("newEnvironment")}
        </Link>
      </div>
      {error && <Notice tone="bad">{error}</Notice>}
      {apps === null && !error && <Loading />}
      {apps?.length === 0 && (
        <div className="card empty t-empty">
          <p>{copy("empty")}</p>
          <Link to="/new" className="btn btn--primary btn--lg">
            {copy("newEnvironment")}
          </Link>
        </div>
      )}
      {apps && apps.length > 0 && (
        <div className="list">
          {apps.map((a) => (
            <Link key={a.id} to={`/apps/${a.id}`} className="card item t-app">
              <div className="item__text">
                <span className="item__title">{a.name}</span>
                <span className="item__meta">
                  <span>{runtimeName(offer, a.runtime)}</span>
                  {a.addons && a.addons.length > 0 && <span>+ {a.addons.join(", ")}</span>}
                  <span>created {timeAgo(a.createdAt)}</span>
                </span>
              </div>
              <State state={a.state} />
            </Link>
          ))}
        </div>
      )}
    </>
  );
}

const slugPattern = /^[a-z0-9]{1,20}$/;

function PresetPicker({ component, value, onChange }: { component: OfferedComponent; value: string; onChange: (v: string) => void }) {
  const { skin, copy } = useSkin();
  const index = Math.max(0, component.presets.findIndex((p) => p.name === value));
  if (component.presets.length === 0) return null;
  return (
    <div className="t-presets">
      {skin.layout === "dashboard" && component.presets.length > 1 && (
        <div className="t-slider">
          <span className="t-slider__label">{copy("preset")}</span>
          <input
            type="range"
            min={0}
            max={component.presets.length - 1}
            step={1}
            value={index}
            aria-label={`${copy("preset")} for ${component.name}`}
            onChange={(e) => onChange(component.presets[Number(e.target.value)].name)}
          />
        </div>
      )}
      <div className="t-preset-grid" role="radiogroup" aria-label={`${copy("preset")} for ${component.name}`}>
        {component.presets.map((p) => (
          <button
            type="button"
            key={p.name}
            data-rs="preset"
            role="radio"
            aria-checked={p.name === value}
            className="t-preset"
            onClick={() => onChange(p.name)}
          >
            <span className="t-preset__name">{p.name}</span>
            {p.price && <span className="t-preset__price">{p.price}</span>}
            {p.summary && <span className="t-preset__summary">{p.summary}</span>}
          </button>
        ))}
      </div>
    </div>
  );
}

function NewApp() {
  const { copy, offer, skin } = useSkin();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [runtime, setRuntime] = useState("");
  const [addons, setAddons] = useState<string[]>([]);
  const [presets, setPresets] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [offerError, setOfferError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState<Offer | undefined>(offer);

  useEffect(() => {
    if (offer) setLoaded(offer);
    else
      api
        .offer()
        .then(setLoaded)
        .catch((e: ApiError) => setOfferError(e.message));
  }, [offer]);

  useEffect(() => {
    if (loaded && !runtime && loaded.runtimes[0]) setRuntime(loaded.runtimes[0].id);
  }, [loaded, runtime]);

  if (offerError) return <Notice tone="bad">{offerError}</Notice>;
  if (!loaded) return <Loading />;
  const rt = loaded.runtimes.find((r) => r.id === runtime);
  const slug = name.toLowerCase().replace(/[^a-z0-9]/g, "").slice(0, 20);
  const pick = (c: OfferedComponent) => presets[c.slug] ?? c.presets[0]?.name ?? "";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!rt) return;
    setBusy(true);
    setError(null);
    const chosen: Record<string, string> = {};
    for (const c of [...rt.components, ...rt.addons.filter((a) => addons.includes(a.slug))]) if (pick(c)) chosen[c.slug] = pick(c);
    try {
      const app = await api.createApp({ slug, name: name.trim() || slug, runtime: rt.id, addons, presets: chosen });
      navigate(`/apps/${app.id}`);
    } catch (err) {
      setError(err as ApiError);
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="t-new">
      <div className="crumbs">
        <Link to="/">{copy("environments")}</Link>
        <span>/</span>
        <span>{copy("newEnvironment")}</span>
      </div>
      <div className="head">
        <div className="head__text">
          <h1>{copy("newEnvironment")}</h1>
          <LimitsNote offer={loaded} />
        </div>
      </div>

      <section className="card t-step">
        <div className="card__head">
          <h2>{capitalize(copy("environment"))} name</h2>
        </div>
        <div className="card__body">
          <div className={`field ${error?.field === "slug" ? "field--error" : ""}`}>
            <input id="name" value={name} onChange={(e) => setName(e.target.value)} placeholder="my-app" autoFocus autoComplete="off" aria-label="Name" />
            <span className="hint">
              Lowercase letters and digits, up to 20. {slug && rt && <span className="mono">{rt.project}-{slug}</span>}
            </span>
            {error?.field === "slug" && <span className="field__error">{error.message}</span>}
          </div>
        </div>
      </section>

      <section className="card t-step">
        <div className="card__head">
          <h2>{copy("runtime")}</h2>
        </div>
        <div className="card__body">
          <div className="t-runtimes" role="radiogroup" aria-label={copy("runtime")}>
            {loaded.runtimes.map((r) => (
              <button
                type="button"
                key={r.id}
                data-rs="preset"
                role="radio"
                aria-checked={r.id === runtime}
                className="t-runtime"
                onClick={() => {
                  setRuntime(r.id);
                  setAddons([]);
                }}
              >
                <span className="t-runtime__mark" aria-hidden="true">
                  {r.name.charAt(0)}
                </span>
                <span className="t-runtime__name">{r.name}</span>
                {r.summary && <span className="t-preset__summary">{r.summary}</span>}
              </button>
            ))}
          </div>
        </div>
      </section>

      {rt?.components.map((c) => (
        <section key={c.slug} className="card t-step">
          <div className="card__head">
            <h2>{c.name}</h2>
          </div>
          <div className="card__body">
            <PresetPicker component={c} value={pick(c)} onChange={(v) => setPresets({ ...presets, [c.slug]: v })} />
          </div>
        </section>
      ))}

      {rt && rt.addons.length > 0 && (
        <section className="card t-step">
          <div className="card__head">
            <h2>{copy("addons")}</h2>
          </div>
          <div className="card__body t-addons">
            {rt.addons.map((a) => {
              const on = addons.includes(a.slug);
              return (
                <div key={a.slug} className={`t-addon ${on ? "t-addon--on" : ""}`}>
                  <label className="t-addon__head">
                    <input type="checkbox" checked={on} onChange={() => setAddons(on ? addons.filter((x) => x !== a.slug) : [...addons, a.slug])} />
                    <span>
                      <b>{a.name}</b>
                      {a.summary && <span className="t-preset__summary">{a.summary}</span>}
                    </span>
                  </label>
                  {on && <PresetPicker component={a} value={pick(a)} onChange={(v) => setPresets({ ...presets, [a.slug]: v })} />}
                </div>
              );
            })}
          </div>
        </section>
      )}

      {error && error.field !== "slug" && <Notice tone="bad">{error.message}</Notice>}
      <div className="btn-row t-submit">
        <button className="btn btn--primary btn--lg" disabled={busy || !slugPattern.test(slug) || !rt}>
          {busy ? (skin.layout === "minimal" ? "…" : "Creating…") : copy("create")}
        </button>
        <Link to="/" className="btn btn--lg">
          Cancel
        </Link>
      </div>
    </form>
  );
}

function LimitsNote({ offer }: { offer: Offer }) {
  const { copy } = useSkin();
  const parts: string[] = [];
  if (offer.limits.appsPerPerson) parts.push(`You can have ${offer.limits.appsPerPerson} ${copy("environments").toLowerCase()} at a time.`);
  if (offer.limits.ttlHours) parts.push(`Each one is removed automatically after ${offer.limits.ttlHours} hours.`);
  return parts.length ? <p>{parts.join(" ")}</p> : null;
}

function expiresIn(createdAt: string, hours: number) {
  const left = new Date(createdAt).getTime() + hours * 3600_000 - Date.now();
  if (left <= 0) return "soon";
  const h = Math.floor(left / 3600_000);
  const m = Math.floor((left % 3600_000) / 60_000);
  return h > 0 ? `in about ${h}h ${m}m` : `in about ${m}m`;
}

function appLinks(instances: PlatformInstance[]): string[] {
  const out: string[] = [];
  for (const pi of instances) {
    if (pi.status !== "PROVISIONED") continue;
    for (const r of pi.resources) {
      for (const key of ["url", "endpoint"]) {
        const v = r.payload?.[key];
        if (typeof v === "string" && v.startsWith("https://") && !out.includes(v)) out.push(v);
      }
    }
  }
  return out;
}

function highlightsFor(offer: Offer | undefined, resourceType: string, payload: Record<string, unknown>): [string, string][] {
  const keys = offer?.highlights[resourceType.replace(/@.*/, "")];
  const entries = Object.entries(payload ?? {}).filter(([k, v]) => ["string", "number", "boolean"].includes(typeof v) && !/secret|password|token|key/i.test(k));
  const picked = keys ? entries.filter(([k]) => keys.includes(k)) : entries.slice(0, 3);
  return picked.map(([k, v]) => [k, String(v)]);
}

const instanceStatus: Record<string, string> = {
  PROVISIONED: "running",
  FAILED: "failed",
  INITIALIZED: "deploying",
  PROVISIONING: "deploying",
  DECOMMISSIONED: "decommissioning",
};

function AppPage() {
  const { id = "" } = useParams();
  const { copy, offer } = useSkin();
  const navigate = useNavigate();
  const [view, setView] = useState<EnvironmentView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const timer = useRef<number | null>(null);

  const refresh = useCallback(async () => {
    try {
      const v = await api.environment(id);
      setView(v);
      setError(null);
      if (v.gone) navigate("/", { replace: true });
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
      timer.current = window.setTimeout(tick, live ? 4000 : 20000);
    }
    tick();
    return () => {
      cancelled = true;
      if (timer.current) window.clearTimeout(timer.current);
    };
  }, [refresh]);

  async function act(fn: () => Promise<unknown>) {
    setBusy(true);
    try {
      await fn();
      await refresh();
    } catch (e) {
      setError((e as ApiError).message);
    } finally {
      setBusy(false);
    }
  }

  if (error && !view) return <Notice tone="bad">{error}</Notice>;
  if (!view) return <Loading />;
  const { environment: app, platform, runs } = view;
  const instances = platform?.instances ?? [];
  const links = appLinks(instances);
  const deleting = app.state === "decommissioning";

  return (
    <>
      <div className="crumbs">
        <Link to="/">{copy("environments")}</Link>
        <span>/</span>
        <span>{app.name}</span>
      </div>
      <div className="head">
        <div className="head__text">
          <h1>{app.name}</h1>
          <p>
            {runtimeName(offer, app.runtime)} · <span className="mono">{app.environmentId}</span>
          </p>
        </div>
        <div className="btn-row">
          <State state={app.state} />
          <button className="btn" disabled={busy || deleting} onClick={() => act(() => api.deploy(id))}>
            {copy("deploy")}
          </button>
          <button
            className="btn btn--danger"
            disabled={busy || deleting}
            onClick={() => window.confirm(`${copy("destroy")} ${app.name}? Everything in it is removed.`) && act(() => api.destroy(id))}
          >
            {copy("destroy")}
          </button>
        </div>
      </div>

      {error && <Notice tone="warn">{error}</Notice>}
      {runs[0]?.status === "failed" && runs[0].message && <Notice tone="bad">{runs[0].message}</Notice>}

      <section className="card t-hero">
        {links.length > 0 ? (
          <div className="t-hero__body">
            <span className="t-hero__label">Your {copy("environment")} is live at</span>
            {links.map((l) => (
              <a key={l} href={l} target="_blank" rel="noreferrer" className="t-hero__url mono">
                {l.replace(/^https:\/\//, "")}
              </a>
            ))}
            <a href={links[0]} target="_blank" rel="noreferrer" className="btn btn--primary btn--lg">
              {copy("open")}
            </a>
          </div>
        ) : deleting ? (
          <div className="t-hero__body">
            <span className="t-hero__label">{copy("decommissioning")}…</span>
          </div>
        ) : (
          <div className="t-hero__body">
            <span className="t-hero__label">{app.state === "failed" ? copy("failed") : `${copy("deploying")}`}</span>
            {app.state === "deploying" && <span className="t-progress" aria-hidden="true" />}
            <span className="muted small">The address shows up here as soon as it answers. This usually takes a few minutes.</span>
          </div>
        )}
        {offer?.limits.ttlHours ? (
          <div className="t-hero__foot muted small">Removed automatically {expiresIn(app.createdAt, offer.limits.ttlHours)}.</div>
        ) : null}
      </section>

      <div className="section">
        <div className="section__title">
          <h2>Resources</h2>
        </div>
        <div className="card">
          <table className="table">
            <thead>
              <tr>
                <th>Component</th>
                <th>{copy("preset")}</th>
                <th>Status</th>
                <th>Details</th>
              </tr>
            </thead>
            <tbody>
              {instances.map((pi) => (
                <tr key={pi.id}>
                  <td className="cell--strong">{pi.componentName || pi.componentSlug}</td>
                  <td>{app.presets[pi.componentSlug] ?? "—"}</td>
                  <td>
                    <State state={instanceStatus[pi.status] ?? "deploying"} />
                  </td>
                  <td>
                    <dl className="kv small">
                      {pi.resources.flatMap((r) =>
                        highlightsFor(offer, r.resourceType, r.payload).map(([k, v]) => [
                          <dt key={`${r.field}-${k}-k`}>{k}</dt>,
                          <dd key={`${r.field}-${k}-v`} className="mono">
                            {v}
                          </dd>,
                        ]),
                      )}
                    </dl>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="section">
        <div className="section__title">
          <h2>Activity</h2>
        </div>
        <div className="card">
          <table className="table">
            <tbody>
              {runs.map((r) => (
                <tr key={r.id}>
                  <td className="mono">#{r.number}</td>
                  <td>{r.trigger === "create" ? "Created" : copy("deploy")}</td>
                  <td>
                    <State state={r.status === "succeeded" ? "running" : r.status === "failed" ? "failed" : "deploying"} />
                  </td>
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
