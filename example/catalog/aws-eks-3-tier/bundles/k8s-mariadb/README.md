# k8s-mariadb

MariaDB as a one-replica StatefulSet with a persistent volume, deployed into the
namespace the connected `kubernetes-cluster` credential is scoped to. Application
and root passwords are generated on first install and kept across upgrades.

Emits a `mysql-database` resource (MariaDB speaks the MySQL protocol) pointing at
the in-cluster service.

Single node, no backups: a development database. A managed MySQL or MariaDB that
emits the same resource type slots in for production without changing consumers.
