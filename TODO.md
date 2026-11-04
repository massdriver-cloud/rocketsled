# Rocketsled ideas

- **Getting apps in.** The lambda-function runtime takes an `image_uri`. The next
  step is linking a repository to the account so pushes build an image and update
  that param on the environments that follow the branch.
- **Organization hand-off.** When a customer outgrows Rocketsled, transfer the
  organization to them in Massdriver and retire the account's service account.
- **More catalogs.** Azure and GCP equivalents of aws-serverless, and a Kubernetes
  three-tier catalog (VPC, EKS, a landing zone that mints namespaced credentials,
  Helm-deployed Postgres/MariaDB/MinIO and an app).
- **Real IaC.** The aws-serverless bundles are `random_pet` stubs with real schemas;
  swap `src/` for the aws-software-factory modules once the product is settled.
- **Install resumability.** The install runs in a goroutine; a server restart
  mid-install leaves the account `installing`. Mark it failed on startup so the retry
  button appears.
