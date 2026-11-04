export type Provider = "github" | "gitlab" | "dev";

export interface User {
  id: string;
  provider: Provider;
  login: string;
  name: string;
  avatarUrl?: string;
}

export interface Meta {
  providers: Provider[];
  baseUrl: string;
}

export interface Repo {
  provider: Provider;
  externalId: string;
  fullName: string;
  url: string;
  defaultBranch: string;
  private: boolean;
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
  environments: { id: string; name: string }[];
}

export interface ProjectLink {
  id: string;
  baseEnvironment: string;
  appComponent?: string;
  imageTagParam: string;
  imageRegistry?: string;
  copySecrets: boolean;
}

export interface LinkableProject {
  link: ProjectLink;
  project: Project;
  appComponent: string;
}

export interface Stack {
  id: string;
  name: string;
  repo: Repo;
  path: string;
  projectId: string;
  createdBy: string;
  createdAt: string;
}

export type InstanceState = "launching" | "running" | "decommissioning" | "failed";

export interface Instance {
  id: string;
  stackId: string;
  slug: string;
  name: string;
  branch: string;
  environmentId: string;
  presets: Record<string, string>;
  state: InstanceState;
  createdAt: string;
  updatedAt: string;
}

export interface StackResponse {
  stack: Stack;
  instances: Instance[];
  deployToken?: string;
}

export type DeployStatus = "queued" | "building" | "running" | "succeeded" | "failed";
export type DeployTrigger = "launch" | "manual" | "webhook" | "ci";

export interface Deploy {
  id: string;
  instanceId: string;
  number: number;
  trigger: DeployTrigger;
  ref: string;
  sha: string;
  imageTag: string;
  image?: string;
  status: DeployStatus;
  message?: string;
  log?: string;
  platformDeploymentIds: string[];
  createdAt: string;
  updatedAt: string;
}

export type PackageStatus = "INITIALIZED" | "PROVISIONED" | "DECOMMISSIONED" | "FAILED" | string;

export interface PlatformInstance {
  id: string;
  componentSlug: string;
  name: string;
  bundleName: string;
  version: string;
  deployedVersion?: string;
  status: PackageStatus;
}

export interface Environment {
  id: string;
  name: string;
  projectId: string;
  instances: PlatformInstance[];
  unfulfilled: { instanceId: string; field: string; resourceType: string }[];
}

export interface InstanceView {
  instance: Instance;
  environment?: Environment;
  deploys: Deploy[];
  gone: boolean;
}

export interface LaunchResponse {
  instance: Instance;
}
