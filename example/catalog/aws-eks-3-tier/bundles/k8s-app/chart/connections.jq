{
  postgres: {
    url: "postgresql://\(.postgres.auth.username):\(.postgres.auth.password)@\(.postgres.auth.hostname):\(.postgres.auth.port)/\(.postgres.auth.database)"
  },
  mariadb: {
    url: "mysql://\(.mariadb.auth.username):\(.mariadb.auth.password)@\(.mariadb.auth.hostname):\(.mariadb.auth.port)/\(.mariadb.auth.database)"
  },
  bucket: {
    name: .bucket.name,
    endpoint: (.bucket.endpoint // ""),
    region: (.bucket.region // "us-east-1"),
    access_key_id: (.bucket.auth.access_key_id // ""),
    secret_access_key: (.bucket.auth.secret_access_key // "")
  }
}
