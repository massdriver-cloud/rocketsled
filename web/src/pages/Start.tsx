import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiError } from "../api";
import type { Account, Catalog, PlatformRequirement } from "../types";
import { Loading, Markdown, Notice, Pill, Steps } from "../components/ui";
import { SchemaForm } from "../components/SchemaForm";
import { schemaDefaults } from "../schema";

// Start walks a new account from "no platform" to "ready": choose a catalog,
// watch it install, connect the cloud credential it needs.
export function Start({ account, onAccount }: { account: Account; onAccount: (a: Account) => void }) {
  const navigate = useNavigate();
  useEffect(() => {
    if (account.status === "ready") navigate("/", { replace: true });
  }, [account.status, navigate]);

  if (account.status === "new") return <ChooseCatalog onAccount={onAccount} />;
  if (account.status === "installing" || account.status === "failed") return <Installing account={account} onAccount={onAccount} />;
  if (account.status === "needs_credentials") return <Credentials account={account} onAccount={onAccount} />;
  return <Loading />;
}

function ChooseCatalog({ onAccount }: { onAccount: (a: Account) => void }) {
  const [catalogs, setCatalogs] = useState<Catalog[] | null>(null);
  const [chosen, setChosen] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .catalogs()
      .then((c) => {
        setCatalogs(c);
        if (c.length === 1) setChosen(c[0].id);
      })
      .catch((e: ApiError) => setError(e.message));
  }, []);

  async function install() {
    setBusy(true);
    setError(null);
    try {
      onAccount(await api.install(chosen));
    } catch (e) {
      setError((e as ApiError).message);
      setBusy(false);
    }
  }

  return (
    <>
      <Steps current={1} />
      <div className="head">
        <div className="head__text">
          <h1>Choose a platform</h1>
          <p>Each platform is a complete setup: the infrastructure pieces, how they connect, and a production environment to start from.</p>
        </div>
      </div>
      {error && <Notice tone="bad">{error}</Notice>}
      {catalogs === null && !error && <Loading />}
      <div className="catalogs">
        {catalogs?.map((c) => (
          <button key={c.id} type="button" className="card catalog" aria-pressed={chosen === c.id} onClick={() => setChosen(c.id)}>
            <div className="catalog__row">
              <h2>{c.name}</h2>
              <span className="catalog__cloud">{c.cloud}</span>
            </div>
            <p className="muted">{c.tagline}</p>
            <div className="chips">
              {c.bundles.map((b) => (
                <span key={b} className="chip">
                  {b}
                </span>
              ))}
            </div>
          </button>
        ))}
      </div>
      <div className="btn-row" style={{ marginTop: 24 }}>
        <button className="btn btn--primary btn--lg" disabled={!chosen || busy} onClick={install}>
          {busy ? "Starting…" : "Install this platform"}
        </button>
      </div>
    </>
  );
}

function Installing({ account, onAccount }: { account: Account; onAccount: (a: Account) => void }) {
  const [current, setCurrent] = useState(account);
  const [busy, setBusy] = useState(false);
  const logRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (current.status !== "installing") return;
    const t = window.setInterval(async () => {
      try {
        const a = await api.account();
        setCurrent(a);
        if (a.status !== "installing") onAccount(a);
      } catch {
        /* keep polling */
      }
    }, 3000);
    return () => window.clearInterval(t);
  }, [current.status, onAccount]);

  useEffect(() => {
    logRef.current?.scrollTo({ top: logRef.current.scrollHeight });
  }, [current.installLog]);

  async function retry() {
    setBusy(true);
    try {
      const a = await api.install(current.catalogId ?? "");
      setCurrent(a);
      onAccount(a);
    } finally {
      setBusy(false);
    }
  }

  const failed = current.status === "failed";
  return (
    <>
      <Steps current={2} />
      <div className="head">
        <div className="head__text">
          <h1>{failed ? "The install stopped" : "Setting up your platform"}</h1>
          <p>
            {failed
              ? "Something in the log below needs attention. Retrying picks up where it left off."
              : "Publishing the platform into your organization and laying out the first project. This takes a few minutes."}
          </p>
        </div>
        <Pill status={current.status} />
      </div>
      {failed && (
        <div style={{ marginBottom: 16 }}>
          <Notice tone="bad">{current.installError}</Notice>
        </div>
      )}
      <pre className="log" ref={logRef}>
        {current.installLog || "Starting…"}
      </pre>
      {failed && (
        <div className="btn-row" style={{ marginTop: 20 }}>
          <button className="btn btn--primary" onClick={retry} disabled={busy}>
            {busy ? "Retrying…" : "Retry install"}
          </button>
        </div>
      )}
    </>
  );
}

