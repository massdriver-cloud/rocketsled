# Rocketsled

Rocketsled is the "pick a platform" front door to [Massdriver](https://massdriver.cloud).
Someone signs in, chooses a catalog (AWS Serverless to start), connects their cloud
account, and gets a working platform with production-shaped environments they size by
choosing presets. Underneath, every account is a real Massdriver organization they can
take over whenever they outgrow the simple view.

## How it works

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

## Tenant mode

A PaaS operator runs Rocketsled as a branded website over templates that already
exist in their own Massdriver organization. Nothing is installed and customers bring
no cloud account. With `massdriver.account_mode: tenant`:

- `skin` names a directory with `skin.json` (name, tagline, logo, layout, design
  tokens, copy) and optional CSS. The whole UI wears it.
- `paas.runtimes` lists template environments (`project`, `template`). A customer
  picks one, and the server forks it with the template's defaults.
- Components already deployed in the template are shared by every app. The rest
  deploy into each app, except `paas.addons`, which deploy only when picked. An
  app can add or remove an add-on later; the components linked to it are
  redeployed so they pick up or drop the connection.
- `paas.pinned` (and `runtimes[].pinned`) set params over every preset;
  `{{environment_id}}`, `{{app}}` and `{{runtime}}` are filled in.
- `paas.plans` labels preset cards with a price and a sentence.
- `paas.highlights` lists, per resource type, the payload fields the app page shows
  and their order (`website: [url]`). Other types show their first five fields in
  schema order. Values starting with `https://` are links.
- `paas.limits` caps apps per person and in total, restricts presets and add-ons,
  and removes apps after a `ttl`.
- `auth.providers.open: {}` lets anyone sign up with a name. An `invite_code` gates it.

Each app's owner and picks are written on its Massdriver environment, so a server
that loses its database still shows people their apps and finishes creates it was
interrupted in. See [massdriver-catalogs/aws examples/rocketsled](https://github.com/massdriver-catalogs/aws/tree/main/examples/rocketsled)
for two PaaSes built this way.

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

All routes under `/api/v1` require the session cookie set by the OAuth flow; routes that
need an account return 412 until signup is complete.

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v1/me` | User and account |
| POST | `/api/v1/account` | Sign up |
| GET | `/api/v1/catalogs` | Installable catalogs |
| POST | `/api/v1/account/install` | Install a catalog (`{catalogId}`), returns 202 |
| GET | `/api/v1/account` | Account status and install log |
| GET, POST | `/api/v1/account/platforms[/{type}]` | Credential requirements; provide one (`{name, payload}`) |
| GET | `/api/v1/project` | Project, components with presets, environments |
| GET, POST | `/api/v1/environments` | List, create (`{slug, name, presets}`), returns 202 |
| GET, DELETE | `/api/v1/environments/{id}` | Live status with resources and runs; destroy |
| POST | `/api/v1/environments/{id}/deploys` | Redeploy, returns 202 |
| POST | `/api/v1/environments/{id}/addons` | Tenant mode: add an add-on (`{component, preset}`), returns 202 |
| DELETE | `/api/v1/environments/{id}/addons/{component}` | Tenant mode: remove an add-on and its data, returns 202 |

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
