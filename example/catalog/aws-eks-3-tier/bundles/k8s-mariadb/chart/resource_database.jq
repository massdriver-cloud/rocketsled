(.outputs[] | select(.kind == "Secret" and .apiVersion == "v1")) as $secret
| (.outputs[] | select(.kind == "Service" and .apiVersion == "v1")) as $svc
| {
    id: $svc.metadata.name,
    auth: {
      hostname: "\($svc.metadata.name).\($svc.metadata.namespace).svc.cluster.local",
      port: 3306,
      database: ($secret.data.database | @base64d),
      username: ($secret.data.username | @base64d),
      password: ($secret.data.password | @base64d)
    },
    version: .params.version,
    high_availability: false,
    policies: [{id: "read-write", name: "Read/Write"}]
  }
