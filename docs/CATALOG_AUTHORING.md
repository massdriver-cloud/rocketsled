# Building a Rocketsled catalog

Rocketsled is a small PaaS on top of Massdriver. A customer signs in, picks a
catalog, gets a Massdriver organization with that catalog published into it, pastes
one cloud credential, and from then on creates environments and deploys with a
Railway-style UI. They never see Massdriver until they outgrow Rocketsled, at which
point the organization is handed to them intact.

A catalog is the whole product for that customer: the credential type they must
provide, the resource types and bundles that get published into their organization,
and an OpenTofu bootstrap that lays out their first project. Everything in it is
ordinary Massdriver material. The Rocketsled-specific parts are `catalog.yaml`,
`bootstrap/`, and a handful of conventions the bundles have to follow so the UI can
drive them without a human.

The reference implementation is `catalogs/aws-serverless` in
github.com/massdriver-cloud/rocketsled. Copy its shape.

## Layout

```
catalogs/<id>/
  catalog.yaml
  README.md
  LICENSE / NOTICE            when material is copied from another catalog
  platforms/<type>/
    massdriver.yaml           a resource type whose schema becomes the credential form
    icon.svg
    instructions/<Label>.md   one markdown file per way of obtaining the credential
  resource-types/<type>/
    massdriver.yaml
  bundles/<name>/
    massdriver.yaml
    src/                      OpenTofu
    README.md, operator.md, icon.svg, CHANGELOG.md
  bootstrap/
    versions.tf, main.tf, outputs.tf
    .terraform.lock.hcl
```

## catalog.yaml

```yaml
id: aws-serverless            # directory name; lowercase, hyphens
name: AWS Serverless          # catalog card title
cloud: aws                    # aws | azure | gcp | kubernetes
tagline: Lambda functions behind an HTTP API, with DynamoDB and a private VPC.
description: >-
  Two or three sentences for the catalog card. Say what it costs when idle.

platforms:                    # credential types the customer must provide
  - aws-iam-role
resource_types:               # published in this order, before bundles
  - aws-vpc
  - aws-http-api
bundles:                      # published in this order
  - shared-vpc
  - http-api
  - lambda-function

bootstrap:
  project: serverless         # the massdriver_project identifier in bootstrap/
  base_environment: production  # the key in bootstrap's `environments` output
  app_component: app          # component whose image follows the customer's app
  image_param: image_uri      # the param on that component that takes the image
```

The loader refuses a catalog when `id`, `name`, `bootstrap.project` or
`bootstrap.base_environment` is missing, when `bootstrap/` is missing, or when any
listed platform, resource type or bundle has no `massdriver.yaml`. `app_component`
and `image_param` are stored but not yet acted on; set them anyway, the app loop is
the next feature and will read them.

## platforms/: the credential form

A platform is a Massdriver resource type with two extra expectations.

```yaml
name: aws-iam-role
version: 0.0.0
label: AWS IAM Role

ui:
  connectionOrientation: environmentDefault
  environmentDefaultGroup: credentials
  instructions:
    - label: AWS CLI
      path: ./instructions/AWS CLI.md
    - label: AWS Console
      path: ./instructions/AWS Console.md
  schema:
    ui:order: [external_id, arn, "*"]

schema:
  title: AWS IAM Role
  description: One sentence shown above the form.
  type: object
  additionalProperties: false
  required: [arn, external_id]
  properties:
    arn:
      title: Role ARN
      description: Where to find it.
      type: string
      pattern: ^arn:aws:iam::[0-9]{12}:role/.+$
      message: { pattern: "Must be an IAM role ARN." }
    external_id:
      title: External ID
      type: string
      $md.sensitive: true
```

What Rocketsled does with it:

- The `schema` becomes a form. Keep it flat: string, number, boolean and enum
  properties only, no nested objects or arrays. Every property needs `title` and
  `description`, because that is all the customer sees. `required` is enforced
  server-side; `pattern` and `message.pattern` are shown as validation hints.
