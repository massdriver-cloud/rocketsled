(.outputs[] | select(.kind == "Service" and .apiVersion == "v1")) as $svc
| ($svc.spec.ports[] | select(.name == "http") | .port) as $port
| {
    name: $svc.metadata.name,
    deployment_id: .params.image.tag,
    service_url: (
      if .params.ingress.enabled and (.params.ingress.host // "") != ""
      then "http://\(.params.ingress.host)"
      else "http://\($svc.metadata.name).\($svc.metadata.namespace).svc.cluster.local:\($port)"
      end
    ),
    health_check_url: "http://\($svc.metadata.name).\($svc.metadata.namespace).svc.cluster.local:\($port)\(.params.health_check_path // "/")",
    tags: (.params.md_metadata.default_tags // {})
  }
