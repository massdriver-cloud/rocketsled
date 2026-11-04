import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError, timeAgo } from "../api";
import type { Account, ProjectView } from "../types";
import { Loading, Notice, Pill } from "../components/ui";

export function Overview({ account }: { account: Account }) {
  const [view, setView] = useState<ProjectView | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .project()
      .then(setView)
      .catch((e: ApiError) => setError(e.message));
  }, [account.id]);

  if (error) return <Notice tone="bad">{error}</Notice>;
  if (!view) return <Loading />;
  const { project, catalog, environments } = view;

  return (
    <>
      <div className="head">
        <div className="head__text">
          <h1>{project.name}</h1>
          <p>
            {catalog.name} on {catalog.cloud.toUpperCase()}. Organization <span className="mono">{account.organizationId}</span>.
          </p>
        </div>
        <Link to="/environments/new" className="btn btn--primary">
          New environment
        </Link>
      </div>

      <div className="section">
        <div className="section__title">
          <h2>Environments</h2>
          {environments.length > 0 && <Link to="/environments">All environments</Link>}
        </div>
        {environments.length === 0 ? (
          <div className="card empty">
            <p>No environments yet. Each one is a complete copy of the platform with the sizes you pick.</p>
            <Link to="/environments/new" className="btn btn--primary">
              Create the first environment
            </Link>
          </div>
        ) : (
          <div className="list">
            {environments.map((e) => (
              <Link key={e.id} to={`/environments/${e.id}`} className="card item">
                <div className="item__text">
                  <span className="item__title">{e.name}</span>
                  <span className="item__meta">
                    <span className="mono">{e.environmentId}</span>
                    <span>created {timeAgo(e.createdAt)}</span>
                  </span>
                </div>
                <Pill status={e.state} />
              </Link>
            ))}
          </div>
        )}
      </div>

      <div className="section">
        <div className="section__title">
          <h2>What's in the platform</h2>
        </div>
        <div className="card">
          <table className="table">
            <thead>
              <tr>
                <th>Component</th>
                <th>Bundle</th>
                <th>Sizes</th>
              </tr>
            </thead>
            <tbody>
              {project.components.map((c) => (
                <tr key={c.slug}>
                  <td className="cell--strong">{c.name}</td>
                  <td className="mono muted">{c.bundle.name}</td>
                  <td>
                    <div className="chips">
                      {c.bundle.presets.map((p) => (
                        <span key={p.name} className="chip">
                          {p.name}
                        </span>
                      ))}
                    </div>
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