- `ui.schema["ui:order"]` sets field order.
- Each `ui.instructions` entry becomes a tab of rendered markdown beside the form.
  Write them for someone who has never opened the cloud console. Each must end with
  "paste these values into the form". The AWS ones in the reference catalog are the
  bar: choose a secret, create the role with a trust policy on Massdriver's account
  `308878630280` plus that external ID, attach a policy, copy the ARN.
- The submitted values are stored as a resource of this type in the customer's
  organization, shared with every environment, and pinned as an environment default
  on the base environment and every environment the customer creates.
  `connectionOrientation: environmentDefault` is what makes that pinning work.
- Every bundle in the catalog must declare a required dependency on this type so
  the environment default satisfies it. Name it the way Massdriver's own bundles do
  for that cloud (`aws_authentication`, `azure_service_principal`,
  `gcp_authentication`, `kubernetes_cluster`), so the generated
  `_massdriver_variables.tf` and any copied IaC line up.

One platform per catalog is the goal. The UI supports several, but each one is
another thing a customer has to do before they see anything deploy.

## resource-types/

Plain Massdriver resource types at `version: 0.0.0` with
`ui.connectionOrientation: link`. Bundles reference them as `name@0.0.0`. Publish
only what a bundle in the catalog produces or depends on. A resource type that
nothing produces is fine when it lets an optional dependency resolve (the reference
catalog ships `aws-user-pool` for that reason) but say so in the README.

Resource schemas are what the environment page shows as "resource highlights", so
put the values a developer wants to copy (endpoint, hostname, table name) at the
top level with clear titles. Keys that look like secrets (`password`, `secret`,
`token`, `key`) are hidden on that page.

## bundles/

Ordinary Massdriver bundles, with these rules.

**Presets are the configuration UI.** Rocketsled never shows a params form. When a
customer creates an environment they pick one preset per component, and that
preset's params are copied onto the new instance. So:

- Every bundle that appears as a bootstrap component needs `params.examples`, each
  with a `__name`. Two or three presets is right: a free or near-free one, a
  production one, maybe one in between.
- The first preset is the default and must be the cheapest that still works.
- A preset must be a complete, valid params document. It is applied as a full
  override, not merged with defaults. Run `mass bundle lint` to catch misses.
- Preset names are shown as radio cards. Keep them short: "Free Tier", "Production".
- Write `params.properties` descriptions for a developer, not an operator, and
  bold anything that costs money (`***Billed per GB-second.***`). They are the
  copy Massdriver shows if the customer ever opens the organization.

**Dependencies must resolve inside the catalog.** `required: true` dependencies are
satisfied either by a bootstrap link from another component or by the platform
environment default. Anything else must be `required: false`, and the IaC has to
behave when it is absent.

**Use `dependencies:` and `resources:` with `resource_type: name@0.0.0`.** The
`connections:`/`artifacts:` spelling is deprecated and the mass CLI warns on it.

**Pin the provisioner** (`provisioner: opentofu:1.10`). Steps run in Massdriver, not
on the Rocketsled server; only the bootstrap runs locally.

**Every bundle gets published into every customer's organization under the catalog's
bundle name**, so names should read well in a project (`http-api`, `shared-vpc`,
`postgres`), not be prefixed with the catalog or cloud.

### Stub versus real IaC

The reference catalog's `src/` directories are stubs: `random_pet` and `random_id`
stand in for cloud resources, and each bundle emits a `massdriver_resource` whose
payload is shaped exactly like the real resource type schema (a plausible endpoint,
ARN, region, and so on). Everything else, the `massdriver.yaml`, schemas, presets,
docs and icons, is real. That lets the whole product be exercised, including
environment creation, deploys and the resource highlights, without a cloud bill.

For a new catalog, build it stubbed first and prove the install and an environment
deploy end to end against the dev organization. Then swap `src/` for the real
modules bundle by bundle. Keep the stubbed copy as `<id>-demo` if it is worth
keeping as a demo; the loader lists every directory under `catalogs/` that has a
`catalog.yaml`.

