import { NavLink, useNavigate } from "react-router-dom";
import { api } from "../api";
import type { Account, User } from "../types";

export function Shell({ user, account, children }: { user: User; account?: Account; children: React.ReactNode }) {
  const navigate = useNavigate();
  async function signOut() {
    await api.logout();
    navigate("/login", { replace: true });
  }
  const ready = account?.status === "ready";
  return (
    <div className="app">
      <aside className="side">
        <NavLink to="/" className="brand">
          <span className="brand__mark" aria-hidden="true">
            <svg width="14" height="14" viewBox="0 0 14 14">
              <path d="M2 11.5 7 2.5l5 9z" fill="#fff" />
            </svg>
          </span>
          Rocketsled
        </NavLink>
        {ready && (
          <nav className="nav" aria-label="Main">
            <NavLink to="/" end>
              Overview
            </NavLink>
            <NavLink to="/environments">Environments</NavLink>
            <NavLink to="/credentials">Credentials</NavLink>
          </nav>
        )}
        <div className="side__foot">
          {account && (
            <div className="side__org">
              <b>{account.name}</b>
              <span className="mono">{account.organizationId}</span>
            </div>
          )}
          <div>
            {user.login} ·{" "}
            <button className="linkbtn" onClick={signOut}>
              Sign out
            </button>
          </div>
        </div>
      </aside>
      <main className="main">{children}</main>
    </div>
  );
}
