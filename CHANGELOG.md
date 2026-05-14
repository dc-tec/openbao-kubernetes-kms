# Changelog

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
