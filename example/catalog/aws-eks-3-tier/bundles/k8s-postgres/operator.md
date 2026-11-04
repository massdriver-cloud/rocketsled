# k8s-postgres

{{#resources.database}}
## Connect

```sh
kubectl --namespace {{dependencies.kubernetes_cluster.namespace}} exec -it statefulset/{{resources.database.id}} -- \
  psql -U {{resources.database.auth.username}} -d {{resources.database.auth.database}}
```

## Pod not ready

```sh
kubectl --namespace {{dependencies.kubernetes_cluster.namespace}} describe pod {{resources.database.id}}-0
kubectl --namespace {{dependencies.kubernetes_cluster.namespace}} logs {{resources.database.id}}-0
```

A `Pending` pod with `FailedScheduling` is usually the landing zone quota or an
unbound volume claim. `CrashLoopBackOff` after a version change means the data
directory was written by a different major version; PostgreSQL does not upgrade
data in place.

## Out of disk

Raise `storage_gb` and redeploy. The volume expands online if the storage class
allows it.
{{/resources.database}}