export function Credentials({ account, onAccount, standalone = false }: { account: Account; onAccount: (a: Account) => void; standalone?: boolean }) {
  const [reqs, setReqs] = useState<PlatformRequirement[] | null>(null);
  const [values, setValues] = useState<Record<string, Record<string, unknown>>>({});
  const [tab, setTab] = useState<Record<string, number>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    api
      .platforms()
      .then((rs) => {
        setReqs(rs);
        setValues((current) => {
          const next = { ...current };
          for (const r of rs) next[r.type.id] = { ...schemaDefaults(r.type.schema), ...current[r.type.id] };
          return next;
        });
      })
      .catch((e: ApiError) => setLoadError(e.message));
  }, [account.id]);

  async function submit(typeId: string) {
    setBusy(typeId);
    setError(null);
    try {
      const a = await api.providePlatform(typeId, "", values[typeId] ?? {});
      onAccount(a);
      setReqs(await api.platforms());
    } catch (e) {
      setError(e as ApiError);
    } finally {
      setBusy(null);
    }
  }

  return (
    <>
      {!standalone && <Steps current={3} />}
      <div className="head">
        <div className="head__text">
          <h1>{standalone ? "Credentials" : "Connect your cloud"}</h1>
          <p>Rocketsled deploys into your own account. Give it a role to use; nothing else about your account is touched.</p>
        </div>
      </div>
      {loadError && <Notice tone="bad">{loadError}</Notice>}
      {reqs === null && !loadError && <Loading />}
      <div className="section">
        {reqs?.map((r) => {
          const t = r.type;
          const tabIndex = tab[t.id] ?? 0;
          return (
            <div key={t.id} className="card">
              <div className="card__head">
                <div>
                  <h2>{t.label}</h2>
                  {t.description && (
                    <p className="muted small" style={{ marginTop: 2 }}>
                      {t.description}
                    </p>
                  )}
                </div>
                <Pill status={r.satisfied ? "ready" : "needs_credentials"} live={false} />
              </div>
              <div className="card__body" style={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)", gap: 32 }}>
                <form
                  className="form"
                  onSubmit={(e) => {
                    e.preventDefault();
                    submit(t.id);
                  }}
                >
                  <SchemaForm
                    schema={t.schema}
                    order={t.uiOrder}
                    value={values[t.id] ?? {}}
                    onChange={(v) => setValues({ ...values, [t.id]: v })}
                    errorField={error?.field}
                    errorMessage={error?.message}
                  />
                  {error && !error.field && <Notice tone="bad">{error.message}</Notice>}
                  <div className="btn-row">
                    <button className="btn btn--primary" disabled={busy === t.id}>
                      {busy === t.id ? "Saving…" : r.satisfied ? "Replace credential" : "Save and continue"}
                    </button>
                  </div>
                </form>
                <div>
                  {t.instructions.length > 0 && (
                    <>
                      <div className="tabs" role="tablist">
                        {t.instructions.map((ins, i) => (
                          <button key={ins.label} role="tab" aria-selected={tabIndex === i} onClick={() => setTab({ ...tab, [t.id]: i })}>
                            {ins.label}
                          </button>
                        ))}
                      </div>
                      <Markdown source={t.instructions[tabIndex].markdown} />
                    </>
                  )}
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </>
  );
}
