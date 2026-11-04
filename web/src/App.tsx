import { useEffect, useState } from "react";
import { BrowserRouter, Navigate, Outlet, Route, Routes, useLocation } from "react-router-dom";
import { api, ApiError } from "./api";
import type { User } from "./types";
import { Shell } from "./components/Shell";
import { Loading } from "./components/ui";
import { Login } from "./pages/Login";
import { Stacks } from "./pages/Stacks";
import { NewStack } from "./pages/NewStack";
import { StackDetail } from "./pages/StackDetail";
import { Launch } from "./pages/Launch";
import { InstancePage } from "./pages/Instance";

function Protected() {
  const [user, setUser] = useState<User | null | undefined>(undefined);
  const location = useLocation();

  useEffect(() => {
    api
      .me()
      .then(setUser)
      .catch((e: ApiError) => {
        if (e.status === 401) setUser(null);
        else setUser(null);
      });
  }, []);

  if (user === undefined) {
    return (
      <main className="page">
        <Loading label="Checking your session" />
      </main>
    );
  }
  if (user === null) {
    const next = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?next=${next}`} replace />;
  }
  return (
    <Shell user={user}>
      <Outlet />
    </Shell>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route element={<Protected />}>
          <Route path="/" element={<Stacks />} />
          <Route path="/stacks/new" element={<NewStack />} />
          <Route path="/stacks/:id" element={<StackDetail />} />
          <Route path="/stacks/:id/launch" element={<Launch />} />
          <Route path="/instances/:id" element={<InstancePage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}