A stub must:

- declare `random` and `massdriver` providers only
- read every variable the generated `_massdriver_variables.tf` declares, so the
  bundle builds
- produce every `required` field of each resource it declares, with realistic
  values, so consumers' IaC and the UI are tested against the real shape
- never produce anything a customer could mistake for a real endpoint without a
  stub-looking marker; the reference uses account `123456789012`

## bootstrap/: the first project

An OpenTofu root module run by Rocketsled with the massdriver provider against the
customer's organization. It is applied once at install and re-applied on retry with
the saved state, so it has to converge.

```hcl
terraform {
  required_version = ">= 1.5"
  required_providers {
    massdriver = { source = "massdriver-cloud/massdriver", version = "~> 2.4" }
  }
}
provider "massdriver" {}   # MASSDRIVER_API_KEY and MASSDRIVER_ORGANIZATION_ID are set for it
```

Rules:

- Create one `massdriver_project` whose `identifier` equals
  `catalog.yaml#bootstrap.project`, one `massdriver_environment` per environment
  (at least the base one), one `massdriver_component` per bundle you want in every
  environment, and `massdriver_component_link`s whose `from_field`/`to_field` are
  the `resources:`/`dependencies:` field names in the bundles.
- Component `identifier`s become the slugs in the UI and in instance IDs
  (`<environment id>-<slug>`, for example `serverless-production-app`). Keep them
  short and generic: `vpc`, `api`, `db`, `app`. Give each a human `name`
  ("Network", "Application"); that is what the environment page shows.
- Environment identifiers are 1 to 20 lowercase letters or digits. Project and
  environment names are what the customer sees if they ever open Massdriver.
- Required outputs, exactly these names and shapes:

  ```hcl
  output "project"      { value = massdriver_project.x.id }
  output "environments" { value = { production = massdriver_environment.production.id } }
  output "components"   { value = { for k, c in massdriver_component.this : k => c.id } }
  ```

  The `environments` map must contain the `bootstrap.base_environment` key.
- Commit a `.terraform.lock.hcl` generated with
  `tofu providers lock -platform=darwin_arm64 -platform=linux_amd64 -platform=linux_arm64`
  (add `-fs-mirror` pointing at a provider cache if GitHub downloads stall, which they
  do from some networks). Rocketsled copies only `*.tf` and the lock file into the
  working directory; no subdirectories, no local modules, no `.tfvars`.
- Do not deploy anything from the bootstrap. The base environment is a template:
  Rocketsled never deploys it. Customer environments are forks of it, with
  environment defaults copied, one preset applied per component, then deployed.
- Put nothing cloud-specific in it. Credentials arrive after the bootstrap, via the
  platform form.

The reference bootstrap is one of everything, linked producer to consumer, with a
single `production` base environment. That is the right shape for a first catalog.
A second environment in the bootstrap would also become a template; the UI only uses
the base one today.

## What Rocketsled does with the catalog

Knowing the sequence explains most of the rules above.

Install, in a goroutine on the Rocketsled server, with the organization's service
account in `MASSDRIVER_API_KEY`/`MASSDRIVER_ORGANIZATION_ID`:

1. For each platform, then each resource type:
   `mass repository create <name> -t resource-type` (failure ignored if it exists),
   then `mass resource-type publish .` from its directory.
2. For each bundle: `mass repository create <name> -t bundle`,
   `mass repository grant create <name> --all-projects`, then
   `mass bundle publish --bundle-directory .` with `--development` added unless the
   server's release channel is `stable`. The grant is required or
   `massdriver_component` in the bootstrap fails with "not granted to this project".
3. Copy `bootstrap/*.tf` and the lock file to `<data_dir>/accounts/<id>/bootstrap`,
   restore prior `terraform.tfstate` if any, `tofu init`, `tofu apply -auto-approve`,
   `tofu output -json`. The state is stored on the account.
