# KMS Conformance Tests

Kubernetes Key Management Service (KMS) v2 protocol conformance tests live
here.

The suite starts `internal/kmsv2` with fake cached status and fake Transit from
`test/fakes`. It serves the real Kubernetes KMS v2 gRPC service over a
filesystem Unix socket and calls it through the generated Kubernetes KMS v2
client.

These tests verify protocol behavior that unit tests can miss:

- `Status` is callable over the Unix socket.
- repeated `Status` calls do not call Transit.
- `EncryptResponse.key_id` matches `Status.key_id`.
- `Decrypt` accepts the output of `Encrypt`.
- unknown `key_id` and invalid annotations are rejected before Transit decrypt.
