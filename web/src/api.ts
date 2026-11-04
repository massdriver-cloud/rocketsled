import type {
  Deploy,
  InstanceView,
  LaunchResponse,
  LinkableProject,
  Meta,
  Repo,
  StackResponse,
  User,
} from "./types";

export class ApiError extends Error {
  status: number;
  field?: string;
  constructor(status: number, message: string, field?: string) {
    super(message);
    this.status = status;
    this.field = field;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { Accept: "application/json", ...(init?.body ? { "Content-Type": "application/json" } : {}), ...init?.headers },
    credentials: "same-origin",
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let body: unknown = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = null;
  }
  if (!res.ok) {
    const err = (body ?? {}) as { error?: string; field?: string };
    throw new ApiError(res.status, err.error ?? res.statusText, err.field);
  }
  return body as T;
}

export const api = {
  meta: () => request<Meta>("/api/v1/meta"),
  me: () => request<User>("/api/v1/me"),
  logout: () => request<void>("/api/v1/logout", { method: "POST" }),
  repos: () => request<Repo[]>("/api/v1/repos"),
  projects: () => request<LinkableProject[]>("/api/v1/projects"),
  project: (id: string) => request<LinkableProject>(`/api/v1/projects/${encodeURIComponent(id)}`),
  stacks: () => request<StackResponse[]>("/api/v1/stacks"),
  stack: (id: string) => request<StackResponse>(`/api/v1/stacks/${encodeURIComponent(id)}`),
  createStack: (input: { name: string; repoFullName: string; projectId: string; path: string }) =>
    request<StackResponse>("/api/v1/stacks", { method: "POST", body: JSON.stringify(input) }),
  deleteStack: (id: string) => request<void>(`/api/v1/stacks/${encodeURIComponent(id)}`, { method: "DELETE" }),
  launch: (stackId: string, input: { slug: string; name: string; branch: string; presets: Record<string, string> }) =>
    request<LaunchResponse>(`/api/v1/stacks/${encodeURIComponent(stackId)}/instances`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  instance: (id: string) => request<InstanceView>(`/api/v1/instances/${encodeURIComponent(id)}`),
  deploy: (id: string, input: { ref?: string; sha?: string; imageTag?: string; message?: string } = {}) =>
    request<Deploy>(`/api/v1/instances/${encodeURIComponent(id)}/deploys`, { method: "POST", body: JSON.stringify(input) }),
  destroy: (id: string) => request<void>(`/api/v1/instances/${encodeURIComponent(id)}`, { method: "DELETE" }),
};

export function loginUrl(provider: string, next = "/") {
  return `/auth/${provider}/login?next=${encodeURIComponent(next)}`;
}
