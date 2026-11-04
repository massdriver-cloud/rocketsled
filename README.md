# Rocketsled

A small platform-as-a-service that runs on top of [Massdriver](https://massdriver.cloud).
Developers sign in with GitHub or GitLab, link a repository to a Massdriver project, and
launch **instances**: forked environments that follow a branch and redeploy on push.

The name is a nod to the rocket sleds that ride test-track rails; a mass driver is the
electromagnetic version.

## How it maps onto Massdriver

| Rocketsled | Massdriver |
|---|---|
| Project (opted in via config) | Project: the parity boundary and blueprint |
| Stack | A repository linked to one project |
| Instance | An environment forked from the project's base environment |
| Preset | An entry in a bundle's `params.examples` (`__name`) |
| Run | A `createDeployment` (app component) or `deployEnvironment` (launch) |

Rocketsled never exposes raw bundle parameters. When launching an instance a user picks
one preset per component; the chosen preset is deep-merged over the base environment's
configuration with `copyInstance`, and the app component's image tag is set to the
branch head. Everything else, including cloud credentials, comes from the base environment
through `copyEnvironmentDefaults`.

## Requirements on the Massdriver side

1. An organization with a service account token (`md_...`).
2. One or more projects, each with a fully configured, deployable **base environment**
   (for example `production` or `staging`) whose environment defaults supply cloud
   credentials.
3. Bundles with `params.examples` presets. Components without presets can still be
   launched; they inherit the base environment's parameters.
4. An application component whose params include an image tag path (default `image.tag`).

## Run it

```sh
cp rocketsled.example.yaml rocketsled.yaml   # fill in secrets, or export them as env vars
make            # builds the UI and the binary
bin/rocketsled check -config rocketsled.yaml   # validates config and lists projects, components, presets
bin/rocketsled serve -config rocketsled.yaml
```

Or with Docker:

```sh
docker build -t rocketsled .
docker run -p 8080:8080 -v "$PWD/data:/data" --env-file .env rocketsled
```

`server.base_url` must be reachable by GitHub/GitLab: OAuth callbacks land on
`/auth/{provider}/callback` and push webhooks on `/hooks/{provider}`. For local
development, a tunnel such as `ngrok http 8080` works.

### OAuth applications

- **GitHub**: Settings → Developer settings → OAuth Apps. Callback URL
  `<base_url>/auth/github/callback`. Rocketsled requests `read:user user:email repo`.
- **GitLab**: User settings → Applications. Redirect URI
  `<base_url>/auth/gitlab/callback`, scopes `read_user api`.

## Building images

Set `builder.docker` and give a project an `image_registry`, and Rocketsled builds the
stack's Dockerfile itself: on launch and on every push it clones the commit, runs
`docker build` in the stack's path, pushes `<image_registry>:<sha>`, then deploys with the
tag. A stack's **path** points at the directory holding the Dockerfile, so a monorepo can
host several stacks. `example/hello-world` is a Lambda container image built this way.

Without a builder, Rocketsled only writes the tag and expects CI to have pushed the image.

## Example catalog

`example/catalog/aws-eks-3-tier` is a Massdriver catalog (same layout as
massdriver-catalog) built for Rocketsled stacks on Kubernetes: `aws-vpc` and `aws-eks`
for the platform team, a `k8s-landing-zone` that turns a cluster into per-tenant
namespaces with scoped credentials, and four Helm bundles (PostgreSQL, MariaDB,
MinIO object storage, and an app Deployment) that consume only the namespaced
`kubernetes-cluster` credential. Its README explains how a tenant's credential becomes
the environment default for that customer's stack instances.

## Deploy automation

Two paths, both per stack:

- **Webhook**: Rocketsled registers a push webhook when a stack is linked. A push to a
  branch redeploys every instance following that branch with the commit sha as the image
  tag.
- **CI callback**: when the pipeline builds the image, call back after the push with the
  stack's deploy token (shown once at link time):

  ```sh
  curl -X POST "$ROCKETSLED_URL/ci/v1/stacks/$STACK_ID/deploys" \
    -H "Authorization: Bearer $ROCKETSLED_DEPLOY_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"ref\":\"$GITHUB_REF_NAME\",\"sha\":\"$GITHUB_SHA\",\"imageTag\":\"$GITHUB_SHA\"}"
  ```

## REST API

All routes under `/api/v1` require the session cookie set by the OAuth flow.

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v1/meta` | Configured sign-in providers |
| GET | `/api/v1/me` | Current user |
| POST | `/api/v1/logout` | End the session |
| GET | `/api/v1/repos` | Repositories visible to the user |
| GET | `/api/v1/projects` | Opted-in projects with components and presets |
| GET | `/api/v1/projects/{id}` | One project |
| GET, POST | `/api/v1/stacks` | List, link (`{name, repoFullName, projectId, path}`) |
| GET, DELETE | `/api/v1/stacks/{id}` | Detail, unlink |
| GET, POST | `/api/v1/stacks/{id}/instances` | List, launch |
| GET, DELETE | `/api/v1/instances/{id}` | Live status, destroy |
| POST | `/api/v1/instances/{id}/deploys` | Deploy now (`{ref?, sha?, imageTag?, message?}`); returns 202, the run continues in the background |
| POST | `/hooks/{provider}` | SCM push webhook |
| POST | `/ci/v1/stacks/{id}/deploys` | CI callback, Bearer deploy token |

Errors are `{"error": "...", "field": "..."}` with 422 for validation, 404, 403, 409, and
502 when Massdriver or the SCM rejected a call.

## Development

```sh
make test        # go test -race ./...
make lint        # golangci-lint + eslint + tsc
make generate    # regenerate domain mocks (moq)
make ui-dev      # Vite dev server on :5173, proxying to :8080
```

Layout:

```
cmd/rocketsled        entrypoint: serve, check
internal/config       rocketsled.yaml
internal/domain       entities, ports (Platform, SCM, Store), Service, tests with mocks
internal/massdriver   GraphQL adapter implementing domain.Platform
internal/scm          GitHub and GitLab providers implementing domain.SCM
internal/store        SQLite store (tokens encrypted at rest) and an in-memory store
internal/httpapi      REST handlers, session auth, embedded UI
internal/builder      docker/git image builder implementing domain.Builder
web                   Vite + React UI, embedded into the binary at build time
example/hello-world   Lambda container image app used to demo the whole loop
example/catalog       Massdriver catalog: VPC, EKS, landing zone, and Helm data/app tier bundles
```

Ideas that are not built yet are in [TODO.md](TODO.md).
