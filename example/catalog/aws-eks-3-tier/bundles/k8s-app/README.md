# k8s-app

The application tier. A Deployment plus Service (and an optional Ingress) that
receives its data-tier connections as environment variables:

| Variable | Source |
|---|---|
| `DATABASE_URL` | `postgres-database` connection |
| `MYSQL_URL` | `mysql-database` connection |
| `S3_ENDPOINT`, `S3_BUCKET`, `S3_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | `object-storage` connection |
| `LOG_LEVEL`, `PORT` | params, through the bundle's `app.envs` |
| `APP_SECRET` | instance secret |

`image.tag` is the field Rocketsled rewrites on every push, so a stack linked to a
project built on this catalog sets `image_tag_param: image.tag` (the default).

Emits a `workload` resource with the in-cluster or ingress URL.
