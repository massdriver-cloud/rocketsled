import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError, timeAgo } from "../api";
import type { Environment } from "../types";
import { Loading, Notice, Pill } from "../components/ui";

export function Environments() {
  const [envs, setEnvs] = useState<Environment[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .environments()
      .then(setEnvs)
      .catch((e: ApiError) => setError(e.message));
  }, []);

  return (
    <>
      <div className="head">
        <div className="head__text">
          <h1>Environments</h1>
          <p>Each environment is a full copy of the platform in your cloud account.</p>
        </div>
        <Link to="/environments/new" className="btn btn--primary">
          New environment
        </Link>
      </div>
      {error && <Notice tone="bad">{error}</Notice>}
      {envs === null && !error && <Loading />}
      {envs?.length === 0 && (
        <div className="card empty">
          <p>Nothing here yet.</p>
          <Link to="/environments/new" className="btn btn--primary">
            Create the first environment
          </Link>
        </div>
      )}
      {envs && envs.length > 0 && (
        <div className="list">
          {envs.map((e) => (
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
    </>
  );
}