4. Record project ID, base environment ID and component IDs on the account. Status
   becomes `needs_credentials`.

Credentials: the platform form posts to the API, the payload is validated against
the schema, created with `createResource` under the platform type, shared with
`createResourceGrant` (action `resource:export`, conditions `*`), and pinned with
`setEnvironmentDefault` on the base environment and all existing environments.
Status becomes `ready`.

Environment creation: `forkEnvironment` from the base with `copyEnvironmentDefaults`,
then for each component `copyInstance` from `<base env id>-<slug>` to the new
instance with the chosen preset as overrides, then `setEnvironmentDefault` for each
credential, then `deployEnvironment`. The UI polls instance statuses and shows each
component's resources from the deployed instances.

## Gotchas learned the hard way

- The GraphQL `Map` scalar must be sent as a JSON-encoded string. Objects are
  rejected with "Expected type Map". Only matters if you touch the Go client.
- Presets are read from `Instance.paramsSchema.examples` of the base environment's
  instances, which is a copy of the published bundle's `params.examples`. Changing
  presets means republishing and retrying the install; it does not require a new
  bootstrap.
- Organization IDs from `createOrganization` are at most 20 characters, lowercase
  and hyphens. Not a catalog concern, but it bounds environment and project IDs
  that get composed with them in names.
- Whether bundles are published as development or stable versions is a server
  setting (`catalogs.release_channel`), not a per-bundle choice. Version every
  bundle and resource type `0.0.0`.
- The massdriver provider downloads stall from GitHub's CDN on some networks.
  `scripts/fetch-providers.sh` pre-populates `data/plugin-cache` and the installer
  points `TF_PLUGIN_CACHE_DIR` at it.
- The catalog loader test `TestLoadsTheShippedCatalog` asserts exactly one catalog.
  A second catalog needs that test updated or its own assertions.

## Definition of done for a new catalog

1. `go test ./internal/catalog/...` passes with the catalog loaded.
2. `rocketsled check` passes (tools present, catalog loads).
3. Against the dev organization `rs-silver-whispering-waterfall` with the credential
   in the repo's gitignored `.env`: install from the Start page completes, the
   credential form accepts the platform values, an environment created with the
   default presets reaches all components `PROVISIONED`, and the environment page
   shows sensible resource highlights for each component.
4. `mass bundle lint` is clean for every bundle; no deprecation warnings on publish.
5. README in the catalog directory with the component table and what is stubbed.
6. If material is copied from another catalog, LICENSE and NOTICE carry the
   attribution (the reference catalog copies from
   github.com/massdriver-catalogs/aws-software-factory, Apache 2.0).

## What makes a good Rocketsled catalog

The customer is below Massdriver's ICP: a solo developer or a two-person team with
a side project and a personal cloud account. They will judge the catalog on the
first ten minutes.

- One credential. Idle cost near zero, and say so in the description.
- Three to six components. Network, data, ingress, an `app` runtime. Enough to
  look like a platform, few enough to understand on one page.
- An `app` component whose bundle takes a container image param and runs a
  placeholder until it gets one. That is where the push-to-deploy loop lands.
- Presets with real cost implications: "Free Tier" should genuinely fit the free
  tier; "Production" should be what you would actually run.
- Instructions a non-cloud person can follow for the credential.

Catalogs wanted next, in order: an Azure equivalent of aws-serverless (Functions or
Container Apps, Cosmos or Postgres flexible, Front Door or APIM, a service principal
platform), a GCP equivalent (Cloud Run, Firestore or Cloud SQL, an API gateway or
load balancer, a service account platform), and a Kubernetes three-tier catalog
(VPC, managed cluster, a landing-zone bundle that mints namespaced credentials,
Helm-deployed Postgres or MariaDB and MinIO, an app). An `aws-containers` catalog
(ECS or App Runner behind an ALB with RDS) would also pull its weight.
