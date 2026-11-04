# AWS Serverless

The first Rocketsled catalog. Everything a serverless application needs on AWS,
copied from [aws-software-factory](https://github.com/massdriver-catalogs/aws-software-factory)
and trimmed to four bundles:

| Component | Bundle | Produces |
|---|---|---|
| Network | `shared-vpc` | `aws-vpc` |
| HTTP API | `http-api` | `aws-http-api` |
| Table | `dynamodb-table` | `aws-dynamodb-table` |
| Application | `lambda-function` | runs a placeholder until given a container image |

`bootstrap/` creates the **AWS Serverless Platform** project with a **Production**
environment and all four components linked. Rocketsled publishes the catalog into
the customer's organization with the `mass` CLI, applies the bootstrap with
OpenTofu, collects the customer's AWS IAM role through a form built from
`platforms/aws-iam-role`, pins it as the environment default, and deploys each
component with the preset the customer chose.

The `aws-user-pool` and `llm-endpoint` resource types are published only so the
`http-api` and `lambda-function` bundles' optional dependencies resolve; nothing
in this catalog produces them.
