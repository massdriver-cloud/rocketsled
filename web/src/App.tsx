import { useCallback, useEffect, useState } from "react";
import { BrowserRouter, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { api, ApiError } from "./api";
import type { Account, Me, Meta } from "./types";
import TenantApp from "./tenant/TenantApp";
import { Shell } from "./components/Shell";
import { Loading, Notice } from "./components/ui";
import { Login } from "./pages/Login";
import { Credentials, Start } from "./pages/Start";
import { Overview } from "./pages/Overview";
import { Environments } from "./pages/Environments";
import { NewEnvironment } from "./pages/NewEnvironment";
import { EnvironmentDetail } from "./pages/EnvironmentDetail";

// Session loads the signed-in user and account once, signs new users up, and
// routes anyone who has not finished setup to the Start flow.
function Session() {
  const [me, setMe] = useState<Me | null | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const location = useLocation();

  useEffect(() => {
    api
      .me()
      .then(async (m) => {
        if (!m.account) {
          try {
            m.account = await api.createAccount("");
          } catch (e) {
            setError((e as ApiError).message);
          }
        }
        setMe(m);
      })
      .catch(() => setMe(null));
  }, []);

  const onAccount = useCallback((a: Account) => setMe((m) => (m ? { ...m, account: a } : m)), []);

  if (me === undefined) {
    return (
      <main className="main main--narrow">
        <Loading />
      </main>
    );
  }
  if (me === null) {
    return <Navigate to={`/login?next=${encodeURIComponent(location.pathname + location.search)}`} replace />;
  }
  if (!me.account) {
    return (
      <Shell user={me.user}>
        <Notice tone="bad">Could not create your account: {error}</Notice>
      </Shell>
    );
  }
  const ready = me.account.status === "ready";
  if (!ready && location.pathname !== "/start") return <Navigate to="/start" replace />;

  return (
    <Shell user={me.user} account={me.account}>
      <Routes>
        <Route path="/start" element={<Start account={me.account} onAccount={onAccount} />} />
        <Route path="/" element={<Overview account={me.account} />} />
        <Route path="/environments" element={<Environments />} />
        <Route path="/environments/new" element={<NewEnvironment />} />
        <Route path="/environments/:id" element={<EnvironmentDetail />} />
        <Route path="/credentials" element={<Credentials account={me.account} onAccount={onAccount} standalone />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Shell>
  );
}

export default function App() {
  const [meta, setMeta] = useState<Meta | null>(null);
  useEffect(() => {
    api
      .meta()
      .then(setMeta)
      .catch(() => setMeta({ providers: [], baseUrl: "", mode: "" }));
  }, []);
  if (!meta) return null;
  if (meta.mode === "tenant") return <TenantApp meta={meta} />;
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="*" element={<Session />} />
      </Routes>
    </BrowserRouter>
  );
}
