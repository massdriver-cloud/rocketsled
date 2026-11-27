import { useCallback, useEffect, useState } from "react";
import { api, ApiError, timeAgo } from "../api";
import type { Environment, Release } from "../types";
import { Notice } from "./ui";

function workflowStep(env: Environment) {
  return `- name: Release to ${env.name}
  env:
    ROCKETSLED_URL: ${window.location.origin}
    ROCKETSLED_TOKEN: \${{ secrets.ROCKETSLED_TOKEN }}
  run: |
    curl -fsS -X POST "$ROCKETSLED_URL/api/v1/environments/${env.slug}/releases" \\
      -H "Authorization: Bearer $ROCKETSLED_TOKEN" -H 'Content-Type: application/json' \\
      -d "{\\"image\\": \\"$IMAGE\\", \\"description\\": \\"$GITHUB_SHA\\"}"`;
}

export function Releases({ env, component, onRelease }: { env: Environment; component: string; onRelease: () => void }) {
  const [releases, setReleases] = useState<Release[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    api
      .releases(env.id)
      .then(setReleases)
      .catch((e: ApiError) => setError(e.message));
  }, [env.id]);

  useEffect(load, [load, env.image]);

  async function rollBack(r: Release) {
    setBusy(true);
    setError(null);
    try {
      await api.release(env.id, r.image, `Roll back to #${r.number}`);
      load();
      onRelease();
    } catch (e) {
      setError((e as ApiError).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="section">
      <div className="section__title">
        <h2>Releases</h2>
      </div>
      {error && <Notice tone="bad">{error}</Notice>}
      <div className="card">
        <div className="card__body">
          <p className="small">
            {env.image ? (
              <>
                <b>{component}</b> runs <span className="mono">{env.image}</span>.
              </>
            ) : (
              <>
                Nothing released to <b>{component}</b> yet. Create a deploy token, add it to your repository as <span className="mono">ROCKETSLED_TOKEN</span>,
                and add this step after the one that pushes <span className="mono">$IMAGE</span>:
              </>
            )}
          </p>
          {!env.image && <pre className="log">{workflowStep(env)}</pre>}
        </div>
        {releases && releases.length > 0 && (
          <table className="table">
            <tbody>
              {releases.map((r, i) => (
                <tr key={r.id}>
                  <td className="mono">#{r.number}</td>
                  <td className="mono">{r.image}</td>
                  <td>{r.description}</td>
                  <td className="muted">{r.requestedBy}</td>
                  <td className="muted" title={r.createdAt}>
                    {timeAgo(r.createdAt)}
                  </td>
                  <td>
                    {i > 0 && r.image !== env.image && (
                      <button className="btn" disabled={busy} onClick={() => rollBack(r)}>
                        Roll back
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      {env.image && (
        <details className="small">
          <summary>Release from GitHub Actions</summary>
          <pre className="log">{workflowStep(env)}</pre>
        </details>
      )}
    </div>
  );
}
