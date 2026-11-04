# k8s-mariadb

{{#resources.database}}
## Connect

```sh
kubectl --namespace {{dependencies.kubernetes_cluster.namespace}} exec -it statefulset/{{resources.database.id}} -- \
  mariadb -u {{resources.database.auth.username}} -p {{resources.database.auth.database}}
```

## Pod not ready

```sh
kubectl --namespace {{dependencies.kubernetes_cluster.namespace}} logs {{resources.database.id}}-0
```

`InnoDB: Unsupported redo log format` after a version change means the volume
holds data from a newer MariaDB; roll the version param back.

## Out of disk

Raise `storage_gb` and redeploy.
{{/resources.database}}
