# k8s-app

{{#resources.workload}}
## Rollout status

```sh
kubectl --namespace {{dependencies.kubernetes_cluster.namespace}} rollout status deployment/{{resources.workload.name}}
kubectl --namespace {{dependencies.kubernetes_cluster.namespace}} get pods -l app.kubernetes.io/instance={{resources.workload.name}}
```

## Crash looping

```sh
kubectl --namespace {{dependencies.kubernetes_cluster.namespace}} logs deployment/{{resources.workload.name}} --previous
```

`ImagePullBackOff` means the tag `{{params.image.tag}}` is not in `{{params.image.repository}}`;
check the Rocketsled run's build log. Failing readiness on `{{params.health_check_path}}`
means the app is up but not answering there; change the path or fix the handler.

## Reach it from your laptop

```sh
kubectl --namespace {{dependencies.kubernetes_cluster.namespace}} port-forward svc/{{resources.workload.name}} {{params.port}}:{{params.port}}
curl -s localhost:{{params.port}}{{params.health_check_path}}
```
{{/resources.workload}}
