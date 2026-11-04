# k8s-blob-storage

{{#resources.bucket}}
## Browse the bucket

```sh
kubectl --namespace {{dependencies.kubernetes_cluster.namespace}} port-forward svc/{{resources.bucket.id}} 9001:9001
```

Open http://localhost:9001 and sign in with the access key pair from the resource.

## Bucket missing

The bucket is created by the `{{resources.bucket.id}}-create-bucket` job after
install. If it failed, its pod log says why:

```sh
kubectl --namespace {{dependencies.kubernetes_cluster.namespace}} logs job/{{resources.bucket.id}}-create-bucket
```

Redeploying re-runs the job.

## Out of disk

Raise `storage_gb` and redeploy.
{{/resources.bucket}}
