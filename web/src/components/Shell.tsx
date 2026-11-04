import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import type { User } from "../types";
import { Wordmark } from "./ui";

function utcClock(d: Date) {
  return d.toISOString().slice(11, 19) + "Z";
}

export function Shell({ user, orgLabel, children }: { user: User; orgLabel?: string; children: React.ReactNode }) {
  const [now, setNow] = useState(() => new Date());
  const navigate = useNavigate();

  useEffect(() => {
    const t = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(t);
  }, []);

  async function signOut() {
    await api.logout();
    navigate("/login", { replace: true });
  }

  return (
    <>
      <header className="board">
        <div className="board__inner">
          <Wordmark />
          <div className="board__spacer" />
          <div className="board__tel" aria-label="Range board">
            {orgLabel && (
              <span>
                ORG <b>{orgLabel}</b>
              </span>
            )}
            <span>
              UTC <b>{utcClock(now)}</b>
            </span>
          </div>
          <div className="board__user">
            {user.avatarUrl && <img src={user.avatarUrl} alt="" />}
            <span className="mono">{user.login}</span>
            <button className="board__link" onClick={signOut}>
              Sign out
            </button>
          </div>
        </div>
      </header>
      <main className="page">{children}</main>
    </>
  );
}
