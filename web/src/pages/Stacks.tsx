import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api";
import type { StackResponse } from "../types";
import { Empty, Eyebrow, Loading, Marker, Notice, Pill } from "../components/ui";
import { timeAgo } from "../format";

export function Stacks() {
  const [stacks, setStacks] = useState<StackResponse[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .stacks()
      .then(setStacks)
      .catch((e: ApiError) => setError(e.message));
  }, []);

  return (
    <>
      <div className="page__head">
        <div className="stack">
          <Eyebrow>Stacks</Eyebrow>
          <h1>Everything on the track</h1>
        </div>
        <Link to="/stacks/new" className="btn btn--primary">
          New stack
        </Link>
      </div>

      {error && <Notice tone="error">{error}</Notice>}
      {stacks === null && !error && <Loading />}

      {stacks && stacks.length === 0 && (
        <Empty title="No stacks yet">
          <p className="muted">A stack is a repository linked to a Massdriver project. Link one to launch your first instance.</p>
          <Link to="/stacks/new" className="btn btn--primary">
            Link a repository
          </Link>
        </Empty>
      )}

      {stacks && stacks.length > 0 && (
        <ul className="rail">
          {stacks.map(({ stack, instances: rawInstances }) => {
            const instances = rawInstances ?? [];
            const worst = instances.some((i) => i.state === "failed")
              ? "failed"
              : instances.some((i) => i.state === "launching")
                ? "launching"
                : instances.length
                  ? "running"
                  : "hold";
            return (
              <li key={stack.id}>
                <Marker status={worst} live={worst === "launching"} />
                <Link to={`/stacks/${stack.id}`} className="card card--link">
                  <div className="card__row">
                    <span className="card__title">{stack.name}</span>
                    <span className="mono muted">
                      {instances.length} {instances.length === 1 ? "instance" : "instances"}
                    </span>
                  </div>
                  <div className="card__meta">
                    <span>{stack.repo.fullName}</span>
                    <span>→ project {stack.projectId}</span>
                    <span>linked {timeAgo(stack.createdAt)}</span>
                  </div>
                  {instances.length > 0 && (
                    <div className="card__meta" style={{ marginTop: 10, gap: 8 }}>
                      {instances.map((i) => (
                        <span key={i.id} style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
                          <Pill status={i.state} />
                          <span>{i.slug}</span>
                        </span>
                      ))}
                    </div>
                  )}
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}
