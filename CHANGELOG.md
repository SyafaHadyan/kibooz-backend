# Changelog

## [0.5.1](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.5.0...v0.5.1) (2026-10-09)


### Bug Fixes

* **auth:** accept only digits in a NISN and refuse names that are only spaces ([ead0650](https://github.com/SyafaHadyan/kibooz-backend/commit/ead065080c97e4681fe24a16ac5c0ad9d2773f3b))
* **auth:** keep the account rate limit on when a body field has another type or the path differs in case ([cbb2a33](https://github.com/SyafaHadyan/kibooz-backend/commit/cbb2a33603bcbc0b7597e44c7452e171f943d891))
* **auth:** store the first refresh token in the same transaction as the new account ([a9908b4](https://github.com/SyafaHadyan/kibooz-backend/commit/a9908b4d27f465df367eb632d348b52ed283c8e0))
* **ci:** publish latest and the sha tag from main only and never cancel a main or tag image run ([f6bceac](https://github.com/SyafaHadyan/kibooz-backend/commit/f6bceac622cb8787f6fc9d9885c4c948e6db92e4))
* **ci:** upload the Trivy filesystem results for Dependabot so the required Trivy check appears ([6db0f2a](https://github.com/SyafaHadyan/kibooz-backend/commit/6db0f2a52f15ff16dbe9de18fa5ff6a284e1142b))
* **classroom:** answer 404 and not 500 when a reply is posted to a thread that was just deleted ([f2d0480](https://github.com/SyafaHadyan/kibooz-backend/commit/f2d048002b43dc6e312817fc30a526e6a1148885))
* **classroom:** lock the video row for an edit and delete a video file only after the video is gone ([0aee047](https://github.com/SyafaHadyan/kibooz-backend/commit/0aee047b255aad796b1883390d0adf4299892cf1))
* **env:** refuse the sample JWT secret and values that are out of range, and load the timezone once ([d15ebff](https://github.com/SyafaHadyan/kibooz-backend/commit/d15ebff8c7e140b748153f5c9eae77321e8ee02b))
* **infra:** cancel the work of a request after REQUEST_TIMEOUT_SECONDS ([98cf89c](https://github.com/SyafaHadyan/kibooz-backend/commit/98cf89cdac5e2e8f814537ca38531ae59580a534))
* **infra:** end idle transactions and bound the calls to the bucket ([c983aa5](https://github.com/SyafaHadyan/kibooz-backend/commit/c983aa5999d1d89c392e5ec1484f7e1e42530cb9))
* **mood:** keep the microseconds of a mood log and order logs of the same moment by id on every screen ([9d748df](https://github.com/SyafaHadyan/kibooz-backend/commit/9d748df3fbd2413f5b3d91b1f6e20d9f29c3876f))
* **wali:** say that the guidance status is recorded and not forwarded ([224c0df](https://github.com/SyafaHadyan/kibooz-backend/commit/224c0df75d18c05ffc9b6c0617618436a500def9))


### Performance Improvements

* **db:** index learning videos by address ([03b5ca3](https://github.com/SyafaHadyan/kibooz-backend/commit/03b5ca3e0b7d5f09772d90170d577ec71de96cd8))

## [0.5.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.4.0...v0.5.0) (2026-10-09)


### Features

* **api:** add the remaining roadmap endpoints ([#75](https://github.com/SyafaHadyan/kibooz-backend/issues/75)) ([4d5fd9b](https://github.com/SyafaHadyan/kibooz-backend/commit/4d5fd9b638f5aa7411f49138aaea4b29814318c8))
* **classroom:** add learning videos and the class forum ([10062d6](https://github.com/SyafaHadyan/kibooz-backend/commit/10062d6bccece23f39ab2f1d4f08fbd0fb328af4))
* **classroom:** add learning videos and the class forum ([#74](https://github.com/SyafaHadyan/kibooz-backend/issues/74)) ([0613d34](https://github.com/SyafaHadyan/kibooz-backend/commit/0613d346d0c702c1a422c59a6eeac04fc9fa394a))
* **classroom:** edit and delete forum threads and replies ([8e88d6c](https://github.com/SyafaHadyan/kibooz-backend/commit/8e88d6cc389b8a25e08c8257c236831296bffa9b))
* **classroom:** edit and delete forum threads and replies ([#80](https://github.com/SyafaHadyan/kibooz-backend/issues/80)) ([c79a0b5](https://github.com/SyafaHadyan/kibooz-backend/commit/c79a0b5e502629feabbf665017e11a4daa61f55c))
* **classroom:** edit and delete learning videos ([ed1e7a0](https://github.com/SyafaHadyan/kibooz-backend/commit/ed1e7a046a782247b7ed42396920bf1fff98c770))
* **classroom:** edit and delete learning videos ([#79](https://github.com/SyafaHadyan/kibooz-backend/issues/79)) ([8f91599](https://github.com/SyafaHadyan/kibooz-backend/commit/8f91599397956706d427a99511a9d5566872a9ae))
* **classroom:** list the children of a class ([aa1eb7f](https://github.com/SyafaHadyan/kibooz-backend/commit/aa1eb7f9805a0a9506eefcf5bf0d058407efedff))
* **classroom:** mark a forum post as edited when its words change ([104df76](https://github.com/SyafaHadyan/kibooz-backend/commit/104df767da8cb44410954f711c8289869d7a3bb9))
* **classroom:** mark a forum post as edited when its words change ([#87](https://github.com/SyafaHadyan/kibooz-backend/issues/87)) ([f785810](https://github.com/SyafaHadyan/kibooz-backend/commit/f785810eca0e134a32f6a48fc416c2b9fb66af0c))
* **classroom:** stage uploaded videos under pending until they are added ([114894c](https://github.com/SyafaHadyan/kibooz-backend/commit/114894cd98266b6ae7aabee69b939affe8db27f3))
* **classroom:** stage uploaded videos under pending until they are added ([#85](https://github.com/SyafaHadyan/kibooz-backend/issues/85)) ([ceb1960](https://github.com/SyafaHadyan/kibooz-backend/commit/ceb19602b80cd6bd4238f49823f0307e6606505d))
* **classroom:** upload class videos through a signed address ([1356a0b](https://github.com/SyafaHadyan/kibooz-backend/commit/1356a0b50db4ca42bb37145fbb6f066860b9103e))
* **classroom:** upload class videos through a signed address ([#77](https://github.com/SyafaHadyan/kibooz-backend/issues/77)) ([4e00923](https://github.com/SyafaHadyan/kibooz-backend/commit/4e00923b4dd3bf36e7ed0fea4744658504413f4e))
* **db:** add learning video and forum post tables ([ef2609e](https://github.com/SyafaHadyan/kibooz-backend/commit/ef2609e690427c00ed63b73eb81633b92a93a30a))
* **db:** add the address of a teacher ([4b6a9c1](https://github.com/SyafaHadyan/kibooz-backend/commit/4b6a9c1ce1df59e594d5c48145be35fe93ac2502))
* **guru:** add classes and the teacher profile ([13bf2ee](https://github.com/SyafaHadyan/kibooz-backend/commit/13bf2ee04af9c6cdb28dfe1d2a83aef148f0e483))
* **storage:** copy an object inside the bucket ([8637edb](https://github.com/SyafaHadyan/kibooz-backend/commit/8637edb20e38b7eb6f921d3aa6b8b7088a02c64c))
* **storage:** sign video uploads and read object details ([dc6eb35](https://github.com/SyafaHadyan/kibooz-backend/commit/dc6eb350dbfcd2886aba9de59393102aa70914ed))
* **wali:** add the child profile and trash statistics ([ae6652d](https://github.com/SyafaHadyan/kibooz-backend/commit/ae6652d807fc961dd6e5fc0efa2f08508ed245e6))
* **wali:** return the class id of the child on the dashboard ([1f8b7b7](https://github.com/SyafaHadyan/kibooz-backend/commit/1f8b7b76fad47ae2536d2ba11c1442a9196e7e65))


### Bug Fixes

* **ci:** fetch Go 1.27.2 with GOTOOLCHAIN and read the base version from go.mod ([9723a62](https://github.com/SyafaHadyan/kibooz-backend/commit/9723a625c10f7bcb1c2e1a5478f4715f3a6c1016))
* **ci:** set up Go 1.27.2 by name because setup-go resets GOTOOLCHAIN ([d8b5195](https://github.com/SyafaHadyan/kibooz-backend/commit/d8b5195106bf19a778b9d7c7876b892ef5ad149a))
* **ci:** set up the newest Go 1.27 release without a toolchain line ([951eb4b](https://github.com/SyafaHadyan/kibooz-backend/commit/951eb4b5cf7fc732d16a150e1b0fc86f0ed1efdd))
* **classroom:** apply a forum edit to the locked post so a stale save cannot undo a newer one ([e51028e](https://github.com/SyafaHadyan/kibooz-backend/commit/e51028e6f27ba315d8c3d5f2996c1c0e8586b4cd))
* **classroom:** refuse a staged file that another request already added ([7e7014b](https://github.com/SyafaHadyan/kibooz-backend/commit/7e7014b33e60c6a99596658d20769906bab4c12b))
* **classroom:** serialize adding and deleting videos of one uploaded file ([fdcc9d5](https://github.com/SyafaHadyan/kibooz-backend/commit/fdcc9d572bcfa0f1dce46bf2d3f1e8b265de43d0))
* **deps:** keep the go directive and pick Go 1.27.2 with a toolchain line ([d81e79c](https://github.com/SyafaHadyan/kibooz-backend/commit/d81e79c8270ad4aff59f4fedfe72cd4149128044))
* **deps:** update x/net and Go to fix GO-2026-6617 ([1c673b4](https://github.com/SyafaHadyan/kibooz-backend/commit/1c673b415e33814fc4a1a27f251aa0ab15112695))
* **deps:** update x/net and Go to fix GO-2026-6617 ([#81](https://github.com/SyafaHadyan/kibooz-backend/issues/81)) ([92fc320](https://github.com/SyafaHadyan/kibooz-backend/commit/92fc32001fdef912af8c33e31d40a14e9b1749db))
* **docker:** build with the Go 1.27.2 image ([201141a](https://github.com/SyafaHadyan/kibooz-backend/commit/201141ab717751e1905a8598b659580076282881))
* **guidance:** leave the banner URL empty without a public base ([ad791dc](https://github.com/SyafaHadyan/kibooz-backend/commit/ad791dc35cea3db7157d11087ef29509e3d3bc66))
* **guidance:** leave the banner URL empty without a public base ([#78](https://github.com/SyafaHadyan/kibooz-backend/issues/78)) ([f411248](https://github.com/SyafaHadyan/kibooz-backend/commit/f4112487db9e1fc6590ee06d2b9f0e9d1859923e))
* **k6:** declare the k6 globals and document the helper functions ([b8b8185](https://github.com/SyafaHadyan/kibooz-backend/commit/b8b8185e935eb8ecef3497bc41b44c35a475f021))
* **k6:** declare the k6 globals and document the helper functions ([#86](https://github.com/SyafaHadyan/kibooz-backend/issues/86)) ([a324454](https://github.com/SyafaHadyan/kibooz-backend/commit/a324454cd46affb5eff3df9ca959c924cd87b98d))

## [0.4.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.3.0...v0.4.0) (2026-10-08)


### ⚠ BREAKING CHANGES

* **limiter:** limit by user and account instead of IP ([#58](https://github.com/SyafaHadyan/kibooz-backend/issues/58))
* **limiter:** limit by user and account instead of IP

### Features

* **auth:** give returning devices their own login rate limit ([dd3426d](https://github.com/SyafaHadyan/kibooz-backend/commit/dd3426dafb87c293ae8372dce8762909258e4fb2))
* **auth:** give returning devices their own login rate limit ([#59](https://github.com/SyafaHadyan/kibooz-backend/issues/59)) ([cd23989](https://github.com/SyafaHadyan/kibooz-backend/commit/cd239894d79171511a556150c1502787f51a3629))
* **dast:** add a weekly ZAP active scan ([7e455ae](https://github.com/SyafaHadyan/kibooz-backend/commit/7e455ae3f67b7394e36029710e5c09ae75c3af1f))
* **dast:** add a weekly ZAP active scan ([#51](https://github.com/SyafaHadyan/kibooz-backend/issues/51)) ([af022dd](https://github.com/SyafaHadyan/kibooz-backend/commit/af022dd717eb2353ba21cba5514eb305ec9f8ef9))
* **dast:** show how many requests the active scan sent ([c5584dd](https://github.com/SyafaHadyan/kibooz-backend/commit/c5584ddeadee4645f8966295c18022fb008b19eb))
* **docs:** add a favicon to the API reference ([2d3ab31](https://github.com/SyafaHadyan/kibooz-backend/commit/2d3ab31bd073a70eeb2007fb84684a947a7a8329))
* **docs:** add a favicon to the API reference ([#69](https://github.com/SyafaHadyan/kibooz-backend/issues/69)) ([17cecc7](https://github.com/SyafaHadyan/kibooz-backend/commit/17cecc7fffbf9fad9b350903c71d41bab718f4f2))
* **limiter:** limit by user and account instead of IP ([373f87c](https://github.com/SyafaHadyan/kibooz-backend/commit/373f87cb6b7b1a6f26f52bf5f70f848a467bce82))
* **limiter:** limit by user and account instead of IP ([#58](https://github.com/SyafaHadyan/kibooz-backend/issues/58)) ([8974bde](https://github.com/SyafaHadyan/kibooz-backend/commit/8974bdec9e048fef18654bef42dbfdd8749eec7d))


### Bug Fixes

* **auth:** reject a device token lifetime below one day ([f9538c4](https://github.com/SyafaHadyan/kibooz-backend/commit/f9538c466dd9799436ea4a891048ef7e77d5cc1d))
* **dast:** move the ZAP proxy off the port of the API ([fe146d7](https://github.com/SyafaHadyan/kibooz-backend/commit/fe146d720debcec40badbd07f4e70b4ca2dcd9d1))
* **dast:** print the request counts in the log as well ([e858359](https://github.com/SyafaHadyan/kibooz-backend/commit/e85835986bfbf5d70b2338a14c210080769ad7ce))
* **limiter:** limit refresh and logout by token and login by email only ([9c49238](https://github.com/SyafaHadyan/kibooz-backend/commit/9c49238440fe36df86daa0a685271aad0c3c459e))
* **limiter:** use a neutral key prefix for the password limiter ([e1823a9](https://github.com/SyafaHadyan/kibooz-backend/commit/e1823a9937449dcdf7d1928151650294ffdce093))

## [0.3.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.2.1...v0.3.0) (2026-10-07)


### Features

* **compose:** add a soft Go memory limit and a Redis memory cap ([dc9dadb](https://github.com/SyafaHadyan/kibooz-backend/commit/dc9dadbe34a28c6a8c3b0fd021ed76af44af6434))
* **compose:** add memory settings for the API and Redis ([#32](https://github.com/SyafaHadyan/kibooz-backend/issues/32)) ([fcf4069](https://github.com/SyafaHadyan/kibooz-backend/commit/fcf40695dc300aed6a6b4c349418f76286a76d36))
* **db:** add soft delete columns and active-only unique indexes ([5fa3362](https://github.com/SyafaHadyan/kibooz-backend/commit/5fa3362eae21e940c1a02a9e5fa0f056b3fc93ed))
* **docs:** add security headers and a build script for the documentation site ([e84e1a6](https://github.com/SyafaHadyan/kibooz-backend/commit/e84e1a65c610bdaa30571c54fe422712d4a366e7))
* **docs:** harden and publish the documentation site ([#45](https://github.com/SyafaHadyan/kibooz-backend/issues/45)) ([a042e04](https://github.com/SyafaHadyan/kibooz-backend/commit/a042e0459556043156a14ac5bf381d5e744a9ad9))
* **docs:** serve the documentation site on its own domain only ([03ef63e](https://github.com/SyafaHadyan/kibooz-backend/commit/03ef63e410f12afd2dccad6032b9e373f96d00ed))
* **http:** add security headers to every response ([e9578d7](https://github.com/SyafaHadyan/kibooz-backend/commit/e9578d7780f81d8e31ba60c1db1f54a07d0bc90d))
* **http:** add security headers to every response ([#36](https://github.com/SyafaHadyan/kibooz-backend/issues/36)) ([7f76f1d](https://github.com/SyafaHadyan/kibooz-backend/commit/7f76f1d1b561670b872af17e626955a84365757a))
* **keepalive:** ping the database and Redis on a timer ([26ce29d](https://github.com/SyafaHadyan/kibooz-backend/commit/26ce29db9d51a71fdf8dcddcf570e5dd9d9f8686))
* **keepalive:** ping the database and Redis on a timer ([#20](https://github.com/SyafaHadyan/kibooz-backend/issues/20)) ([8e28644](https://github.com/SyafaHadyan/kibooz-backend/commit/8e2864415d39e97399376666a0c936873e1f37a6))
* **storage:** add object deletion and a best-effort discard helper ([15525db](https://github.com/SyafaHadyan/kibooz-backend/commit/15525db38d52f51383a236cc41655d22a2808a57))
* **user:** add account deletion endpoint ([b2641af](https://github.com/SyafaHadyan/kibooz-backend/commit/b2641af5f65529fcc485c1248f0d0f62676bdca5))
* **user:** add soft delete and account deletion ([#19](https://github.com/SyafaHadyan/kibooz-backend/issues/19)) ([8bf9a42](https://github.com/SyafaHadyan/kibooz-backend/commit/8bf9a42254d2a8995ba9e52ab19fe1c783e4f736))


### Bug Fixes

* **api:** correct the license in the OpenAPI description ([eb0f221](https://github.com/SyafaHadyan/kibooz-backend/commit/eb0f221fceccdba70bc5a180c8532a8f183ea11d))
* **api:** correct the license in the OpenAPI description ([#46](https://github.com/SyafaHadyan/kibooz-backend/issues/46)) ([d06143f](https://github.com/SyafaHadyan/kibooz-backend/commit/d06143fed604192ad230ae921adb284be06e5446))
* **auth:** block joining a class without an active teacher ([5312129](https://github.com/SyafaHadyan/kibooz-backend/commit/5312129417398fa070414e5fef01b92ee9dfb543))
* **auth:** block joining a class without an active teacher ([#24](https://github.com/SyafaHadyan/kibooz-backend/issues/24)) ([2f09d0e](https://github.com/SyafaHadyan/kibooz-backend/commit/2f09d0ece6cce1b45b645ee9f852b563b97d7b34))
* **auth:** keep refresh retryable after a database error and reject overlong passwords ([b84410f](https://github.com/SyafaHadyan/kibooz-backend/commit/b84410f53251f376025474feb80b75dc7341329a))
* **auth:** keep refresh retryable after a database error and reject overlong passwords ([#23](https://github.com/SyafaHadyan/kibooz-backend/issues/23)) ([05630d3](https://github.com/SyafaHadyan/kibooz-backend/commit/05630d3f6149dcd33a36be4701602bb35e9eb3d0))
* **db:** quote the postgres connection string values ([635d2de](https://github.com/SyafaHadyan/kibooz-backend/commit/635d2de008be63c02cf5f464960015ef274aae83))
* **db:** quote the postgres connection string values ([#17](https://github.com/SyafaHadyan/kibooz-backend/issues/17)) ([d38749b](https://github.com/SyafaHadyan/kibooz-backend/commit/d38749b2cbd7def77900ad0f37f0c889722bdca6))
* **docker:** use the numeric uid for the non-root user ([d9deabd](https://github.com/SyafaHadyan/kibooz-backend/commit/d9deabd707123cea8f2f50eacb679330d742a86a))
* **perf:** generate the k6 account password at runtime ([d0b07b2](https://github.com/SyafaHadyan/kibooz-backend/commit/d0b07b251ed3cd3b3d99b74cde5864c30273a347))
* **trash:** ignore deleted accounts and children in raw lookups ([6e59503](https://github.com/SyafaHadyan/kibooz-backend/commit/6e59503cf4a075512e4c644da83cb3463da0d8ae))
* **trash:** ignore deleted accounts and children in raw lookups ([#22](https://github.com/SyafaHadyan/kibooz-backend/issues/22)) ([a7c427a](https://github.com/SyafaHadyan/kibooz-backend/commit/a7c427af7e67dcb9b2630daf8b41e548c67d1566))
* **trash:** remove the uploaded photo when the claim fails ([c918673](https://github.com/SyafaHadyan/kibooz-backend/commit/c918673f58ed8bbb41256292c6fca7efeaeca15f))
* **trash:** remove uploaded photos when the request fails ([#28](https://github.com/SyafaHadyan/kibooz-backend/issues/28)) ([6425fcb](https://github.com/SyafaHadyan/kibooz-backend/commit/6425fcbc6136478d5ff8bd41d3742dc1c259f759))
* **user:** delete the avatar file a new upload replaces ([84c5ce3](https://github.com/SyafaHadyan/kibooz-backend/commit/84c5ce3e770d95357925be29346d59eb49be00b7))
* **user:** delete the avatar file a new upload replaces ([#34](https://github.com/SyafaHadyan/kibooz-backend/issues/34)) ([02f16f3](https://github.com/SyafaHadyan/kibooz-backend/commit/02f16f35ae3f26d0061314978f20d0d8e48a1702))
* **user:** refuse avatar uploads from a deleted account ([bafdf5f](https://github.com/SyafaHadyan/kibooz-backend/commit/bafdf5fd21ed31d4c62e777fa7bdcc28940d0d6f))
* **user:** reject overlong passwords when deleting an account ([69a4059](https://github.com/SyafaHadyan/kibooz-backend/commit/69a40595b372d31cebe6f874bf8d77a5f18b878f))
* **user:** remove the uploaded avatar when the update is refused ([02a2953](https://github.com/SyafaHadyan/kibooz-backend/commit/02a2953d6ce026051db04fdc63950264681c335f))

## [0.2.1](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.2.0...v0.2.1) (2026-10-05)


### Bug Fixes

* **docker:** move the base image to distroless debian13 ([380e787](https://github.com/SyafaHadyan/kibooz-backend/commit/380e787a9e9bd204a87151c96e5e0a3a575255d0))
* **docker:** move the base image to distroless debian13 ([#13](https://github.com/SyafaHadyan/kibooz-backend/issues/13)) ([60ec204](https://github.com/SyafaHadyan/kibooz-backend/commit/60ec2040467bcf2a1287d9681568a7bee55b4e08))

## [0.2.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.1.0...v0.2.0) (2026-10-05)


### Features

* **docker:** publish the image to github container registry ([07ffd75](https://github.com/SyafaHadyan/kibooz-backend/commit/07ffd75a696f5de53a2fc3ebd8510402a2dadbb1))
* **docker:** publish the image to github container registry ([#11](https://github.com/SyafaHadyan/kibooz-backend/issues/11)) ([0bfaf0e](https://github.com/SyafaHadyan/kibooz-backend/commit/0bfaf0e01d86a63e1b1dac51bf1f28bf98e570ed))


### Bug Fixes

* **release:** drop the component name from release tags ([f1d3e05](https://github.com/SyafaHadyan/kibooz-backend/commit/f1d3e051f6dd9a715391e1ffd5fe0fdc8eebd7b8))
* **release:** drop the component name from release tags ([#9](https://github.com/SyafaHadyan/kibooz-backend/issues/9)) ([2ae2d64](https://github.com/SyafaHadyan/kibooz-backend/commit/2ae2d64be7c004ffe6f1c42ced2e588a0db8d77a))

## 0.1.0 (2026-10-05)


### Features

* **app:** wire modules, health check and graceful shutdown ([4127a9f](https://github.com/SyafaHadyan/kibooz-backend/commit/4127a9fb6ed53530832a360220cca007980445d7))
* **auth:** add register, login, refresh-token and logout with postgres backed refresh tokens ([f782114](https://github.com/SyafaHadyan/kibooz-backend/commit/f782114f218abf7664ce92b1d23536cf6d6e9079))
* **backend:** add kibooz rest api, container setup and ci/cd ([1b27e36](https://github.com/SyafaHadyan/kibooz-backend/commit/1b27e36cf13479bd3a9f9ae074c48738cc5bf71f))
* **core:** add config, jwt, optional redis, s3, http server, validation and auth middleware ([f2ddfcd](https://github.com/SyafaHadyan/kibooz-backend/commit/f2ddfcd6100d3790c4061291c70e1c280e9b941e))
* **db:** add postgres connection and schema migrations ([00925d2](https://github.com/SyafaHadyan/kibooz-backend/commit/00925d23582f8d55d525ba54d2b460c66c7d21a6))
* **guru:** add teacher dashboard, mood logging and mood analytics endpoints ([7b9d8cb](https://github.com/SyafaHadyan/kibooz-backend/commit/7b9d8cbf73c75775a064aa5552399e34457bc8b0))
* **redis:** add REDIS_TLS option for hosted redis ([#6](https://github.com/SyafaHadyan/kibooz-backend/issues/6)) ([3271b85](https://github.com/SyafaHadyan/kibooz-backend/commit/3271b858b75fa37a9128f7793f8f59a68c7fdb6b))
* **trash:** add point claim with daily limit and cached class leaderboard ([bb0209c](https://github.com/SyafaHadyan/kibooz-backend/commit/bb0209c7f8f81076a9f408f0e1f256b00beef197))
* **user:** add avatar upload for users and children ([41477d2](https://github.com/SyafaHadyan/kibooz-backend/commit/41477d26d6ba45f5497ced87064053685026762f))
* **wali:** add parent dashboard and guidance confirmation endpoints ([c78a14a](https://github.com/SyafaHadyan/kibooz-backend/commit/c78a14ac69cf182f3e7473f54f283adacbbd04c0))


### Bug Fixes

* **compose:** use fixed container ports and make redis optional for the api ([#7](https://github.com/SyafaHadyan/kibooz-backend/issues/7)) ([16199f3](https://github.com/SyafaHadyan/kibooz-backend/commit/16199f397b477e7562bc813977362e8ea9c4bdfc))
* **docker:** skip the registry push for dependabot pull requests ([#5](https://github.com/SyafaHadyan/kibooz-backend/issues/5)) ([b210a92](https://github.com/SyafaHadyan/kibooz-backend/commit/b210a92a5177f5bd906f972c42ca826bfa5dff77))
* **release:** start versioning at 0.1.0 ([#4](https://github.com/SyafaHadyan/kibooz-backend/issues/4)) ([51de88f](https://github.com/SyafaHadyan/kibooz-backend/commit/51de88f789edd2c7d0e62dde375f02e966a7acb0))
