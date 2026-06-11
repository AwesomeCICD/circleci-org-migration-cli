# Changelog

## [0.8.2](https://github.com/AwesomeCICD/circleci-org-migration-cli/compare/v0.8.1...v0.8.2) (2026-06-11)


### Bug Fixes

* **release:** disable cosign keyless signing that aborted the goreleaser release ([#216](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/216)) ([33e5b00](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/33e5b005759c848e903fcc328aeaab96f5cf7bfb))

## [0.8.1](https://github.com/AwesomeCICD/circleci-org-migration-cli/compare/v0.8.0...v0.8.1) (2026-06-11)


### Bug Fixes

* **cli:** correctness & cleanup (Phase 1) ([#181](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/181)) ([#197](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/197)) ([8f6230f](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/8f6230fb7fc6b40088319b3b046122b3a84f4d15))

## [0.8.0](https://github.com/AwesomeCICD/circleci-org-migration-cli/compare/v0.7.0...v0.8.0) (2026-06-11)


### Features

* **sync:** warn when destination equals source; document mapping schema + secrets/--missing-secrets/--yes ([#165](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/165), [#170](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/170)) ([#174](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/174)) ([12e18e6](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/12e18e6169c64d7476cdb6c00ba64969e24064a7))


### Bug Fixes

* **org:** treat empty content-type 2xx as success for feature PUTs ([#166](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/166)) ([#172](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/172)) ([26e924c](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/26e924c905ced534813c5f181f0974aac6435286))
* **report,sync:** render+match CIAM users by username when email blank; report polish ([#167](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/167), [#168](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/168)) ([#173](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/173)) ([4d46044](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/4d46044e1189e8c2606319444cf2df86404e807c))
* **report:** stop claiming CIAM is automated by sync; flag as manual ([#176](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/176)) ([#178](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/178)) ([7c7a316](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/7c7a316a5af8b21cf81da6ec34229b39fc7ff0d8))
* **secrets:** fail closed on unattended capture-all in non-TTY ([#164](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/164)) ([#171](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/171)) ([93db19a](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/93db19a484df63b6cd61aedb4a2bda8143a76bdd))

## [0.7.0](https://github.com/AwesomeCICD/circleci-org-migration-cli/compare/v0.6.0...v0.7.0) (2026-06-11)


### Features

* capture project API tokens; document recreation; optional --create-project-tokens ([#154](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/154)) ([c052f75](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/c052f759b669dd3f93bbefc8825a7c5d024e0c71)), closes [#132](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/132)
* **export:** optional --include-usage data snapshot (opt-in; does not transfer) ([#161](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/161)) ([3d874df](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/3d874df211167ffbd09648aaeb8bcf01e47dba28)), closes [#152](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/152)
* migrate CIAM roles, groups, and project role-grants for standalone orgs ([#151](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/151)) ([f3514bf](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/f3514bf11e035afce1bcabb699202a4327567927)), closes [#134](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/134)
* **report:** render all captured data (pipelines/triggers/…); links, clearer summary, automatable callouts, detailed cutover ([#162](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/162)) ([f03d346](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/f03d3467cb25c37a63dc533d386f1a512fc742d3)), closes [#156](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/156)


### Bug Fixes

* **cli:** separate + style interactive prompts (circleci-cli feel); hide completion ([#159](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/159)) ([30a1afb](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/30a1afb3796bef55413d55221a989f2d574fe93c)), closes [#157](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/157) [#158](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/158)

## [0.6.0](https://github.com/AwesomeCICD/circleci-org-migration-cli/compare/v0.5.0...v0.6.0) (2026-06-11)


### Features

* **cli:** add --json output to export and sync ([#110](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/110)) ([0277c23](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/0277c23016bac9c45dee7a6568b86ba68ac70eb9)), closes [#107](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/107)
* **cli:** support circleci run migrate via CIRCLE_* env fallbacks ([#118](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/118)) ([eb96f4d](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/eb96f4db228606c109b490e0b9ebff900e43d826)), closes [#111](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/111)
* **export:** capture additional project SSH-key metadata ([#116](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/116)) ([6ec5626](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/6ec56266f1c349f2ed3367693dfbf5b64e8ae695))
* **export:** warn on non-migratable items; capture retention limits, full v1.1 flags, schedule actor, namespace ([#135](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/135)) ([ad858f3](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/ad858f32d1681f7d57964e675e810d308359181b)), closes [#130](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/130) [#131](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/131)
* **migrate:** add --json and --skip-runner for export/sync consistency ([#147](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/147)) ([e988df6](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/e988df6af9ca961ce1f86237fb1aabe02bea531f)), closes [#146](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/146)
* **report:** actionable manual items — friendly names, details, settings URLs ([#143](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/143)) ([d15f940](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/d15f940707bc5dc9d6c7730e81f253d477735d90)), closes [#142](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/142)
* **secrets:** extract additional project SSH private keys, fingerprint-matched ([#124](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/124)) ([bd0563e](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/bd0563eb5c00408c75e984d193430b59c2eecae0)), closes [#115](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/115)
* **sync:** actionable plan output (names + dest URLs); only warn drop_all_build_requests when set ([#145](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/145)) ([2ab00c0](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/2ab00c0b3af2b55b65fd9971a52fe0d293185e85)), closes [#144](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/144)
* **sync:** re-add additional project SSH keys from the secret bundle ([#120](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/120)) ([bcb85a6](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/bcb85a69be0aeed39eabeb918f008a1f4d6b89e3))


### Bug Fixes

* **ci:** bound gosec download with curl timeouts to stop sast hangs ([#119](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/119)) ([11f80f8](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/11f80f81596f32e1b3af22920207d66def4e135b))
* **ci:** gosec SAST on 8GB executor + gitleaks allowlist for ssh-key fixtures ([#121](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/121)) ([29c9c87](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/29c9c87bd8098679a8d11bf07fb28da23156316a))
* **ci:** install gosec from pinned prebuilt binary with retry ([#114](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/114)) ([5f6ba42](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/5f6ba42b99b59dddaf9f352269630e14b1e57dd0)), closes [#113](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/113)
* **secrets:** emit extract job as a string when there are no contexts ([#126](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/126)) ([9d659a7](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/9d659a79ced5dc31ed037d00ae848c0b1471273b)), closes [#125](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/125)
* **secrets:** preserve trailing newline when extracting SSH private keys ([#128](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/128)) ([28b0e01](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/28b0e01e9f4eebe2b4e8686897dca08215507dae))
* **secrets:** skip env-var extraction when a project has no variables ([#127](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/127)) ([b1dd222](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/b1dd222e69906fa881ccb646b2109f98e6359d99))
* **sync:** activate OTel/URL-orb idempotency by exposing adapter getters ([#141](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/141)) ([d37fc31](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/d37fc31e67ed3961b69bd99a4e95e1a55504588b))
* **sync:** idempotent re-runs, dry-run guard, report actions, budget UUID mapping ([#140](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/140)) ([5c14509](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/5c145097a4c58c9d0e7ef78959bec6bc0cb4a014)), closes [#138](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/138)
* **sync:** JSON/stderr hygiene, fatal on missing --secrets, --skip-runner ([#139](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/139)) ([2b31aa6](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/2b31aa64901d315899423fa7131d093cfce3b32a)), closes [#137](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/137)

## [0.5.0](https://github.com/AwesomeCICD/circleci-org-migration-cli/compare/v0.4.1...v0.5.0) (2026-06-11)


### Features

* **cli:** circleci-cli parity — host alias, command header, flag-error, version ([#94](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/94)) ([a97b274](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/a97b27402299783da712476fae7786001b91253e)), closes [#77](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/77)


### Bug Fixes

* **cli:** clearer arg errors, extract/capture wording, TTY/dest-org help notes, exit code 1 ([#100](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/100)) ([7af3347](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/7af3347aa88fe60ef729cc61bc36c1ac37de4e7d)), closes [#78](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/78)
* **cli:** correct broken workflow examples in root help ([#84](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/84)) ([cef5836](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/cef5836a4fd6c88f172b15538024c3fa1f858553)), closes [#69](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/69)
* **cli:** walkthrough step numbering + output-path prompt + secrets auto-load feedback ([#99](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/99)) ([9074576](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/9074576e9f004bbeb77a5315d22fb1ba6abeebcc)), closes [#76](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/76)
* **deps:** update go modules — golang.org/x/crypto v0.53.0 ([#91](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/91)) ([5d43976](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/5d43976a015cc458f5a8b764bc3649f02343316b))
* **docs:** disable cobra auto-gen date footer in generated docs ([#87](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/87)) ([6075d3b](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/6075d3b33dad0af29c98772961c34ce61545833f))
* **export:** drop malformed repo URL for CircleCI-native projects; friendly progress names ([#96](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/96)) ([84cc238](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/84cc238946f5bf6ae1085430575958c4642f9cc3)), closes [#95](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/95)
* **orb:** resolve orb inline token via source-token chain ([#93](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/93)) ([df9f864](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/df9f8647dd3046590f858a73fea3034cd025ff0c)), closes [#70](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/70)
* **report:** full org-settings section + project repo; fix double-logged progress ([#63](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/63)) ([c691000](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/c691000751c9bc93601eb7a64709f52f0f3911e2))
* **secrets:** encrypt captures by default; add --no-encrypt opt-out ([#86](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/86)) ([ced0bd3](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/ced0bd341c6f016824b8b54ecf898dd78a8ecc18)), closes [#67](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/67)
* **secrets:** mask secret input with x/term; drop false hidden-input claims ([#92](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/92)) ([11d0693](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/11d06938ccfde8f4ad41952c1be7f10d94dbb5b8)), closes [#66](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/66)
* **secrets:** org-type-aware context restrictions; flag group restrictions manual ([#98](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/98)) ([525d062](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/525d0620cb41a31d11e7d9f24904ba39790b73ea)), closes [#74](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/74)
* **secrets:** scope capture strictly + never touch default group (incident fix) ([#65](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/65)) ([02f5111](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/02f5111dd713bb75f0da7577f2ffcc0097ce183c))
* **security:** 0700 secret dirs + always remove plaintext on encrypt failure ([#82](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/82)) ([1af4da1](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/1af4da1a900415e952c3f1eb61e8cbf2c69e1793)), closes [#72](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/72)
* **security:** harden manifest redaction — OTel headers, URL-orb auth, nested SSO ([#83](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/83)) ([d7f5475](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/d7f54750ca8ccf0ac667b4a9baf07d0e18cc5b5e)), closes [#73](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/73)
* **sync:** describe full sync scope — contexts, projects, settings ([#85](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/85)) ([1cba8f0](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/1cba8f051feb900b5361d93db7ee9344c1ed4fea)), closes [#71](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/71)

## [0.4.1](https://github.com/AwesomeCICD/circleci-org-migration-cli/compare/v0.4.0...v0.4.1) (2026-06-10)


### Bug Fixes

* **api:** retention SET via PUT; v1.1 settings flat-shape (standalone orgs) ([#60](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/60)) ([8dbacfd](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/8dbacfdfcdc42c8d2d2317fe358c88e0ab1b96d8))
* **secrets-capture:** fix 7 live bugs found running capture interactively ([#61](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/61)) ([c306ebb](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/c306ebb2f038d1a691d18dc4f8d94b3ead6c29a6))

## [0.4.0](https://github.com/AwesomeCICD/circleci-org-migration-cli/compare/v0.3.0...v0.4.0) (2026-06-10)


### Features

* capture & transfer spend budgets, block-unregistered-users, and org contacts (write) ([#54](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/54)) ([d4e1147](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/d4e114799011e8b89450c9e6e8b5f35584d5b4e5))
* capture org orbs + release-tracker settings & environment hierarchy ([#56](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/56)) ([2dba112](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/2dba11282a9eea6f79de35ec75d4961fb17d5291))
* capture, transfer, and minimize storage retention (artifacts) ([#52](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/52)) ([c01b06e](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/c01b06ecbd0919ce5750dd4d5c0639c70d13690b))
* **secrets:** age/SSH encryption of the secret bundle + S3 storage option ([#55](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/55)) ([8a0e900](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/8a0e900ac886ee0831e9d4ff2093ee085acfc022))
* **secrets:** interactive guided extraction (recommended path) with host-project selection ([#57](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/57)) ([c670d24](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/c670d24255d3840a0021edb5b0d9b2a4087de240))

## [0.3.0](https://github.com/AwesomeCICD/circleci-org-migration-cli/compare/v0.2.0...v0.3.0) (2026-06-10)


### Features

* capture and sync self-hosted runner resource classes ([#44](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/44)) ([dc59141](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/dc59141eb0a490d26fb25655aaba2e5e62937ee6))
* **cli:** leveled debug logging and actionable API errors ([a87c09c](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/a87c09c02a84a548203dee6ed8b776e1af4c21a9))
* **migrate:** interactive guided migration (flags bypass for automation) ([#40](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/40)) ([325412a](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/325412af12291e3521ad0c42c2ed43987fdfaa62))


### Bug Fixes

* **exporter:** redact SSO IdP secrets from the manifest ([#46](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/46)) ([52beb38](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/52beb38d816c8e45e3fa723f5db13cb43241c3c6))
* **orb:** capture bundles into captured/ dir with sanitized names (extract-project slug bug) ([#41](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/41)) ([72f5c55](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/72f5c55e5b7fc14b2fcabd48db1919b1a1877415))
* **orb:** import extract commands via &lt;&lt;include&gt;&gt; (RC009) ([e8cbc29](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/e8cbc29345d9220d101cc60dbade992d2760c032))
* **orb:** matrix example requires explicit matrix.alias; nicer merge job names ([feaad04](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/feaad0479e416b89eacc2797b37026831b30e368))
* **orb:** snake_case component/param names (RC010), fix example versions (RC011), re-enable orb-review ([88dbb7b](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/88dbb7b1b6082d0a1664fc0554181eebaf9685fb))
* **security:** never print token env values in --help output ([#42](https://github.com/AwesomeCICD/circleci-org-migration-cli/issues/42)) ([fe80500](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/fe805006d6f4e00dcb4a45b049f5f97628f7ebb6))

## [0.2.0](https://github.com/AwesomeCICD/circleci-org-migration-cli/compare/v0.1.0...v0.2.0) (2026-06-10)


### Features

* orb inline — inline private orbs into a config (overlap-window workaround) ([8997747](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/899774780ce6163c8dc8ff17517e209423695ad5))


### Bug Fixes

* **ci:** relax orb yamllint (line-length/document-start); drop release-as ([55cc52b](https://github.com/AwesomeCICD/circleci-org-migration-cli/commit/55cc52be8c90c3e373527fa34587370d1c644cdc))

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
