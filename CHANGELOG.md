# Changelog

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
