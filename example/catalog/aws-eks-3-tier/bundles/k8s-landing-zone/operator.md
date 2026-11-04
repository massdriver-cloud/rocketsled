# k8s-landing-zone

{{#resources.kubernetes_cluster}}
## What the tenant can see

```sh
kubectl --namespace {{params.namespace}} get all
kubectl --namespace {{params.namespace}} describe resourcequota tenant
```

## Pods stuck Pending

Usually the quota. Compare requests against the ceiling:

```sh
kubectl --namespace {{params.namespace}} describe resourcequota tenant
kubectl --namespace {{params.namespace}} get events --field-selector reason=FailedCreate
```

Raise `quota.cpu` / `quota.memory` / `quota.pods` and redeploy.

## Rotate the deployer token

```sh
kubectl --namespace {{params.namespace}} delete secret deployer-token
```

Redeploy the landing zone; the secret and the resource are recreated. Instances
that inherited the old credential pick up the new one on their next deploy.
{{/resources.kubernetes_cluster}}
