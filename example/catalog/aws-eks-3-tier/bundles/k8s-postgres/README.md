# k8s-postgres

PostgreSQL as a one-replica StatefulSet with a persistent volume, deployed into
whatever namespace the connected `kubernetes-cluster` credential is scoped to.
The application password is generated on the first install and reused on every
upgrade, so connection strings stay stable.

Emits a `postgres-database` resource whose hostname is the in-cluster service DNS
name. Anything in the same namespace (or allowed by the landing zone's network
policy) can reach it.

This is a development database. It has no replicas, no backups and no failover;
production traffic belongs on a managed service that emits the same resource type.
