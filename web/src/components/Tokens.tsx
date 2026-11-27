import { useCallback, useEffect, useState } from "react";
import { api, ApiError, timeAgo } from "../api";
import type { DeployToken } from "../types";
import { Loading, Notice } from "./ui";

export function Tokens() {
  const [tokens, setTokens] = useState<DeployToken[] | null>(null);
  const [name, setName] = useState("");
  const [secret, setSecret] = useState<string | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    api
      .tokens()
      .then(setTokens)
      .catch((e: ApiError) => setError(e));
  }, []);
  useEffect(load, [load]);

  async function create(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const t = await api.createToken(name);
      setSecret(t.token);
      setName("");
      load();
    } catch (err) {
      setError(err as ApiError);
    } finally {
      setBusy(false);
    }
  }

  async function revoke(t: DeployToken) {
    if (!window.confirm(`Revoke ${t.name}? Builds using it stop releasing.`)) return;
    try {
      await api.revokeToken(t.id);
      load();
    } catch (err) {
      setError(err as ApiError);
    }
  }

  return (
    <>
      <div className="head">
        <div className="head__text">
          <h1>Deploy tokens</h1>
          <p>A build uses a deploy token to release an image to an environment. It can do nothing else.</p>
        </div>
      </div>
      {secret && (
        <Notice tone="ok">
          Copy this token now. It is not shown again.
          <pre className="log">{secret}</pre>
        </Notice>
      )}
      {error && !error.field && <Notice tone="bad">{error.message}</Notice>}
      <form className="card card__body btn-row" onSubmit={create}>
        <div className={`field ${error?.field === "name" ? "field--error" : ""}`}>
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="github-actions" aria-label="Token name" />
          {error?.field === "name" && <span className="field__error">{error.message}</span>}
        </div>
        <button className="btn btn--primary" disabled={busy || !name.trim()}>
          Create token
        </button>
      </form>
      {tokens === null && !error && <Loading />}
      {tokens && tokens.length > 0 && (
        <div className="section">
          <div className="card">
            <table className="table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Created</th>
                  <th>Last used</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {tokens.map((t) => (
                  <tr key={t.id}>
                    <td className="cell--strong">{t.name}</td>
                    <td className="muted">{timeAgo(t.createdAt)}</td>
                    <td className="muted">{t.lastUsedAt ? timeAgo(t.lastUsedAt) : "never"}</td>
                    <td>
                      <button className="btn btn--danger" onClick={() => revoke(t)}>
                        Revoke
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </>
  );
}
