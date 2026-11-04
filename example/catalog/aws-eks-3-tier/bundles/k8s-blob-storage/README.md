# k8s-blob-storage

MinIO in single-node mode with one bucket created by a post-install job. Any S3
SDK works against it: point the client at the emitted endpoint, use path-style
addressing, and pass the access key pair from the resource.

Emits an `object-storage` resource. This catalog's copy of that resource type adds
an optional `auth` block for stores that are not IAM-integrated; a real S3 bucket
bundle would leave it out and rely on the workload's IAM role.

Single node, no erasure coding: for development and previews.
