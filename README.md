# Rocketsled

Build your own Heroku or Vercel on [Massdriver](https://massdriver.cloud).

Rocketsled is a branded front door for an internal or public PaaS. Customers sign in,
pick a runtime, choose plans and add-ons, push images from CI, promote builds through
stages, and get a preview app for every pull request. Your platform team never builds a
console: everything a customer gets is a Massdriver bundle, deployed by Massdriver, in an
organization you own.

<p>
  <img src="docs/screenshots/fauxroku/02-apps.png" alt="Fauxroku apps grouped by pipeline stage" width="49%">
  <img src="docs/screenshots/fauxcel/12-deployments-dark.png" alt="Fauxcel deployments in its dark skin" width="49%">
</p>

Both of those are the same server. **Fauxroku** is a Heroku-style PaaS on ECS with dynos,
Postgres and a key-value store. **Fauxcel** is a Vercel-style PaaS on Lambda with
functions, static sites, KV and blob storage. Each is one config file and one skin over
templates in a Massdriver organization; see
[massdriver-catalogs/aws examples/rocketsled](https://github.com/massdriver-catalogs/aws/tree/main/examples/rocketsled).

## What customers get

**Create an app.** Pick a runtime, a pipeline stage, a plan for each component, and the
add-ons it needs. Plans are the bundles' own presets, labelled with a price and a sentence.

<p>
  <img src="docs/screenshots/fauxroku/03-new-app.png" alt="Fauxroku: create a new app" width="49%">
  <img src="docs/screenshots/fauxcel/03-new.png" alt="Fauxcel: new deployment" width="49%">
</p>

**See it run.** The app's address shows up once everything answers. Each component lists
its plan, bundle version, status and the fields of its resources worth knowing, in the
order the operator picked.

<p>
  <img src="docs/screenshots/fauxroku/04-app-overview.png" alt="Fauxroku: app overview with resources" width="49%">
  <img src="docs/screenshots/fauxcel/10-static-site.png" alt="Fauxcel: a static site" width="49%">
</p>

**Add and remove add-ons later.** Adding one deploys it, then redeploys whatever is linked
to it so it picks up the connection. Removing one asks first, because it deletes the data.

<p>
  <img src="docs/screenshots/fauxroku/05-add-ons.png" alt="Fauxroku: add-ons on a running app" width="49%">
  <img src="docs/screenshots/fauxcel/05-storage.png" alt="Fauxcel: storage add-ons" width="49%">
</p>

**Push to deploy.** A build releases an image with a deploy token that can do nothing
else. Every release is numbered, and any older one is a click away. The page shows the
GitHub Actions step to paste.

<p>
  <img src="docs/screenshots/fauxroku/06-releases.png" alt="Fauxroku: releases with roll back" width="49%">
  <img src="docs/screenshots/fauxcel/06-releases.png" alt="Fauxcel: releases" width="49%">
</p>

<p>
  <img src="docs/screenshots/fauxroku/09-deploy-tokens.png" alt="Fauxroku: deploy tokens" width="49%">
  <img src="docs/screenshots/fauxcel/09-deploy-tokens.png" alt="Fauxcel: deploy tokens" width="49%">
</p>

**Promote, don't rebuild.** Apps sit in pipeline stages. Promoting copies the build and
config that ran in one stage to the next, while the target keeps its own plans, add-ons,
domains and data.

<p>
  <img src="docs/screenshots/fauxroku/07-pipeline.png" alt="Fauxroku: promote staging to production" width="49%">
  <img src="docs/screenshots/fauxcel/07-pipeline.png" alt="Fauxcel: promote a preview to production" width="49%">
</p>

**A preview for every pull request.** One call from CI makes a short-lived app with cheap
plans and the branch's image, and every later push only releases. Closing the pull
request deletes it, and a preview nobody has pushed to within its TTL is removed.

<p>
  <img src="docs/screenshots/fauxroku/08-preview-app.png" alt="Fauxroku: a preview app for pull request 142" width="49%">
  <img src="docs/screenshots/fauxcel/08-preview-pr142.png" alt="Fauxcel: a preview deployment" width="49%">
</p>

**Know when it breaks.** Every deploy, release, promotion and add-on change is a numbered
run, and a failed one says why.

<p>
  <img src="docs/screenshots/fauxroku/11-crashed.png" alt="Fauxroku: a crashed app with its error" width="49%">
  <img src="docs/screenshots/fauxcel/11-error.png" alt="Fauxcel: a failed deployment" width="49%">
</p>

<p>
  <img src="docs/screenshots/fauxroku/10-building.png" alt="Fauxroku: an app building" width="49%">
  <img src="docs/screenshots/fauxroku/12-activity.png" alt="Fauxroku: activity" width="49%">
</p>

**Your brand, not ours.** A skin sets the name, logo, layout, design tokens and copy, so
"apps" can be "deployments" and "plans" can be "dyno types".

<p>
  <img src="docs/screenshots/fauxroku/01-sign-in.png" alt="Fauxroku sign in" width="49%">
  <img src="docs/screenshots/fauxcel/01-sign-in.png" alt="Fauxcel sign in" width="49%">
</p>

<p>
  <img src="docs/screenshots/fauxcel/02-deployments.png" alt="Fauxcel deployments in its light skin" width="49%">
  <img src="docs/screenshots/fauxcel/13-deployment-dark.png" alt="Fauxcel deployment page in its dark skin" width="49%">
</p>

<p><img src="docs/screenshots/fauxcel/04-deployment-overview.png" alt="Fauxcel: deployment overview" width="49%"></p>

## How it maps to Massdriver

| Rocketsled | Massdriver |
|---|---|
| Platform layer (network, DNS, cluster) | One project, deployed once; its resources are granted to every runtime and pinned as environment defaults |
| Runtime (Node.js, Functions, Static) | A `template` environment whose canvas holds every component an app of that kind could use |
| App or environment | A fork of the template, with only the picked components deployed |
| Plan | A preset from the bundle's `params.examples` |
| Add-on | A component linked on the canvas and left undeployed until picked |
| Release | The image param set on the app component, deploying only that instance |
| Promotion | `copyInstance` plus the deployed bundle version, keeping the target's own fields |

Each app's owner and choices are written on its Massdriver environment, so a server that
loses its database still finds every app, and finishes any create a restart interrupted.

## Configuring a PaaS

A PaaS runs in tenant mode: Rocketsled serves templates that already exist in the
operator's Massdriver organization. Nothing is installed and customers bring no cloud
account. With `massdriver.account_mode: tenant`:

- `skin` names a directory with `skin.json` (name, tagline, logo, layout, design
  tokens, copy) and optional CSS. The whole UI wears it.
- `paas.runtimes` lists template environments (`project`, `template`). A customer
  picks one, and the server forks it with the template's defaults.
- A runtime with `app_projects: true` gives each customer app its own Massdriver
  project, made from the runtime's template (`createProjectFromEnvironment`), with
  every default of the runtime's template shared with it and pinned on its own
  `template` environment. The app's environments, stages and previews fork that
  template, so they share one canvas and nothing else.
- Components already deployed in the template are shared by every app. The rest
  deploy into each app, except `paas.addons`, which deploy only when picked. An
  app can add or remove an add-on later; the components linked to it are
  redeployed so they pick up or drop the connection.
- `paas.pinned` (and `runtimes[].pinned`) set params over every preset;
  `{{environment_id}}`, `{{app}}` and `{{runtime}}` are filled in.
- A runtime's `app_component` and `image_param` say where releases land. A build
  releases an image with a deploy token; the image wins over pinned params from
  then on, so a pinned welcome image only shows until the first release.
- `paas.platform` names a project deployed once (`project`, `environment`). The server
  deploys each of its `components` that isn't provisioned, in order, with its preset
  and `params` (`{{platform_domain}}` is `domain`), then grants each of its `shares`
  (`component.field`) to every runtime project and pins it on every runtime template.
  It checks again every five minutes, and new apps wait while it isn't running.
- `paas.previews` (`presets`, `ttl`) lets a build make a preview per branch or pull
  request: `PUT /api/v1/previews/pr42 {image, runtime}` with a deploy token makes it
  with those presets the first time and releases to it after that. A preview whose
  last release is older than `ttl` is removed; nothing else ever is.
- `paas.plans` labels preset cards with a price and a sentence.
- `paas.highlights` lists, per resource type, the payload fields the app page shows
  and their order (`website: [url]`). Other types show their first five fields in
  schema order. Values starting with `https://` are links.
- `paas.domains` (`component`, `param`, `target_field`) lets customers point their
  own hostnames at an app. They land on that component's param, which keeps them
  over a pinned value, and the app page shows the CNAME to create, pointing at
  `target_field` of the component's resource.
- `paas.pipeline.stages` (`id`, `name`, `presets`) put apps in stages, listed in
  pipeline order. Picking a stage fills in its presets and switches on the add-ons
  it names. `paas.pipeline.promote` names the components a promotion copies from
  an app to one in a later stage of the same runtime: their config and bundle
  version move, while the target keeps its own presets, pinned params and add-ons,
  and only what changed deploys.
- `paas.limits` caps apps per person and in total, restricts presets and add-ons,
  and removes apps after a `ttl`.
- `auth.providers.open: {}` lets anyone sign up with a name. An `invite_code` gates it.

## Install mode

Without a PaaS operator in the middle, Rocketsled can also be the "pick a platform" front
door to Massdriver itself: someone signs in, chooses a catalog, connects their own cloud
account, and gets production-shaped environments in a Massdriver organization they can
take over whenever they outgrow the simple view.

1. **Sign up.** A signup is a Massdriver organization. In `organization` mode Rocketsled
   creates one (`rs-<adjective>-<noun>-<nn>`) with a user token that may create
   organizations, then mints an admin service account in it and keeps that token,
   encrypted, as the account's credential. In `shared` mode (development) every account
   borrows one configured organization.
2. **Install a catalog.** A catalog is a directory with `platforms/`, `resource-types/`,
   `bundles/` and `bootstrap/`. Rocketsled shells out to the `mass` CLI to publish the
   platforms, resource types and bundles into the account's organization, then runs
   `tofu apply` on the bootstrap, which creates the project, its environments and the
   links between components. The OpenTofu state is stored with the account so a retry
   converges. This happens once per account.
3. **Connect the cloud.** Each platform type the catalog lists (an AWS IAM role, say)
   becomes a form generated from its JSON Schema, with the type's own instructions next
   to it. The submitted credential is created as a resource in the organization and
   pinned as an environment default on every environment.
4. **Create environments.** An environment forks the bootstrapped base environment,
   applies one preset per component (the bundle's own `params.examples`), pins the
   credentials, and deploys. The environment page shows each component's status and the
   addresses and names its resources expose.

## Run it

```sh
cp rocketsled.example.yaml rocketsled.yaml
printf 'MASSDRIVER_ORGANIZATION_ID=...\nMASSDRIVER_API_KEY=md_...\n' > .env   # gitignored
make                           # builds the UI and the binary
bin/rocketsled check           # credential, catalogs
bin/rocketsled serve
```

The server reads `.env` (or `-env-file`) before the config. Without an explicit
credential it falls back to the `mass` CLI profile named by `MASSDRIVER_CREDENTIAL_PATH`
and `MASSDRIVER_PROFILE` (default `~/.config/massdriver/config.yaml`, `default`).
`mass` and `tofu` must be on the PATH for installs.

## Catalogs

`catalogs/aws-serverless` is the first one: `shared-vpc`, `http-api`, `dynamodb-table`
and `lambda-function` lifted from
[aws-software-factory](https://github.com/massdriver-catalogs/aws-software-factory),
with the real schemas, presets and resource contracts but stub IaC (`random_pet`) so
deploys cost nothing while the product is being designed. Its bootstrap creates the
**AWS Serverless Platform** project with a **Production** environment and all four
components linked.

`catalog.yaml` declares the id, cloud, platforms, publish order and what the bootstrap
produces. Adding a catalog is adding a directory. A catalog's `paths` can point at
platforms, resource types and bundles outside it, and `catalogs.dirs` loads catalogs
from directories other than `catalogs/`; see
[docs/CATALOG_AUTHORING.md](docs/CATALOG_AUTHORING.md).

`catalogs.include: [<id>, ...]` limits what new accounts can install to those catalogs;
the others are not parsed beyond their `id`, except one an existing account already
installed, which stays loaded for that account. With a single catalog the setup page skips the choice and
offers just that platform.

## REST API

All routes under `/api/v1` require the session cookie set by the sign-in flow; routes that
need an account return 412 until signup is complete. The release routes also take a
deploy token as a bearer token, which is good for nothing else.

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v1/me` | User and account |
| POST | `/api/v1/account` | Sign up |
| GET | `/api/v1/catalogs` | Installable catalogs |
| POST | `/api/v1/account/install` | Install a catalog (`{catalogId}`), returns 202 |
| GET | `/api/v1/account` | Account status and install log |
| GET, POST | `/api/v1/account/platforms[/{type}]` | Credential requirements; provide one (`{name, payload}`) |
| GET | `/api/v1/project` | Project, components with presets, environments |
| GET, POST | `/api/v1/environments` | List, create (`{slug, name, presets}`; tenant mode adds `runtime`, `addons`, `stage`, and `app` for a runtime with app projects), returns 202 |
| GET, DELETE | `/api/v1/environments/{id}` | Live status with resources and runs; destroy |
| POST | `/api/v1/environments/{id}/deploys` | Redeploy, returns 202 |
| GET, POST | `/api/v1/account/tokens` | Deploy tokens; create one (`{name}`), its secret is returned once |
| DELETE | `/api/v1/account/tokens/{id}` | Revoke a deploy token |
| GET, POST | `/api/v1/environments/{id}/releases` | Releases, newest first; release an image (`{image, description}`), returns 202. `{id}` may be the slug. Accepts `Authorization: Bearer <deploy token>` |
| GET, POST | `/api/v1/apps` | Tenant mode: apps with their own project and their environments; make one (`{name, runtime}`) |
| DELETE | `/api/v1/apps/{id}` | Tenant mode: delete an app's project once it has no environments |
| PUT, DELETE | `/api/v1/previews/{name}` | Tenant mode: make a preview and release to it (`{image, description, runtime, app}`), or remove it (`?app=` for a preview inside an app). Takes a deploy token, returns 202 |
| POST | `/api/v1/environments/{id}/addons` | Tenant mode: add an add-on (`{component, preset}`), returns 202 |
| DELETE | `/api/v1/environments/{id}/addons/{component}` | Tenant mode: remove an add-on and its data, returns 202 |
| GET, PUT | `/api/v1/environments/{id}/domains` | Tenant mode: custom domains with the CNAME each needs; replace them (`{domains}`), returns 202 |
| POST | `/api/v1/environments/{id}/promote` | Tenant mode: promote to an app in a later stage (`{to}`), returns 202 |

Errors are `{"error": "...", "field": "..."}` with 422 for validation, 404, 409, and 502
when Massdriver rejected a call.

## Development

```sh
make hooks       # once per clone: pre-commit lint, pre-push make check
make check       # everything CI runs
make fmt         # gofumpt, goimports, prettier
make test        # go test -race ./...
make lint        # golangci-lint + eslint + tsc
make generate    # regenerate domain mocks (moq)
make ui-dev      # Vite dev server on :5173, proxying to :8080
```

```
cmd/rocketsled        entrypoint: serve, check
internal/config       rocketsled.yaml, .env, mass profile fallback
internal/domain       entities, ports (Platform, SCM, Store, Installer), Service, mocked tests
internal/catalog      loads catalogs from disk
internal/installer    mass + tofu shell-out implementing domain.Installer
internal/massdriver   GraphQL adapter implementing domain.Platform
internal/scm          GitHub, GitLab and dev sign-in
internal/store        SQLite store (credentials encrypted) and an in-memory store
internal/httpapi      REST handlers, session auth, embedded UI
web                   Vite + React UI, embedded into the binary at build time
catalogs              installable catalogs
```

Ideas that are not built yet are in [TODO.md](TODO.md).
