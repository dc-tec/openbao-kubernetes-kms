# Changelog

## Unreleased

### Bug Fixes

- **ci:** Lint certificate and E2E build tags in the core gate. Clean up fixture
  setup, limit OAuth fixture request bodies, and keep recursive ownership
  changes within the certificate fixture directory.

- **test:** Give OAuth expiry assertions a bounded observation window beyond
  the last token's maximum lifetime, and bound each status RPC by that window.

- **docs:** Document diagnostic check IDs, credential file delivery and symlink
  restrictions, and the defensive meaning of `aad_missing`.

- **filesystem:** Require the socket directory to belong to the provider's
  effective UID. Open state and checkpoint files relative to a pinned directory
  and validate the opened file before decoding it.

- **observability:** Export Go runtime, process, and build metadata metrics.
  Add a provider-scoped alert for repeated observed process restarts.

- **policy:** Include token renewal in `policy openbao` output by default,
  matching `init` and runtime behavior. Add `--include-token-renewal=false` for
  non-renewable tokens or policies that grant renewal separately.

- **rotation:** Measure activation delay from durable stable observations with
  process-local elapsed time. Wait the full delay after restart or recovery of
  an unconfirmed stable save. Preserve local timestamp ordering after backward
  clock corrections and report a bounded `clock.regressed` warning.

- **clock:** Prevent clock corrections from extending token and cached-status
  validity or changing retry, discovery, and circuit-breaker cooldowns.

- **cli:** Return the documented usage and configuration exit codes for command
  parsing and configuration inspection. Reject unknown help topics and
  completion subcommands, and remove the unsupported `trace` log level from
  the CLI reference.

- **config:** Reject contradictory probe and staleness intervals, renewal and
  refresh intervals, and duplicate fixed health and metrics endpoints before
  startup. Preserve disabled listeners and dynamically assigned ports.

- **config:** Validate Transit key names against the OpenBao metadata-path
  contract. Reject ambiguous path segments and unsupported names before startup,
  and escape OpenBao request paths once without rewriting identity values.

- **rotation:** Preserve service during failed observation-only saves when the
  published keys remain valid and both persistence files are unchanged. Report
  deferred saves through readiness diagnostics and a persistence warning metric.
  New identities, promotions, and partial writes still require recovery.

- **rotation:** Reconcile failed state and checkpoint saves before restoring
  readiness. Retain the exact attempted transition and reject conflicting disk
  state. Stop rewriting stable observation counts during the activation delay.

- **observability:** Report cached readiness reasons and probe failure classes.
  Log persisted key promotions and serve startup, readiness, and shutdown events
  without raw backend errors or key identities.

- **observability:** Distinguish TLS, DNS, and connection failures in redacted
  OpenBao and KMS error classes, including authentication failures. Keep gRPC
  status codes unchanged and identify Encrypt failures with the correct operation.

- **auth:** Bound PKCS#11 session pool waits by the effective
  `auth.loginTimeout`, including signer probes and TLS signing. Native calls
  inside the HSM module still require vendor client timeouts.

- **cli:** Check Transit configuration, trim, restore, and rewrap capabilities
  in doctor. Accept Kubernetes migration encryption configurations with local
  encryption keys and additional KMS providers while requiring the configured
  provider identity and socket. Redact inline keys from parse errors.
- **cli:** Return exit code `4` when rotation commands cannot read live OpenBao
  metadata. Preserve available local evidence and include
  `transitMetadataStatus` and, on failure, `transitMetadataError` in text and JSON
  reports.

- **auth:** Recover rejected OpenBao tokens with a shared, rate-limited login
  and one request retry. Keep valid tokens usable during early refresh, isolate
  refresh from caller cancellation, and apply `auth.loginTimeout` to shared
  work. Preserve redacted auth error causes and KMS error classifications.

- **rotation:** Decrypt peer ciphertext through validated pending snapshots.
  Discover unknown key IDs with bounded metadata refresh without advancing
  promotion. Retain pending identities across later rotations and reject
  metadata rollback or identity changes that would lose decrypt coverage.
