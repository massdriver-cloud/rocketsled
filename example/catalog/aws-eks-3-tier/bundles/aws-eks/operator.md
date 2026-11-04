# aws-eks

{{#resources.kubernetes_cluster}}
## Connect

Download the kubeconfig from the resource, or build one from the AWS side:

```sh
aws eks update-kubeconfig --name {{resources.kubernetes_cluster.id}} --region {{dependencies.network.region}}
kubectl get nodes
```

## Nodes not joining

```sh
aws eks describe-nodegroup --cluster-name {{resources.kubernetes_cluster.id}} --nodegroup-name {{resources.kubernetes_cluster.id}}-default --region {{dependencies.network.region}} --query 'nodegroup.health'
```

Most joins fail on private subnets with no NAT route. Confirm the network was
deployed with `nat_gateways` set to `single` or `per_az`.

## Rotate the admin credential

Delete the token secret and redeploy; the bundle recreates it and republishes
the resource.

```sh
kubectl -n kube-system delete secret massdriver-admin-token
```
{{/resources.kubernetes_cluster}}
