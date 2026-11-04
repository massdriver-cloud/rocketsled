(.outputs[] | select(.kind == "Secret" and .apiVersion == "v1")) as $secret
| (.outputs[] | select(.kind == "Service" and .apiVersion == "v1")) as $svc
| {
    id: $svc.metadata.name,
    name: .params.bucket_name,
    endpoint: "http://\($svc.metadata.name).\($svc.metadata.namespace).svc.cluster.local:9000",
    region: "us-east-1",
    auth: {
      access_key_id: ($secret.data["access-key-id"] | @base64d),
      secret_access_key: ($secret.data["secret-access-key"] | @base64d)
    },
    policies: [{id: "read-write", name: "Read/Write"}]
  }
