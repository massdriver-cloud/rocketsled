export type Provider = "github" | "gitlab" | "dev" | "password" | "open";

export interface User {
  id: string;
  provider: Provider;
  login: string;
  name: string;
  avatarUrl?: string;
}

export type AccountStatus = "new" | "installing" | "needs_credentials" | "ready" | "failed";

export interface Account {
  id: string;
  name: string;
  organizationId: string;
  organizationName: string;
  status: AccountStatus;
  catalogId?: string;
  projectId?: string;
  baseEnvironmentId?: string;
  platforms: Record<string, string>;
  installLog?: string;
  installError?: string;
  createdAt: string;
}

export interface Me {
  user: User;
  account?: Account;
}

export interface Instruction {
  label: string;
  markdown: string;
}

export interface JsonSchema {
  title?: string;
  description?: string;
  type?: string;
  required?: string[];
  properties?: Record<
    string,
    JsonSchema & {
      message?: { pattern?: string };
      oneOf?: { const: unknown; title: string }[];
      enum?: unknown[];
      default?: unknown;
      examples?: unknown[];
      pattern?: string;
    }
  >;
}

export interface PlatformType {
  id: string;
  label: string;
  description: string;
  schema: JsonSchema;
  uiOrder?: string[];
  instructions: Instruction[];
}

export interface Catalog {
  id: string;
  name: string;
  cloud: string;
  tagline: string;
  description: string;
  platforms: PlatformType[];
  resourceTypes: string[];
  bundles: string[];
  bootstrap: { project: string; baseEnvironment: string; appComponent?: string };
}

export interface PlatformRequirement {
  type: PlatformType;
  resourceId?: string;
  satisfied: boolean;
}

export interface Preset {
  name: string;
  params: Record<string, unknown>;
}

export interface Component {
  slug: string;
  name: string;
  bundle: { name: string; version: string; presets: Preset[] };
}

export interface Project {
  id: string;
  name: string;
  description?: string;
  components: Component[];
  links: { from: string; to: string }[];
  environments: { id: string; name: string }[];
}

export type EnvironmentState = "deploying" | "running" | "failed" | "decommissioning";

export interface Environment {
  id: string;
  accountId: string;
  slug: string;
  name: string;
  environmentId: string;
  presets: Record<string, string>;
  runtime?: string;
  addons?: string[];
  state: EnvironmentState;
  createdAt: string;
  updatedAt: string;
}

export interface ProjectView {
  project: Project;
  catalog: Catalog;
  environments: Environment[];
}

export type RunStatus = "queued" | "running" | "succeeded" | "failed";

export interface Run {
  id: string;
  number: number;
  trigger: "create" | "deploy";
  status: RunStatus;
  message?: string;
  createdAt: string;
  updatedAt: string;
}

export interface InstanceResource {
  field: string;
  name: string;
  resourceType: string;
  payload: Record<string, unknown>;
  highlights: Highlight[];
}

export interface Highlight {
  field: string;
  title: string;
  value: string;
}

export interface PlatformInstance {
  id: string;
  componentSlug: string;
  componentName: string;
  name: string;
  bundleName: string;
  version: string;
  deployedVersion?: string;
  status: string;
  resources: InstanceResource[];
}

export interface PlatformEnvironment {
  id: string;
  name: string;
  projectId: string;
  instances: PlatformInstance[];
  unfulfilled: { instanceId: string; field: string; resourceType: string }[];
}

export interface EnvironmentView {
  environment: Environment;
  platform?: PlatformEnvironment;
  runs: Run[];
  gone: boolean;
}

export interface Skin {
  name: string;
  tagline?: string;
  logo?: string;
  favicon?: string;
  layout?: string;
  colorScheme?: string;
  copy?: Record<string, string>;
}

export interface Meta {
  providers: string[];
  baseUrl: string;
  mode: string;
  skin?: Skin;
}

export interface OfferedPreset {
  name: string;
  price?: string;
  summary?: string;
}

export interface OfferedComponent {
  slug: string;
  name: string;
  summary?: string;
  bundle: string;
  presets: OfferedPreset[];
}

export interface RuntimeOffer {
  id: string;
  name: string;
  summary?: string;
  project: string;
  components: OfferedComponent[];
  addons: OfferedComponent[];
}

export interface Offer {
  runtimes: RuntimeOffer[];
  limits: { appsPerPerson?: number; totalApps?: number; ttlHours?: number };
}
