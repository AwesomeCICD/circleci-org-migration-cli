# Changelog

## 0.1.0 (2026-06-10)


### Features

* add 'secrets extract' for in-pipeline secret capture ([13270cb](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/13270cb79a352b1dfbb0dfc94672356fb3c22879))
* add 'secrets merge' and the CircleCI orb ([7ee4961](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/7ee496158f73af8b381c0b34c517440b0615bccc))
* add api/org and api/context read clients ([bb10ec7](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/bb10ec7d1465be0f232159f39111a8d6795bc822))
* add api/project read client ([5d01795](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/5d017955b4ebd1999016fd450f62fb6169281a89))
* add export command with manifest + audit report ([b93c197](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/b93c1971bac18fd420a8791bce5cd986993282d2))
* add sync command for context migration (dry-run by default) ([a8f1a58](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/a8f1a584625ea3c13e4239bc5bc169be068f8bd8))
* **api:** add project write methods (env var create, settings patch) ([9ffd552](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/9ffd5523cb72b41c42eef8d2b91c5711d03e4ad3))
* capture and sync org-level settings ([68faad4](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/68faad4f77cc2537761867bf616a09dcd1bdf291))
* capture audit-log config; sync group restrictions via CIAM groups ([35a2047](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/35a20479bf8707312614a616a908473e932941e5))
* capture SSO/SAML config for reference (manual sync) ([828d471](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/828d4715723c4abad083fc313ab121aa47a12c44))
* capture+sync OTel exporters and org technical/security contacts ([483bb99](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/483bb9906f6d6c1637132196a2855de65ecac1eb))
* **capture:** temporarily remove + restore genuine context restrictions ([1685f8d](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/1685f8d991c8e8e278b9d5597d6e92373af09572))
* define manifest, secret-bundle, and mapping data contract ([5ef7aba](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/5ef7aba0f28d95e273c82c1ceef3ae69d8b06b2e))
* **export:** capture org CircleCI groups (definitions) + runbook guidance ([cc39bef](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/cc39befde9c5a3bceeb390c27bdffc6ba0d30630))
* **M4c:** capture pipeline-definitions + triggers; discover via private project list ([1bddb38](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/1bddb3886d2e7da2585ca15ecd7a3b445c7efe5e))
* **M4c:** create GitHub App projects — pipeline-definitions + triggers (paused) ([8fc255a](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/8fc255a7bcf9e1b7ab5a53128f96d5be5b4ea68a))
* **M4c:** create OAuth projects (paused) + enable-builds via follow ([04b5888](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/04b5888149636b85ce0c11334f64f1a7f5f6d2cd))
* **M5:** implement migrate all-in-one command (export -&gt; sync -&gt; enable) ([979a909](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/979a909ae63ed5e8c4067885333485976be776a4))
* **orb:** restructure into Orb Development Kit src/ layout + install caching ([5fb4e18](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/5fb4e18152fcab968c7d178d0c64d15a41177c0c))
* **report:** turn migration report into a customer cutover runbook ([a78bee9](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/a78bee9f1b2c0e922a7eff06817ce40a77711418))
* secrets capture — CLI-orchestrated extraction via unversioned config ([3df729d](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/3df729d212b887f77d969941f727c01f08159e48))
* sync project settings and env vars to existing projects ([3da25a0](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/3da25a040149a8e9321c8ee32d53bc0101640ed7))
* sync webhooks/schedules; capture+sync project OIDC and v1.1 flags ([cc4d769](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/cc4d76919498cfb6d6023448b311efb6c32d998c))
* **sync:** cross-type OAuth-&gt;App — synthesize pipeline-def + translated trigger ([8969bda](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/8969bdad48741d37d933a795ced6cc91c02670fd))
* **sync:** verify dest repos in the new GitHub org; skip+flag missing (repo-move) ([2deda50](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/2deda50285e75d8d6594451055a83f9b3d942fb4))


### Bug Fixes

* **capture:** org-level trigger flag + All-members restriction + stale warnings ([ad8af69](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/ad8af69b9097b29f1ed7090a6d1c9b3ba6eb2df0))
* **ci:** bump path-filtering orb to [@3](https://github.com/3).0.0 (Python 3.10+) ([29df599](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/29df599301511ad804eb467f67a7361fc19eb612))
* **ci:** enable cgo for race tests and drop unused test helpers ([a709de8](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/a709de89d5b593fa19ebb14584c9c96f02d686eb))
* **export:** --projects now restricts to the given slugs (was additive) ([a6339aa](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/a6339aa03802feb02e15f4fe2a66bc9ef79d4750))
* **orb:** store only one plaintext secret artifact, with caution note ([f2084ba](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/f2084ba506db582ad8f1c36329a1a341deb72f09))
* **security:** pin go1.26.4 toolchain and justify gosec G704 ([a8ee07b](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/a8ee07b264542dbdb6bfa2b09e8b5f8c64e2bd4e))
* **sync:** App project settings + v1.1 flag write + clearer repo-access ([c2158b7](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/c2158b7bd18aae15ba0235e10c78e054be5393cd))