- **rotation:** Persist observations when `min_encryption_version` blocks the
  old active key, then promote after the configured delay and observation count.
  Encrypt remains unavailable until the new active key passes its deep probe.

### Compatibility

Key IDs, AAD, annotations, and state schema remain unchanged. Upgrade all nodes
before rotating Transit keys. Older binaries do not provide pending-key decrypt
coverage. See [rotation upgrade guidance](docs/reference/compatibility.md#unreleased-rotation-corrections).

## [0.1.0-preview.3](https://github.com/dc-tec/openbao-kubernetes-kms/compare/0.1.0-preview.2...0.1.0-preview.3) (2026-10-01)


### ⚠ BREAKING CHANGES

* **auth:** JWT authentication now requires auth.jwt.source to select file or oauth2. Existing JWT file configurations must set source: file.

### Features

* **auth:** add native OAuth client credentials ([#88](https://github.com/dc-tec/openbao-kubernetes-kms/issues/88)) ([185a53b](https://github.com/dc-tec/openbao-kubernetes-kms/commit/185a53bcabb932656a9d9c313a65bd87abb0cb38))
* **cli:** add init to generate matching deployment files ([#82](https://github.com/dc-tec/openbao-kubernetes-kms/issues/82)) ([8410475](https://github.com/dc-tec/openbao-kubernetes-kms/commit/84104759494bb1bb95b3fc05e52cc086b728d2d8))
* **cli:** verify running provider sockets under the caller identity ([#120](https://github.com/dc-tec/openbao-kubernetes-kms/issues/120)) ([94b5e2b](https://github.com/dc-tec/openbao-kubernetes-kms/commit/94b5e2b9627f3a7e11400f715faa0d1face57d17))
* **distribution:** add pinned release downloads and offline transfer guidance ([#118](https://github.com/dc-tec/openbao-kubernetes-kms/issues/118)) ([a8e69c6](https://github.com/dc-tec/openbao-kubernetes-kms/commit/a8e69c62e405a63a3350550e71118ed224c5f164))
* **init:** generate a per-node node-setup.sh ([#145](https://github.com/dc-tec/openbao-kubernetes-kms/issues/145)) ([cbefdcc](https://github.com/dc-tec/openbao-kubernetes-kms/commit/cbefdccbe7b45437d1eaebf7a3fb4644a789ca7e))
* **init:** record installation inputs and stage KMS readers ([#115](https://github.com/dc-tec/openbao-kubernetes-kms/issues/115)) ([e472b98](https://github.com/dc-tec/openbao-kubernetes-kms/commit/e472b9866be5facae7828a1a7257a6c11156a024))
* **metrics:** expose runtime and process health signals ([#104](https://github.com/dc-tec/openbao-kubernetes-kms/issues/104)) ([19d1a45](https://github.com/dc-tec/openbao-kubernetes-kms/commit/19d1a4568460b61ea5535bf3e5639603c279b757))
* **packaging:** ship complete architecture-specific installation kits ([#117](https://github.com/dc-tec/openbao-kubernetes-kms/issues/117)) ([06b94df](https://github.com/dc-tec/openbao-kubernetes-kms/commit/06b94df41fbd424f9fdb372987cda57463f47791))


### Bug Fixes

* **auth:** bound PKCS[#11](https://github.com/dc-tec/openbao-kubernetes-kms/issues/11) session pool waits ([#84](https://github.com/dc-tec/openbao-kubernetes-kms/issues/84)) ([dbb2739](https://github.com/dc-tec/openbao-kubernetes-kms/commit/dbb2739c4f93efff2a15f26bef2f5fc6557dfb1e))
* **auth:** recover rejected tokens and isolate shared refresh ([#79](https://github.com/dc-tec/openbao-kubernetes-kms/issues/79)) ([e55d44d](https://github.com/dc-tec/openbao-kubernetes-kms/commit/e55d44d4f8db6edf8795a4ef1c3978b00dc90faa))
* **cli:** harden diagnostic checks and rotation reporting ([#80](https://github.com/dc-tec/openbao-kubernetes-kms/issues/80)) ([fdb12ac](https://github.com/dc-tec/openbao-kubernetes-kms/commit/fdb12ac7ce6242cd63ce0f03a9cc25058716eb4f))
* **cli:** honor usage and configuration exit codes ([#98](https://github.com/dc-tec/openbao-kubernetes-kms/issues/98)) ([c210bf0](https://github.com/dc-tec/openbao-kubernetes-kms/commit/c210bf0947463d65d8bf55e137529a674ba1a6df))
* **clock:** bound runtime lifetimes with elapsed time ([#100](https://github.com/dc-tec/openbao-kubernetes-kms/issues/100)) ([d2eb8f8](https://github.com/dc-tec/openbao-kubernetes-kms/commit/d2eb8f8d437c22c10cd0c851ae9d3e934a19f5ff))
* **compatibility:** preserve file JWT upgrades and qualify provider lifecycle ([#111](https://github.com/dc-tec/openbao-kubernetes-kms/issues/111)) ([7546305](https://github.com/dc-tec/openbao-kubernetes-kms/commit/7546305249d24fe765cb6263a76fe65eee8ef721))
* **config:** enforce Transit key name and URL contracts ([#96](https://github.com/dc-tec/openbao-kubernetes-kms/issues/96)) ([73f83b5](https://github.com/dc-tec/openbao-kubernetes-kms/commit/73f83b56dc3b47f3e6db23530cd6688a464b3bcb))
* **config:** reject contradictory timing and listener settings ([#97](https://github.com/dc-tec/openbao-kubernetes-kms/issues/97)) ([402d2e6](https://github.com/dc-tec/openbao-kubernetes-kms/commit/402d2e6762adfacd4eefcf5094e27545144d793b))
* **config:** reject implicit scalar coercion ([#75](https://github.com/dc-tec/openbao-kubernetes-kms/issues/75)) ([edd3d29](https://github.com/dc-tec/openbao-kubernetes-kms/commit/edd3d29ed9374f82de9520fa47c68d0ca6edc556))
* **deployment:** make systemd installation usable and testable ([#73](https://github.com/dc-tec/openbao-kubernetes-kms/issues/73)) ([684db20](https://github.com/dc-tec/openbao-kubernetes-kms/commit/684db2071cc62ad0decc90fcbc7442d93158d4c5))
* **deployment:** preserve atomic static-pod JWT rotation ([#92](https://github.com/dc-tec/openbao-kubernetes-kms/issues/92)) ([c9e7eef](https://github.com/dc-tec/openbao-kubernetes-kms/commit/c9e7eef7d83036bae0ad6a4710ffeab3213af816))
* **filesystem:** validate opened state and socket ownership ([#105](https://github.com/dc-tec/openbao-kubernetes-kms/issues/105)) ([85b9d06](https://github.com/dc-tec/openbao-kubernetes-kms/commit/85b9d06a6385a1cf565cf652773aabe83237a11e))
* **init:** reuse values with node-specific socket groups ([#127](https://github.com/dc-tec/openbao-kubernetes-kms/issues/127)) ([fafa0c0](https://github.com/dc-tec/openbao-kubernetes-kms/commit/fafa0c00ab13659c6a2b26fdb82180a11da98cca))
* **init:** validate generated setup boundaries and inputs ([#132](https://github.com/dc-tec/openbao-kubernetes-kms/issues/132)) ([e9384da](https://github.com/dc-tec/openbao-kubernetes-kms/commit/e9384da3f969353b28b524b2873a61d58e7c85df))
* **install:** verify consumer access and native release packages ([#135](https://github.com/dc-tec/openbao-kubernetes-kms/issues/135)) ([0657955](https://github.com/dc-tec/openbao-kubernetes-kms/commit/065795592fae25f9fcc41c6628bfc5f274c81614))
* **lab:** align Harvester bootstrap and recovery qualification ([#137](https://github.com/dc-tec/openbao-kubernetes-kms/issues/137)) ([d7ca395](https://github.com/dc-tec/openbao-kubernetes-kms/commit/d7ca395b089a191e9d17a7490f185c36861b932e))
* **observability:** explain readiness and runtime transitions ([#86](https://github.com/dc-tec/openbao-kubernetes-kms/issues/86)) ([1fa3ba2](https://github.com/dc-tec/openbao-kubernetes-kms/commit/1fa3ba25b23483bbc9be590a5c4d7e0b5debef7a))
* **observability:** preserve OpenBao transport failure classes ([#85](https://github.com/dc-tec/openbao-kubernetes-kms/issues/85)) ([88f0e1a](https://github.com/dc-tec/openbao-kubernetes-kms/commit/88f0e1abaafe24c5d964bd3e60b60848137b2dac))
* **policy:** align generated token renewal permissions ([#103](https://github.com/dc-tec/openbao-kubernetes-kms/issues/103)) ([35d76a9](https://github.com/dc-tec/openbao-kubernetes-kms/commit/35d76a950c2ddf2e36c827680cecfeb4e57321b0))
* **rotation:** add guarded operator key retirement ([#77](https://github.com/dc-tec/openbao-kubernetes-kms/issues/77)) ([2e13e70](https://github.com/dc-tec/openbao-kubernetes-kms/commit/2e13e7021395992f99af3dca6d384a2a2abb9d34))
* **rotation:** preserve decrypt coverage during promotion ([#78](https://github.com/dc-tec/openbao-kubernetes-kms/issues/78)) ([afbcd82](https://github.com/dc-tec/openbao-kubernetes-kms/commit/afbcd824b7802a53667430adf841103cf1ca446b))
* **rotation:** preserve service during deferred observation saves ([#90](https://github.com/dc-tec/openbao-kubernetes-kms/issues/90)) ([24d7b48](https://github.com/dc-tec/openbao-kubernetes-kms/commit/24d7b4861c9f2abc509df0fc08c02d2b0e40ceae))
* **rotation:** reconcile failed state persistence before recovery ([#89](https://github.com/dc-tec/openbao-kubernetes-kms/issues/89)) ([ba92060](https://github.com/dc-tec/openbao-kubernetes-kms/commit/ba920607887742666bd6eff37fabcabc3552ed92))
* **rotation:** recover reviewed historical version retirement ([#133](https://github.com/dc-tec/openbao-kubernetes-kms/issues/133)) ([bb91732](https://github.com/dc-tec/openbao-kubernetes-kms/commit/bb91732dee0c16378f9313b2dcb648ced8e1d1da))
* **rotation:** require elapsed activation delay after restart ([#101](https://github.com/dc-tec/openbao-kubernetes-kms/issues/101)) ([ba3b2ee](https://github.com/dc-tec/openbao-kubernetes-kms/commit/ba3b2eec19159911f72d64f52089924a7103b570))
* **runtime:** bound denial recovery and isolate discovery cancellation ([#134](https://github.com/dc-tec/openbao-kubernetes-kms/issues/134)) ([828ba7e](https://github.com/dc-tec/openbao-kubernetes-kms/commit/828ba7e98d7c6ef0ef00fb7aecb663e1de0b74e8))
* **state:** bind persisted backend identity ([#124](https://github.com/dc-tec/openbao-kubernetes-kms/issues/124)) ([a0ebca4](https://github.com/dc-tec/openbao-kubernetes-kms/commit/a0ebca428b14822bb6f0e6ea290fe8f7aa404fc8))
* **state:** validate checkpoint successor history ([#123](https://github.com/dc-tec/openbao-kubernetes-kms/issues/123)) ([6162731](https://github.com/dc-tec/openbao-kubernetes-kms/commit/6162731eed89f4a70811ae8f9f0c3d8e3bbc37c2))
* **status:** recover deep probes and protect pod startup ([#76](https://github.com/dc-tec/openbao-kubernetes-kms/issues/76)) ([d2ff1e1](https://github.com/dc-tec/openbao-kubernetes-kms/commit/d2ff1e1878ffff2d1a7bcb187db0f8acaf81e5d0))
* **test:** qualify VM upgrades against the published provider baseline ([#121](https://github.com/dc-tec/openbao-kubernetes-kms/issues/121)) ([69510f1](https://github.com/dc-tec/openbao-kubernetes-kms/commit/69510f1a3216aa62fb1951551180af5f7911c8fc))


### Continuous Integration

* **release:** gate publication on installation of the selected archives ([#122](https://github.com/dc-tec/openbao-kubernetes-kms/issues/122)) ([0181e05](https://github.com/dc-tec/openbao-kubernetes-kms/commit/0181e050a44d414c80cd96206d28e8cacdc9e5c5))

## [0.1.0-preview.2](https://github.com/dc-tec/openbao-kubernetes-kms/compare/0.1.0-preview.1...0.1.0-preview.2) (2026-09-21)


### Features

* **tooling:** add pinned devenv environment ([#57](https://github.com/dc-tec/openbao-kubernetes-kms/issues/57)) ([07d6fa7](https://github.com/dc-tec/openbao-kubernetes-kms/commit/07d6fa7493dbb0ce6608acf8a71262f14910d14f))


### Bug Fixes

* **ci:** update Go toolchain to 1.26.4 ([#24](https://github.com/dc-tec/openbao-kubernetes-kms/issues/24)) ([aa82b2d](https://github.com/dc-tec/openbao-kubernetes-kms/commit/aa82b2da39aebf6e06f608e817c7079e13b81486))
* **ci:** update Go toolchain to 1.26.6 ([#56](https://github.com/dc-tec/openbao-kubernetes-kms/issues/56)) ([0ed6c91](https://github.com/dc-tec/openbao-kubernetes-kms/commit/0ed6c912971731f37fc6f60fa80117f0c3c3a165))
* **deps:** update gRPC to 1.83.2 for GO-2026-6443 ([#68](https://github.com/dc-tec/openbao-kubernetes-kms/issues/68)) ([25948aa](https://github.com/dc-tec/openbao-kubernetes-kms/commit/25948aaec6155a0845b8543eebed2103ece4afb8))
* **deps:** update x/mod to v0.40.0 ([#61](https://github.com/dc-tec/openbao-kubernetes-kms/issues/61)) ([208a27e](https://github.com/dc-tec/openbao-kubernetes-kms/commit/208a27e69d6164c9327c0be7f9cb36210f4318bf))
* **kmsv2:** bound concurrent requests ([#53](https://github.com/dc-tec/openbao-kubernetes-kms/issues/53)) ([1f20f10](https://github.com/dc-tec/openbao-kubernetes-kms/commit/1f20f10ec521e0ccb6aeefd263672ab350b83066))
* **openbao:** bound HTTP response bodies ([#52](https://github.com/dc-tec/openbao-kubernetes-kms/issues/52)) ([82854c2](https://github.com/dc-tec/openbao-kubernetes-kms/commit/82854c2df8ce5e3c93987b94c6f109220086ba52))
* **openbao:** reject redirects before forwarding credentials ([228f0a2](https://github.com/dc-tec/openbao-kubernetes-kms/commit/228f0a20d22c6889f8a633824709d27ee397623a))
* **status:** correct probe health and token use ([#50](https://github.com/dc-tec/openbao-kubernetes-kms/issues/50)) ([7c59a38](https://github.com/dc-tec/openbao-kubernetes-kms/commit/7c59a38b0ce840fa21015918a3e20662a242eac4))
* **status:** enforce Transit disable_upsert at runtime ([#51](https://github.com/dc-tec/openbao-kubernetes-kms/issues/51)) ([1582938](https://github.com/dc-tec/openbao-kubernetes-kms/commit/15829384444959b5a4ec8e7abb909cb48f3590a8))
* **test:** stabilize nightly OpenBao E2E fixtures ([60e149d](https://github.com/dc-tec/openbao-kubernetes-kms/commit/60e149d9771744a705d9807277e2daf7201fd889))

## 0.1.0-preview.1 (2026-05-14)


### Features

* **auth:** add certificate login source ([24b7e26](https://github.com/dc-tec/openbao-kubernetes-kms/commit/24b7e268cc33d604e456204de7d80921acc62396))
* **auth:** add JWT token lifecycle ([58dca68](https://github.com/dc-tec/openbao-kubernetes-kms/commit/58dca68d3f78e817c9d2a9e44026bfc0dd5f574e))
* **auth:** add pkcs11 and spiffe cert providers ([f28c51a](https://github.com/dc-tec/openbao-kubernetes-kms/commit/f28c51a1a9dbfd4b09bc5c91a20bd97aab788ee8))
* **auth:** validate certificate identity material ([2259ca9](https://github.com/dc-tec/openbao-kubernetes-kms/commit/2259ca9c29c35b4cc13739f192a49104eb400d9b))
* **build:** add cert auth artifact targets ([6f9a97e](https://github.com/dc-tec/openbao-kubernetes-kms/commit/6f9a97e4460a37460440dcf30d3b42ec7b30f5c0))
* **cli:** add OpenBao policy generator ([cbd4b3b](https://github.com/dc-tec/openbao-kubernetes-kms/commit/cbd4b3bb2484ca6b25022122ead0395991301cc8))
* **cli:** add operational command suite ([87167bb](https://github.com/dc-tec/openbao-kubernetes-kms/commit/87167bbfed4ee1a61717a4f32c3e85efdecb53f7))
* **cli:** include capabilities self policy ([9cf2867](https://github.com/dc-tec/openbao-kubernetes-kms/commit/9cf2867bdcc0a14e95234fe4388824769f69b3e9))
* **cli:** initialize provider command scaffold ([26e51b0](https://github.com/dc-tec/openbao-kubernetes-kms/commit/26e51b0aa9e85eb753043f490fff16a42f9f4304))
* **cli:** support configured auth in diagnostics ([332a095](https://github.com/dc-tec/openbao-kubernetes-kms/commit/332a095a3bf5adc674208167cc2d4355f79b186e))
* **config:** add auth clock skew leeway ([0e83cbe](https://github.com/dc-tec/openbao-kubernetes-kms/commit/0e83cbe128b38c257f51d32f75802d1a198d839d))
* **config:** add nested auth method configuration ([f6cf421](https://github.com/dc-tec/openbao-kubernetes-kms/commit/f6cf421a906eb7e7a9ea6fcff4f22ace39bb76d6))
* **config:** implement strict validation and schema export ([62d97a0](https://github.com/dc-tec/openbao-kubernetes-kms/commit/62d97a06ce45a91c1d24b031784a6ffa0490484c))
* **deploy:** add Grafana observability dashboard ([4387c86](https://github.com/dc-tec/openbao-kubernetes-kms/commit/4387c86563d4499279b34bd2e0fc35bde93c2639))
* **deploy:** add linux release packages ([0c9318e](https://github.com/dc-tec/openbao-kubernetes-kms/commit/0c9318e031d5fa256aba45e76f877e3745656bb8))
* **deploy:** add WS10 packaging artifacts ([72b7e2b](https://github.com/dc-tec/openbao-kubernetes-kms/commit/72b7e2bceba7157658ecd60f7cfa1b6dacdd459f))
* **keys:** add key registry and AAD metadata primitives ([224eb7d](https://github.com/dc-tec/openbao-kubernetes-kms/commit/224eb7d154c8c68f9d6585f2670927e7af9a46f9))
* **keys:** complete decrypt preflight and registry state ([90813c4](https://github.com/dc-tec/openbao-kubernetes-kms/commit/90813c4b6c29cf8cc9deb83ac8a522e2c075962b))
* **kmsv2:** add KMS v2 protocol server ([89a34e4](https://github.com/dc-tec/openbao-kubernetes-kms/commit/89a34e420abe288e2eb9b9b57d6a19755c9f817a))
* **metrics:** expose cert auth certificate ttl ([da083dd](https://github.com/dc-tec/openbao-kubernetes-kms/commit/da083dd921d340ffba2c646a53d02efd5e6d091d))
* **observability:** add metrics logging and debug correlation ([d9fbbfe](https://github.com/dc-tec/openbao-kubernetes-kms/commit/d9fbbfeec6705b8b8c3bcc432d7a48808e3d89e4))
* **openbao:** add certificate auth login client ([daaf431](https://github.com/dc-tec/openbao-kubernetes-kms/commit/daaf431d390b5df212338c7d6d48511b862332e5))
* **openbao:** add transit client ([149f9e0](https://github.com/dc-tec/openbao-kubernetes-kms/commit/149f9e0f803bea6467859eedc05a31ee14bca352))
* **provider:** harden release contract ([dfea900](https://github.com/dc-tec/openbao-kubernetes-kms/commit/dfea900674b496b08b45a14318eb6d1be889a539))
* **runtime:** add socket and health lifecycle ([e053f69](https://github.com/dc-tec/openbao-kubernetes-kms/commit/e053f69272e2d02fe6043643d2702064e90b139a))
* **status:** add diagnostics and circuit breaker ([30f349c](https://github.com/dc-tec/openbao-kubernetes-kms/commit/30f349cc24f46aece19df89a96248bc3f94cc4d4))
* **status:** add status cache and rotation watcher ([7e4edc4](https://github.com/dc-tec/openbao-kubernetes-kms/commit/7e4edc47786f0bf24d6284c8cadf66dce5eba2e4))


### Bug Fixes

* **aad:** use fqdn kms annotation keys ([6a71875](https://github.com/dc-tec/openbao-kubernetes-kms/commit/6a718751cbaf9d146fbde06cd1f0ebe0abf21bf5))
* **auth:** complete local cert auth diagnostics ([3ce210b](https://github.com/dc-tec/openbao-kubernetes-kms/commit/3ce210b95fb8e51d98b23fbd3618a9c6ca62ffde))
* **auth:** fail closed on JWT identity drift ([62014e5](https://github.com/dc-tec/openbao-kubernetes-kms/commit/62014e58ee54a6dbe558aefba4f8e8e00ef79f7c))
* **auth:** harden pkcs11 certificate inputs ([ca52d15](https://github.com/dc-tec/openbao-kubernetes-kms/commit/ca52d1591c61bcc0ba2797f6ddb09996adaf73b1))
* **auth:** propagate cert provider handshake context ([9cd0a92](https://github.com/dc-tec/openbao-kubernetes-kms/commit/9cd0a92f9b1303d96a553c6e70f9b1f23bf3652e))
* **auth:** satisfy cert auth lint checks ([2ce196d](https://github.com/dc-tec/openbao-kubernetes-kms/commit/2ce196dda70bbcb67be64a86355b8fc931514255))
* **ci:** clear core quality lint failures ([938dbdb](https://github.com/dc-tec/openbao-kubernetes-kms/commit/938dbdbd8a3913717a91cfd66b8830bc022470c0))
* **ci:** make deployment verification portable ([7afe405](https://github.com/dc-tec/openbao-kubernetes-kms/commit/7afe405abfd643390a40975a35d18b03a0bc6f8a))
* **config:** gate spiffe cert auth source ([0d39006](https://github.com/dc-tec/openbao-kubernetes-kms/commit/0d39006f15c33b80a03ca146fd94761ac4571020))
* **config:** restrict environment overrides ([bddbe56](https://github.com/dc-tec/openbao-kubernetes-kms/commit/bddbe565670c0647fbf86187277a44f961b9e896))
* **deploy:** align OpenBao policy samples ([c39c145](https://github.com/dc-tec/openbao-kubernetes-kms/commit/c39c14543026945eed0e0238773d5b235bfa3bc5))
* **deploy:** scope OpenTofu module to OpenBao setup ([d517d19](https://github.com/dc-tec/openbao-kubernetes-kms/commit/d517d192696c7403450ac9782c7b21d0899168a2))
* **deps:** update go-jose security patch ([1a32965](https://github.com/dc-tec/openbao-kubernetes-kms/commit/1a3296507d0a2ea075397874a28f2fc9ce039595))
* **docs:** publish mermaid vendor asset ([#11](https://github.com/dc-tec/openbao-kubernetes-kms/issues/11)) ([8ba9197](https://github.com/dc-tec/openbao-kubernetes-kms/commit/8ba9197f1f0976eb8071930afa2ffb4a1418b528))
* **docs:** repair pages rendering ([#10](https://github.com/dc-tec/openbao-kubernetes-kms/issues/10)) ([0c7f539](https://github.com/dc-tec/openbao-kubernetes-kms/commit/0c7f53957a9a221eb5ac2ab53cb06d431d4623a1))
* **e2e:** allow HA client sample writes in CI ([f53e65c](https://github.com/dc-tec/openbao-kubernetes-kms/commit/f53e65cef1edd4b94ae04b3851b00911569c9d2d))
* **e2e:** isolate OpenBao release gate lanes ([5a7faac](https://github.com/dc-tec/openbao-kubernetes-kms/commit/5a7faacd5cc892e1c4b158a03a5761b712018af4))
* **e2e:** make OpenBao startup portable in CI ([27a9a3b](https://github.com/dc-tec/openbao-kubernetes-kms/commit/27a9a3b40080e0898b771667dca701ea17c978ab))
* **e2e:** stabilize OpenBao soak lanes ([5c2d1df](https://github.com/dc-tec/openbao-kubernetes-kms/commit/5c2d1df31ae437f53807226ae37c80a60a87bc31))
* **e2e:** wait for node-local kind apiserver restarts ([82469b8](https://github.com/dc-tec/openbao-kubernetes-kms/commit/82469b8cfec94451bea01952b439cc84e9c8118e))
* **keyregistry:** harden registry state persistence ([559da1a](https://github.com/dc-tec/openbao-kubernetes-kms/commit/559da1a00acca00dce765c16096270e1fac24813))
* **kmsv2:** enforce protocol boundary limits ([01f92e8](https://github.com/dc-tec/openbao-kubernetes-kms/commit/01f92e8e71f126f7d7eb2baae9cac2ac7b8f0622))
* **kmsv2:** preserve OpenBao availability classes ([8d8ea11](https://github.com/dc-tec/openbao-kubernetes-kms/commit/8d8ea11daa7f4c95ece230074ba3a547a3173261))
* **kmsv2:** preserve transit error semantics ([c574a23](https://github.com/dc-tec/openbao-kubernetes-kms/commit/c574a232b9d5c6bfa3aac96f9d3071a00f280b6c))
* **openbao:** enforce supported transit key type ([0746798](https://github.com/dc-tec/openbao-kubernetes-kms/commit/074679805d5d84caac9419a0583ffe148f6a7999))
* **rotation:** reject unsafe CLI state rebuilds ([459698d](https://github.com/dc-tec/openbao-kubernetes-kms/commit/459698d8ac70828ad1ba6b1f6b845f95678f2d75))
* **rotation:** retain skipped transit versions ([6bbde35](https://github.com/dc-tec/openbao-kubernetes-kms/commit/6bbde35207060d23b343d1f41d37f9026549af5c))
* **runtime:** harden diagnostics and OpenBao transport ([fd5bc3c](https://github.com/dc-tec/openbao-kubernetes-kms/commit/fd5bc3cac7d64a6dff7021da2804968d8a62f71f))
* **runtime:** harden transit identity validation ([c41d358](https://github.com/dc-tec/openbao-kubernetes-kms/commit/c41d358ad54cf7b35b1980b55e7696809795e260))
* **security:** harden provider socket and auth lifecycle ([ac69018](https://github.com/dc-tec/openbao-kubernetes-kms/commit/ac690182c4aa2169593bb1b31e4bbfc45fb756fd))
* **state:** explain bootstrap denial reasons ([8b0b5a0](https://github.com/dc-tec/openbao-kubernetes-kms/commit/8b0b5a0ee4a334e9dbb1b0da981f3ad9960ff79b))
* **state:** surface checkpoint rollback posture ([3901959](https://github.com/dc-tec/openbao-kubernetes-kms/commit/3901959207279da86992ab6f452b2aa07030504d))
* **status:** fail closed on unsafe registry recovery ([89ceb41](https://github.com/dc-tec/openbao-kubernetes-kms/commit/89ceb4146a67a169ca7dc5888b419f15796be553))


### Miscellaneous Chores

* **release:** prepare 0.1.0-preview.1 ([#14](https://github.com/dc-tec/openbao-kubernetes-kms/issues/14)) ([642485b](https://github.com/dc-tec/openbao-kubernetes-kms/commit/642485b6e574cdb8a3992b6eb10e73f12f8af64e))

## Changelog

Release notes are generated and maintained by release-please from Conventional Commits.

Manual release notes, migration warnings, and operator-facing callouts should be added to the release PR before it is merged when the generated entry is not sufficient.
