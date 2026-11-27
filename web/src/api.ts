import type { Meta, Offer, User, Account, Catalog, Environment, EnvironmentView, Me, PlatformRequirement, ProjectView, Run } from "./types";

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
  passwordLogin: (login: string, password: string) =>
    request<User>("/auth/password", { method: "POST", body: JSON.stringify({ login, password }) }),
  offer: () => request<Offer>("/api/v1/offer"),
  createApp: (input: { slug: string; name: string; runtime: string; addons: string[]; presets: Record<string, string> }) =>
    request<Environment>("/api/v1/environments", { method: "POST", body: JSON.stringify(input) }),
  me: () => request<Me>("/api/v1/me"),
  logout: () => request<void>("/api/v1/logout", { method: "POST" }),
  createAccount: (name: string) => request<Account>("/api/v1/account", { method: "POST", body: JSON.stringify({ name }) }),
  account: () => request<Account>("/api/v1/account"),
  catalogs: () => request<Catalog[]>("/api/v1/catalogs"),
  install: (catalogId: string) => request<Account>("/api/v1/account/install", { method: "POST", body: JSON.stringify({ catalogId }) }),
  platforms: () => request<PlatformRequirement[]>("/api/v1/account/platforms"),
  providePlatform: (type: string, name: string, payload: Record<string, unknown>) =>
    request<Account>(`/api/v1/account/platforms/${encodeURIComponent(type)}`, { method: "POST", body: JSON.stringify({ name, payload }) }),
  project: () => request<ProjectView>("/api/v1/project"),
  environments: () => request<Environment[]>("/api/v1/environments"),
  createEnvironment: (input: { slug: string; name: string; presets: Record<string, string> }) =>
    request<Environment>("/api/v1/environments", { method: "POST", body: JSON.stringify(input) }),
  environment: (id: string) => request<EnvironmentView>(`/api/v1/environments/${encodeURIComponent(id)}`),
  deploy: (id: string) => request<Run>(`/api/v1/environments/${encodeURIComponent(id)}/deploys`, { method: "POST" }),
  destroy: (id: string) => request<Environment>(`/api/v1/environments/${encodeURIComponent(id)}`, { method: "DELETE" }),
};

export function loginUrl(provider: string, next = "/") {
  return `/auth/${provider}/login?next=${encodeURIComponent(next)}`;
}

export function timeAgo(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "";
  const s = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h}h ago`;
  return `${Math.round(h / 24)}d ago`;
}
