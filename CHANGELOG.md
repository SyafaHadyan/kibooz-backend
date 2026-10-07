# Changelog

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
