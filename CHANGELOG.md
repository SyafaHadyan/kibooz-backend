# Changelog

## [0.18.1](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.18.0...v0.18.1) (2026-10-22)


### Bug Fixes

* **db:** bound the first ping and say what the ten seconds cover ([0e1b24d](https://github.com/SyafaHadyan/kibooz-backend/commit/0e1b24d339d95bc27c5ec68dd6d2d57a70bcf292))
* **db:** give up on a database that does not answer after ten seconds ([e65d3e0](https://github.com/SyafaHadyan/kibooz-backend/commit/e65d3e0571130a24f27e4ed0fb3b1232210e2e55))
* **db:** give up on a database that does not answer after ten seconds ([#331](https://github.com/SyafaHadyan/kibooz-backend/issues/331)) ([c4b9762](https://github.com/SyafaHadyan/kibooz-backend/commit/c4b9762c4acc3c379c8726cfd987a852fbe3a878))
* **main:** end the process at once on a second stop signal while it starts ([55df901](https://github.com/SyafaHadyan/kibooz-backend/commit/55df90187d38f70b6742a5743729552343b1235d))
* **main:** let the migrations finish when the process is told to stop while it starts ([86633a2](https://github.com/SyafaHadyan/kibooz-backend/commit/86633a2c61755041cc11d592c87ad1de7bab1109))
* **main:** let the migrations finish when the process is told to stop while it starts ([#326](https://github.com/SyafaHadyan/kibooz-backend/issues/326)) ([9e1c1d1](https://github.com/SyafaHadyan/kibooz-backend/commit/9e1c1d195494ad1b4fa3cf14c328f2e50e6591ea))
* **season:** ignore an older semester and keep going past a class that fails ([76447f5](https://github.com/SyafaHadyan/kibooz-backend/commit/76447f572bdcdb7bd308707b84c3e5df847a6c28))
* **season:** lock the history table while the key is added and test the skipped save ([b88cffb](https://github.com/SyafaHadyan/kibooz-backend/commit/b88cffbdd2fcddb74d545fe9972c50938978f585))
* **season:** make the database refuse a second save of the same standings ([3730407](https://github.com/SyafaHadyan/kibooz-backend/commit/373040705b86c673b89ca121e471dbf95c1df410))
* **season:** make the database refuse a second save of the same standings ([#330](https://github.com/SyafaHadyan/kibooz-backend/issues/330)) ([f0c0ad6](https://github.com/SyafaHadyan/kibooz-backend/commit/f0c0ad6267eced63082bd3e1750a4427ccb35d55))
* **season:** reset the leaderboard one class at a time so that it scales and survives a stopped copy ([149d5aa](https://github.com/SyafaHadyan/kibooz-backend/commit/149d5aaf1ef0ce76b7db0ceae5475e14aaa1b495))
* **season:** reset the leaderboard one class at a time so that it scales and survives a stopped copy ([#328](https://github.com/SyafaHadyan/kibooz-backend/issues/328)) ([9d28e79](https://github.com/SyafaHadyan/kibooz-backend/commit/9d28e794bd684437b3a6c8e0d55980d06ba1d5ff))

## [0.18.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.17.1...v0.18.0) (2026-10-22)


### Features

* **chat:** keep direct chats open after a class is archived ([a30c305](https://github.com/SyafaHadyan/kibooz-backend/commit/a30c30529f54a990825222ce0b74218c457bee66))
* **classes:** let the owner archive a class and any teacher restore it with a new code ([8a0ab61](https://github.com/SyafaHadyan/kibooz-backend/commit/8a0ab614f6f363163556e320189281beacb30c3a))
* **classes:** let the owner archive a class and any teacher restore it with a new code ([#321](https://github.com/SyafaHadyan/kibooz-backend/issues/321)) ([c58d600](https://github.com/SyafaHadyan/kibooz-backend/commit/c58d600eb0b96bb405b0b834f91b0499caed92c2))
* **classes:** make an archived class read only and show when it was archived ([46dd57d](https://github.com/SyafaHadyan/kibooz-backend/commit/46dd57d01e37ce60513db35dbb5edc34567117cb))
* **classes:** make an archived class read only and show when it was archived ([#320](https://github.com/SyafaHadyan/kibooz-backend/issues/320)) ([4d28fcd](https://github.com/SyafaHadyan/kibooz-backend/commit/4d28fcd59be1460138437db559fc9758df142c24))
* **enrollments:** add the enrollments table and backfill existing children ([#311](https://github.com/SyafaHadyan/kibooz-backend/issues/311)) ([a59de37](https://github.com/SyafaHadyan/kibooz-backend/commit/a59de377a7d8c644b19f4427c64a02a60189de32))
* **enrollments:** add the enrollments table, keep it in step with students and backfill existing children ([402a734](https://github.com/SyafaHadyan/kibooz-backend/commit/402a734fa8bddfa1a6d8a3e589a6fb5774936a5a))
* **enrollments:** let a parent ask for a move and a teacher of the new class decide ([4bd3fd3](https://github.com/SyafaHadyan/kibooz-backend/commit/4bd3fd331b434e4d776f4289fa3eda1ca0cdef25))
* **enrollments:** let a parent ask for a move and a teacher of the new class decide ([#314](https://github.com/SyafaHadyan/kibooz-backend/issues/314)) ([6f3b133](https://github.com/SyafaHadyan/kibooz-backend/commit/6f3b13315d53f06461ae4ff5a177bb87d511ec15))
* **enrollments:** let a parent list the classes that a child left ([d5203f6](https://github.com/SyafaHadyan/kibooz-backend/commit/d5203f64e751ccad7d78ca0015adea62a7e774d2))
* **enrollments:** let a parent list the classes that a child left ([#315](https://github.com/SyafaHadyan/kibooz-backend/issues/315)) ([4051c1b](https://github.com/SyafaHadyan/kibooz-backend/commit/4051c1be8ada03094ed4e0d7d91db8555f1fd334))
* **enrollments:** let a teacher of the new class read what a child did before ([#319](https://github.com/SyafaHadyan/kibooz-backend/issues/319)) ([6df2398](https://github.com/SyafaHadyan/kibooz-backend/commit/6df23981373249aca11f7e68ed512bf419e82728))
* **enrollments:** let a teacher of the new class read what a child did in the classes it left ([7983363](https://github.com/SyafaHadyan/kibooz-backend/commit/7983363378a5ff105d0d708eda7f3bc61911427a))
* **leaderboard:** limit the leaderboard and the trash stats to a day, week or month ([5ec1759](https://github.com/SyafaHadyan/kibooz-backend/commit/5ec1759a92cd52d4895b181a84f222c65a679ed7))
* **leaderboard:** limit the leaderboard and the trash stats to a day, week or month ([#308](https://github.com/SyafaHadyan/kibooz-backend/issues/308)) ([0d868ee](https://github.com/SyafaHadyan/kibooz-backend/commit/0d868eed6ed9d99d677654ac4b61fb268f3d1ae9))
* **ops:** add a script that updates the API copies of a host one at a time ([3ea6941](https://github.com/SyafaHadyan/kibooz-backend/commit/3ea69416cced8d5d940b38a3784076633cccb779))
* **ops:** add a script that updates the API copies of a host one at a time ([#299](https://github.com/SyafaHadyan/kibooz-backend/issues/299)) ([4fea5ff](https://github.com/SyafaHadyan/kibooz-backend/commit/4fea5ff3fce8a359789d90e9a4f428a060a9987e))
* **ops:** warn before the update script goes back and stop when migrations were added ([6cf27ca](https://github.com/SyafaHadyan/kibooz-backend/commit/6cf27ca61200cd7da900a2fb5fc6d0c27e638904))
* **ops:** warn before the update script goes back and stop when migrations were added ([#302](https://github.com/SyafaHadyan/kibooz-backend/issues/302)) ([b2fe92f](https://github.com/SyafaHadyan/kibooz-backend/commit/b2fe92f4532f61862dd3de5fbc734ea33ae31cfb))


### Bug Fixes

* **classes:** count every teacher at the restore limit and check the owner before the ranking lock ([5e5a55d](https://github.com/SyafaHadyan/kibooz-backend/commit/5e5a55d424eb61df1893e1fbea2f8bb73c0aa1ba))
* **classes:** keep the archived error, order the default class and tell parents ([1d5faf4](https://github.com/SyafaHadyan/kibooz-backend/commit/1d5faf4fa1d061e9d49bf0c14d4a5a40033e1560))
* **enrollments:** reach the saved numbers check with a pending row that only has final points ([4efb65b](https://github.com/SyafaHadyan/kibooz-backend/commit/4efb65b46b8722ba0b270f57cc17ed324bbc1887))
* **enrollments:** refuse a mood dated before the child joined its class ([34da68e](https://github.com/SyafaHadyan/kibooz-backend/commit/34da68e29433f5c752f11bc152b7e51322c53a86))
* **enrollments:** run the backfill test through the plain pool, name the checks it expects and word the docs exactly ([e99eca7](https://github.com/SyafaHadyan/kibooz-backend/commit/e99eca7529150e13ea14c983fc08457ccbd8add5))
* **ops:** give every compose command the checked IMAGE_TAG even when the shell exports another ([a85c99e](https://github.com/SyafaHadyan/kibooz-backend/commit/a85c99e13f54d0d2078afc9f3e670c5095cd377a))
* **ops:** list the migrations of both tags, stop when they cannot be checked and say when the running version is unknown ([8d3022f](https://github.com/SyafaHadyan/kibooz-backend/commit/8d3022f340ba8f3876b219f92ed8c4baf7aa8d62))
* **ops:** pull before the env edit, check the services, accept a disabled storage and name the old and new copies ([f65a971](https://github.com/SyafaHadyan/kibooz-backend/commit/f65a97193aed3f6bf2305e7fb762b99f6ebcaf88))
* **ops:** take the newest version of all copies as the running one before a downgrade ([eef20ad](https://github.com/SyafaHadyan/kibooz-backend/commit/eef20ad690049394d851344b6105a43ad7cfe80f))

## [0.17.1](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.17.0...v0.17.1) (2026-10-21)


### Bug Fixes

* **auth:** shorten the default access token lifetime to 15 minutes ([4b7582c](https://github.com/SyafaHadyan/kibooz-backend/commit/4b7582c90bb05825f6edcfc6e66e39645c10f30b))
* **auth:** shorten the default access token lifetime to 15 minutes ([#281](https://github.com/SyafaHadyan/kibooz-backend/issues/281)) ([e29d1d7](https://github.com/SyafaHadyan/kibooz-backend/commit/e29d1d751f38d28e1014734f5601a3c9ca7a9be6))
* **docs:** pin undici to 7.30.1 in the docs tooling ([#280](https://github.com/SyafaHadyan/kibooz-backend/issues/280)) ([94f21e5](https://github.com/SyafaHadyan/kibooz-backend/commit/94f21e55ce2e95721fc734fad2af1445a38068de))
* **fuzz:** reject a malformed fuzztime and keep the failed input when the job stops early ([a7bf4ad](https://github.com/SyafaHadyan/kibooz-backend/commit/a7bf4ad3617888f50110bc8242bc405ddcac62ea))
* **monitoring:** do not name one host in the copies alert text ([bbcdb92](https://github.com/SyafaHadyan/kibooz-backend/commit/bbcdb921a229e1066f89badfec4e155589a38727))
* **monitoring:** fire the copies alert only when fewer than 2 API copies are up ([4285a8f](https://github.com/SyafaHadyan/kibooz-backend/commit/4285a8f2c5e3313e8d6ec046d000fa79e095ee7f))
* **monitoring:** fire the copies alert only when fewer than 2 API copies are up ([#285](https://github.com/SyafaHadyan/kibooz-backend/issues/285)) ([df457a6](https://github.com/SyafaHadyan/kibooz-backend/commit/df457a6b66f4d67229054c9dbd3c5c2f9c947ccb))
* **pentest:** keep a running target, fail on a bad seed, tighten the inputs and document the limits ([b656a48](https://github.com/SyafaHadyan/kibooz-backend/commit/b656a489bbf1c31be984f140537318469460f716))
* **perf:** hold one socket per listener, fail a run without delivered frames and document the chat script ([b705d8f](https://github.com/SyafaHadyan/kibooz-backend/commit/b705d8f872a69ba2fdfd1649b7cc1438a7232217))

## [0.17.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.16.0...v0.17.0) (2026-10-21)


### ⚠ BREAKING CHANGES

* **env:** rename VIDEO_MAX_MB and S3_BUCKET_NAME and refuse the old names ([#273](https://github.com/SyafaHadyan/kibooz-backend/issues/273))
* **env:** rename VIDEO_MAX_MB and S3_BUCKET_NAME and refuse the old names

### Bug Fixes

* **auth:** keep a used refresh token until its session reaches its longest lifetime ([616b710](https://github.com/SyafaHadyan/kibooz-backend/commit/616b710fffa530a8f0b4288c11128ce7316b9be9))
* **auth:** keep a used refresh token until its session reaches its longest lifetime ([#276](https://github.com/SyafaHadyan/kibooz-backend/issues/276)) ([abf93c9](https://github.com/SyafaHadyan/kibooz-backend/commit/abf93c9527c8cf055406f227c0e6d717e214c4ad))
* **chat:** send a new room message only to the members who had joined when it was sent ([13110db](https://github.com/SyafaHadyan/kibooz-backend/commit/13110db13628ffbfea05c86c1e8db28b22915c6e))
* **chat:** send a new room message only to the members who had joined when it was sent ([#275](https://github.com/SyafaHadyan/kibooz-backend/issues/275)) ([d2c7b15](https://github.com/SyafaHadyan/kibooz-backend/commit/d2c7b1530e47de02d9fb642be12aa6e45a3dfd25))
* **guru:** check the teacher who rotates the join code again once the class lock is held ([9fbadef](https://github.com/SyafaHadyan/kibooz-backend/commit/9fbadef279077959a931953410695e2149fb539e))
* **guru:** check the teacher who rotates the join code again once the class lock is held ([#274](https://github.com/SyafaHadyan/kibooz-backend/issues/274)) ([f5d68d4](https://github.com/SyafaHadyan/kibooz-backend/commit/f5d68d4bd74ad1308686f10c743d276df2b43afd))
* **guru:** require an active teacher account in the recheck of adding a teacher ([c51e043](https://github.com/SyafaHadyan/kibooz-backend/commit/c51e043c3002b6d0b57edd153d7d385aec51c276))
* **guru:** require an active teacher account in the recheck of adding a teacher ([#278](https://github.com/SyafaHadyan/kibooz-backend/issues/278)) ([6a3538d](https://github.com/SyafaHadyan/kibooz-backend/commit/6a3538dd4e8f98e13aa775e460b9318bfcbf45a5))
* **guru:** require an active teacher account in the recheck of the join code rotation ([c3672c3](https://github.com/SyafaHadyan/kibooz-backend/commit/c3672c3b6f54bf3f9098c8f0f70621e3cc3ad883))


### Code Refactoring

* **env:** rename VIDEO_MAX_MB and S3_BUCKET_NAME and refuse the old names ([f06faf1](https://github.com/SyafaHadyan/kibooz-backend/commit/f06faf1a206731b0200e1476f83c4274512912f7))
* **env:** rename VIDEO_MAX_MB and S3_BUCKET_NAME and refuse the old names ([#273](https://github.com/SyafaHadyan/kibooz-backend/issues/273)) ([f3a8c0f](https://github.com/SyafaHadyan/kibooz-backend/commit/f3a8c0f6cd4bc89ec07f54a22d9d9618a701baf0))

## [0.16.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.15.0...v0.16.0) (2026-10-21)


### Features

* **chat:** add the room of a class to the chat ([988d44d](https://github.com/SyafaHadyan/kibooz-backend/commit/988d44d5828e549104e3b2a121f2d504672225fc))
* **chat:** add the room of a class to the chat ([#260](https://github.com/SyafaHadyan/kibooz-backend/issues/260)) ([bdaac41](https://github.com/SyafaHadyan/kibooz-backend/commit/bdaac41935e42db657f3d301b89b0895ea523e52))
* **chat:** push chat events to open sockets ([f72ea56](https://github.com/SyafaHadyan/kibooz-backend/commit/f72ea56f8b8881100d6e6d0e1f6e0108ea940225))
* **chat:** push chat events to open sockets ([#265](https://github.com/SyafaHadyan/kibooz-backend/issues/265)) ([134989e](https://github.com/SyafaHadyan/kibooz-backend/commit/134989ea982e607e926005d36c99afd286d0c189))
* **chat:** send photos and videos through a private bucket ([2c97a85](https://github.com/SyafaHadyan/kibooz-backend/commit/2c97a85eb94d29199d3319d78f9db3fe2a3c4d5a))
* **chat:** send photos and videos through a private bucket ([#263](https://github.com/SyafaHadyan/kibooz-backend/issues/263)) ([c193a64](https://github.com/SyafaHadyan/kibooz-backend/commit/c193a64db3ddb61b78e93c441bde982a8584bcb3))
* **push:** save the push token of an installed app ([95ebc5c](https://github.com/SyafaHadyan/kibooz-backend/commit/95ebc5c56f94ad6cf1fef75f66e2d473cdfa561c))
* **push:** save the push token of an installed app ([#266](https://github.com/SyafaHadyan/kibooz-backend/issues/266)) ([777c2ff](https://github.com/SyafaHadyan/kibooz-backend/commit/777c2ff36a28df387565ef84c40a7464b43e2357))
* **push:** wake a closed chat app through Firebase when a message arrives ([21aff28](https://github.com/SyafaHadyan/kibooz-backend/commit/21aff28e5c3819223d32f71e446f2518c2d14d24))
* **push:** wake a closed chat app through Firebase when a message arrives ([#271](https://github.com/SyafaHadyan/kibooz-backend/issues/271)) ([929a412](https://github.com/SyafaHadyan/kibooz-backend/commit/929a412597c8bc6089d7ec2793157dad40262437))


### Bug Fixes

* **chat:** announce a deleted room message only to the members who had joined ([a0d4607](https://github.com/SyafaHadyan/kibooz-backend/commit/a0d46073cff4707595284fd9c7671614657e7488))
* **chat:** build the signing key of the socket tests at run time and ignore its first version ([7f0f077](https://github.com/SyafaHadyan/kibooz-backend/commit/7f0f077f9b1cddcebc3ed5ae2c442c7160f90f65))
* **chat:** close the oldest socket of an account first ([57e3404](https://github.com/SyafaHadyan/kibooz-backend/commit/57e34048f7c63240a994171e9fc394cff2ab87ec))
* **chat:** end the socket with its writer, refuse frames after the token ends and let a stopped typing hint through ([ba390b3](https://github.com/SyafaHadyan/kibooz-backend/commit/ba390b3ae949c99b9be116101e901d2edab84782))
* **chat:** give the nullable media a type ([59bc79f](https://github.com/SyafaHadyan/kibooz-backend/commit/59bc79fd1555c373d7ba9d6be736770001cbd54a))
* **chat:** give the nullable peer and class a type and read the teacher name in the room test ([8f15cc7](https://github.com/SyafaHadyan/kibooz-backend/commit/8f15cc7436856de0fac6c6e541b3aa2992caa6ef))
* **chat:** hide older room messages from a late joiner when deleting and word the room access rules precisely ([ec816f1](https://github.com/SyafaHadyan/kibooz-backend/commit/ec816f1ba066a60524ba728673f6b9a1d546f3df))
* **chat:** keep the media test from leaving a file on the cleanup queue ([468b984](https://github.com/SyafaHadyan/kibooz-backend/commit/468b984075950bfec1ab7dbb369b4bab6c79d539))
* **chat:** let the chat photo upload carry a photo and keep the photo limit inside the body limit ([187a099](https://github.com/SyafaHadyan/kibooz-backend/commit/187a09908d458fa434a1ead64f6b0cbd699694c3))
* **chat:** put a deadline on the database work of a typing frame ([fe868db](https://github.com/SyafaHadyan/kibooz-backend/commit/fe868dbc90d357de5a05d6c7e5e785e580d445fc))
* **ci:** ignore the first version of a socket test key in the secret scan ([de57f94](https://github.com/SyafaHadyan/kibooz-backend/commit/de57f9453f6713cb26b0dcec61fa3530169bab10))
* **ci:** ignore the first version of a socket test key in the secret scan ([08fb5bf](https://github.com/SyafaHadyan/kibooz-backend/commit/08fb5bf90d79d6c2a8dd50a44d3540b41b55d83c))
* **ci:** ignore the first version of a socket test key in the secret scan ([76b79cd](https://github.com/SyafaHadyan/kibooz-backend/commit/76b79cd020924c781965828c59efea2de662d97e))
* **ci:** read the request counts of the active scan from the JSON access log ([9583097](https://github.com/SyafaHadyan/kibooz-backend/commit/95830970bf11f7ffec2b595d051bf53990879118))
* **ci:** read the request counts of the active scan from the JSON access log ([#262](https://github.com/SyafaHadyan/kibooz-backend/issues/262)) ([064db85](https://github.com/SyafaHadyan/kibooz-backend/commit/064db85c5cf5b269ef49a6e61c282c62f0294bf6))
* **ci:** turn the chat media checks of the smoke test on by an input and not by the public bucket ([4cf42d9](https://github.com/SyafaHadyan/kibooz-backend/commit/4cf42d9fb7123a8bbb620ec3ff2cde85de768bf8))
* **classes:** check the join code again under the class lock and replace it under the same lock ([bb4b834](https://github.com/SyafaHadyan/kibooz-backend/commit/bb4b834a0188ccc57933f90f92c5a0d216c6202b))
* **classes:** check the join code again under the class lock and replace it under the same lock ([#269](https://github.com/SyafaHadyan/kibooz-backend/issues/269)) ([36155fa](https://github.com/SyafaHadyan/kibooz-backend/commit/36155fa6c8d8b820a6b1088cc069deefba9adad6))
* **deps:** move to Go 1.27.3 for the crypto/x509 advisory GO-2026-6809 ([b4d3aeb](https://github.com/SyafaHadyan/kibooz-backend/commit/b4d3aeb7c6ccc52b9d567244e159182b3f15a637))
* **deps:** move to Go 1.27.3 for the crypto/x509 advisory GO-2026-6809 ([#272](https://github.com/SyafaHadyan/kibooz-backend/issues/272)) ([fec63fe](https://github.com/SyafaHadyan/kibooz-backend/commit/fec63fe31c2597829b798b699fc0df3c8dec86e9))
* **guru:** check the teacher who adds another teacher again once the class lock is held ([007a501](https://github.com/SyafaHadyan/kibooz-backend/commit/007a5018de8f548061d8e1cf69f62b24882b5b9e))
* **guru:** check the teacher who adds another teacher again once the class lock is held ([#268](https://github.com/SyafaHadyan/kibooz-backend/issues/268)) ([9ad2486](https://github.com/SyafaHadyan/kibooz-backend/commit/9ad248649ca26a1adda527722ef9ec8ac770efe0))
* **push:** check the length of a push token after the trim ([8b4f54e](https://github.com/SyafaHadyan/kibooz-backend/commit/8b4f54e73842a95b508d1ee7aaafef9a0d352c00))
* **push:** delete a token only when Firebase says it is unregistered ([acf5562](https://github.com/SyafaHadyan/kibooz-backend/commit/acf5562508ab0c557114c275ef52416e1db5f616))
* **push:** leave the token length to the use case so a padded token is judged after the trim ([a7d3178](https://github.com/SyafaHadyan/kibooz-backend/commit/a7d31784a9a46634cc92a5a9961efe4d0c50a369))
* **push:** refuse a token for a deleted account by locking the user row first ([f18194e](https://github.com/SyafaHadyan/kibooz-backend/commit/f18194ed9541ada064a7e17cb7997ba880f756fd))
* **season:** archive and reset only the classes that the rollover locked ([2f65938](https://github.com/SyafaHadyan/kibooz-backend/commit/2f659384ca272c92f4e5c91b3670b72024062b5b))
* **season:** archive and reset only the classes that the rollover locked ([#270](https://github.com/SyafaHadyan/kibooz-backend/issues/270)) ([40adbdd](https://github.com/SyafaHadyan/kibooz-backend/commit/40adbdd0f2c1ef511068df67a81c9abc15ba2757))
* **user:** bind the account deletion to the password hash that was verified ([fb34230](https://github.com/SyafaHadyan/kibooz-backend/commit/fb34230cfb044a513af987967e8639a73ae8af6d))
* **user:** bind the account deletion to the password hash that was verified ([#267](https://github.com/SyafaHadyan/kibooz-backend/issues/267)) ([a03d064](https://github.com/SyafaHadyan/kibooz-backend/commit/a03d0644b34944f2afe0daee0011f28ab7ae4eaf))

## [0.15.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.14.1...v0.15.0) (2026-10-20)


### Features

* **chat:** add direct conversations between a teacher and a parent ([#252](https://github.com/SyafaHadyan/kibooz-backend/issues/252)) ([6388648](https://github.com/SyafaHadyan/kibooz-backend/commit/6388648b9adc15b11cd9153ca3f1a5e7fc6964e3))
* **chat:** add the contacts, conversations and messages routes for a teacher and a parent ([0473585](https://github.com/SyafaHadyan/kibooz-backend/commit/04735852879a782bba0aeec3f243f9c94305f927))
* **chat:** add the tables of the direct conversations ([3688cb1](https://github.com/SyafaHadyan/kibooz-backend/commit/3688cb148ef6dab4569808cfbc0abbe016ad0368))
* **errorreport:** report server failures and panics to Sentry ([#255](https://github.com/SyafaHadyan/kibooz-backend/issues/255)) ([d1b9896](https://github.com/SyafaHadyan/kibooz-backend/commit/d1b9896b693d892ad02b0609856d2fa1d03e9295))
* **errorreport:** report server failures and panics to Sentry, scrubbed and capped ([b820768](https://github.com/SyafaHadyan/kibooz-backend/commit/b820768e6e40b824879cd225d987a5223ba1a460))


### Bug Fixes

* **errorreport:** attach a stack to the report of a panic with a string ([5d68661](https://github.com/SyafaHadyan/kibooz-backend/commit/5d6866147407d858ecdee2edf6f364449c517efa))
* **errorreport:** use the data collection options and build the token fixture at run time ([690ef37](https://github.com/SyafaHadyan/kibooz-backend/commit/690ef374e112829e38ce1db1f3f6a5911ceeaae2))

## [0.14.1](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.14.0...v0.14.1) (2026-10-20)


### Bug Fixes

* **monitoring:** leave the health check out of the p95 latency alert and panels ([2648f1e](https://github.com/SyafaHadyan/kibooz-backend/commit/2648f1e6be77c2ea98c2b1791aed27b1ef67d9a9))
* **monitoring:** leave the health check out of the p95 latency alert and panels ([#249](https://github.com/SyafaHadyan/kibooz-backend/issues/249)) ([d5d7bdf](https://github.com/SyafaHadyan/kibooz-backend/commit/d5d7bdf66745d697fce87902175d753c96943d6b))

## [0.14.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.13.0...v0.14.0) (2026-10-20)


### Features

* **monitoring:** find the containers by label and name every series by host and copy ([976ae27](https://github.com/SyafaHadyan/kibooz-backend/commit/976ae27ce49dcf919eb1a4a6a2d15e211aa5227e))
* **monitoring:** ship metrics and logs to Grafana Cloud with Alloy ([#246](https://github.com/SyafaHadyan/kibooz-backend/issues/246)) ([a219fd1](https://github.com/SyafaHadyan/kibooz-backend/commit/a219fd1e0b2b5dcde13db40897cf67febbf02951))


### Bug Fixes

* **monitoring:** accept the metrics port of the label with or without a colon ([e30874f](https://github.com/SyafaHadyan/kibooz-backend/commit/e30874ffec22e665f7ae2f4044b322e826c66b78))
* **monitoring:** discover every network of the API so the internal one can be picked ([0edd64c](https://github.com/SyafaHadyan/kibooz-backend/commit/0edd64c5d453b55a44f1e5f4b47f9a08bf6dcb09))
* **monitoring:** keep the job label of the job metrics when Alloy scrapes them ([59a1278](https://github.com/SyafaHadyan/kibooz-backend/commit/59a1278a07021b0091ff72401f78abaf732b06c3))
* **monitoring:** make Alloy scrape the API and keep its labels ([#248](https://github.com/SyafaHadyan/kibooz-backend/issues/248)) ([965c0a7](https://github.com/SyafaHadyan/kibooz-backend/commit/965c0a785deb1fb9ed6a28f73e1645b1550fa37b))
* **monitoring:** name a vanished API copy in the alert and word the no-data summary ([3f9f726](https://github.com/SyafaHadyan/kibooz-backend/commit/3f9f726a4510e2666fa6e4e3f9acdca947803719))
* **monitoring:** scrape the API only through the internal network of the compose file ([b5731c6](https://github.com/SyafaHadyan/kibooz-backend/commit/b5731c6acc2aa1e3d95c3beab4293248e748c760))

## [0.13.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.12.1...v0.13.0) (2026-10-19)


### Features

* **api:** refuse a JSON body over 64 KB except on the photo uploads ([d3430b7](https://github.com/SyafaHadyan/kibooz-backend/commit/d3430b7daf744877c9fb4b49d2d57db7ff94170f))
* **api:** refuse a JSON body over 64 KB except on the photo uploads ([#232](https://github.com/SyafaHadyan/kibooz-backend/issues/232)) ([90a0190](https://github.com/SyafaHadyan/kibooz-backend/commit/90a0190a8905664cb39f025ad6afca2201fe0748))
* **classes:** let a teacher close a class for new children without replacing the join code ([0bf5bd1](https://github.com/SyafaHadyan/kibooz-backend/commit/0bf5bd1ef22d53ca5f1b48527d9845c7aa748acc))
* **classes:** let a teacher close a class for new children without replacing the join code ([#238](https://github.com/SyafaHadyan/kibooz-backend/issues/238)) ([19196cc](https://github.com/SyafaHadyan/kibooz-backend/commit/19196cc37e296ec387ad6a5fb328dfad6e116b78))
* **classes:** limit a class to 60 children and a teacher to 20 classes ([6c03977](https://github.com/SyafaHadyan/kibooz-backend/commit/6c03977f26c847d3c3579128b9e100616b051467))
* **classes:** limit a class to 60 children and a teacher to 20 classes ([#233](https://github.com/SyafaHadyan/kibooz-backend/issues/233)) ([4e29986](https://github.com/SyafaHadyan/kibooz-backend/commit/4e29986bd90531d9411bda8c58ec4c7239f5e8af))
* **guru:** let a teacher remove a child from a class ([1390dd7](https://github.com/SyafaHadyan/kibooz-backend/commit/1390dd710a5f65590d9887bde89583e997a9316a))
* **guru:** let a teacher remove a child from a class ([#239](https://github.com/SyafaHadyan/kibooz-backend/issues/239)) ([8088f50](https://github.com/SyafaHadyan/kibooz-backend/commit/8088f50f214d8adb0ec773b43ced58be6c54edc0))
* **guru:** limit mood logs to 20 per child per day and notes to 500 characters ([de8b766](https://github.com/SyafaHadyan/kibooz-backend/commit/de8b76688c0ebd8777665338ba8a425c90da70d4))
* **guru:** limit mood logs to 20 per child per day and notes to 500 characters ([#230](https://github.com/SyafaHadyan/kibooz-backend/issues/230)) ([eaa44f7](https://github.com/SyafaHadyan/kibooz-backend/commit/eaa44f7cd4c68cdfd18a8af92bd28bc2e8f432be))
* **guru:** limit the batch mood route to 30 requests per minute per user ([a07cc83](https://github.com/SyafaHadyan/kibooz-backend/commit/a07cc83952c82282ecbb87733d5d5aa0d8554f4d))
* **guru:** limit the batch mood route to 30 requests per minute per user ([#235](https://github.com/SyafaHadyan/kibooz-backend/issues/235)) ([2459e71](https://github.com/SyafaHadyan/kibooz-backend/commit/2459e71afe5f063b3af92f84fa9b80cc065c77a7))
* **guru:** replace the join code when a teacher is removed or leaves a class ([3a459a8](https://github.com/SyafaHadyan/kibooz-backend/commit/3a459a8894938ed7cbe32c179cb5f9c3b4cc48e0))
* **guru:** replace the join code when a teacher is removed or leaves a class ([#237](https://github.com/SyafaHadyan/kibooz-backend/issues/237)) ([0568d93](https://github.com/SyafaHadyan/kibooz-backend/commit/0568d93fe224bfc4875560ea6545d74ce19c0c1f))
* **guru:** show which teacher is the signed in one and the account id in the teacher list ([03f61fd](https://github.com/SyafaHadyan/kibooz-backend/commit/03f61fd8150732b4f0b1055b68b28ab980572d28))
* **guru:** show which teacher is the signed in one and the account id in the teacher list ([#229](https://github.com/SyafaHadyan/kibooz-backend/issues/229)) ([5a31b91](https://github.com/SyafaHadyan/kibooz-backend/commit/5a31b911e6b8702ed95a1b53d19ea165860f5c82))
* **logging:** write JSON logs with the reason of each failure and the stack of each panic ([cc22eb9](https://github.com/SyafaHadyan/kibooz-backend/commit/cc22eb91b996a1d28f68a734c4b99fd46d5b639d))
* **logging:** write JSON logs with the reason of each failure and the stack of each panic ([#245](https://github.com/SyafaHadyan/kibooz-backend/issues/245)) ([2f74859](https://github.com/SyafaHadyan/kibooz-backend/commit/2f74859d74af52e6cca1cc427640a43128e01253))
* **metrics:** serve Prometheus metrics on a private listener ([a1c71cb](https://github.com/SyafaHadyan/kibooz-backend/commit/a1c71cb257f0ce2968468510404d52fca111ada2))
* **metrics:** serve Prometheus metrics on a private listener ([#244](https://github.com/SyafaHadyan/kibooz-backend/issues/244)) ([8b69e37](https://github.com/SyafaHadyan/kibooz-backend/commit/8b69e37b8271ed391de1c4ba170816fd32772cd2))
* **profile:** let a teacher change their name and a parent change their profile ([89145ca](https://github.com/SyafaHadyan/kibooz-backend/commit/89145cac0dce0ed72de51fd4a78a6fc9d0515a88))
* **profile:** let a teacher change their name and a parent change their profile ([#240](https://github.com/SyafaHadyan/kibooz-backend/issues/240)) ([4fee375](https://github.com/SyafaHadyan/kibooz-backend/commit/4fee375062ad91aa89ed26b5f3f264ddfac3d480))
* **wali:** let a parent change the name or the NISN of a child ([4d52be3](https://github.com/SyafaHadyan/kibooz-backend/commit/4d52be39a0f7fa77b8f8d7b04d42cc3f01f83752))
* **wali:** let a parent change the name or the NISN of a child ([#241](https://github.com/SyafaHadyan/kibooz-backend/issues/241)) ([9617a75](https://github.com/SyafaHadyan/kibooz-backend/commit/9617a75699961136fe99297b1680c59266db9917))
* **wali:** let a parent delete a child ([63027d3](https://github.com/SyafaHadyan/kibooz-backend/commit/63027d3b817a91682fbf59ee64d433e87c464fde))
* **wali:** let a parent delete a child ([#242](https://github.com/SyafaHadyan/kibooz-backend/issues/242)) ([e316381](https://github.com/SyafaHadyan/kibooz-backend/commit/e316381d608e25807f4807647a8ff44b39b62245))


### Bug Fixes

* **api:** match the upload paths like the router and name a huge body by its route ([34eba4c](https://github.com/SyafaHadyan/kibooz-backend/commit/34eba4c65b126d272d77a8fb6ec45f8de34df363))
* **classes:** answer the class limit instead of a server error when creating a class ([07e963e](https://github.com/SyafaHadyan/kibooz-backend/commit/07e963eec801a04827f11f89fe68f9565a42200e))
* **compose:** cap the memory of the API and Redis and give the API and database time to stop ([1b87daa](https://github.com/SyafaHadyan/kibooz-backend/commit/1b87daa955da424707ddb032d4af7280bafd1a36))
* **compose:** cap the memory of the API and Redis and give the API and database time to stop ([#231](https://github.com/SyafaHadyan/kibooz-backend/issues/231)) ([042cded](https://github.com/SyafaHadyan/kibooz-backend/commit/042cded79554e61c4b563a0379c1cb3bd0b0afba))
* **compose:** set the memory caps under deploy.resources like the CI compose file ([4b6da30](https://github.com/SyafaHadyan/kibooz-backend/commit/4b6da30e624b93522fcd94667c034cac6193756d))
* **guru:** compute the local day once in the mood record query ([5869a2d](https://github.com/SyafaHadyan/kibooz-backend/commit/5869a2de26a4f49aac4913e3d9cf3ae71c84dfc4))
* **guru:** refuse a class when the teacher row is gone by the time it is locked ([159217c](https://github.com/SyafaHadyan/kibooz-backend/commit/159217c69c5d498b36ee4b0b065b48ab8350843d))
* **logging:** close the health connection when the metrics listener cannot start ([959540c](https://github.com/SyafaHadyan/kibooz-backend/commit/959540cc5a46bcef91109d345176eb1c5138e53a))
* **logging:** ping the health connection at start-up and use --no-log-prefix with jq ([3abb133](https://github.com/SyafaHadyan/kibooz-backend/commit/3abb133cef01fc54c2a8a00cf0ea88461930bbbc))
* **metrics:** close the connections when the listener cannot start and name the requests the metric misses ([780ca44](https://github.com/SyafaHadyan/kibooz-backend/commit/780ca442bdc822925b255261b144ebfa96b68e90))
* **metrics:** count the requests that the body checks refuse ([322568f](https://github.com/SyafaHadyan/kibooz-backend/commit/322568f2016bedb9e2e3440e89102a0ca18cbad0))
* **metrics:** label a refusal by a group check with the path of the group ([366b69c](https://github.com/SyafaHadyan/kibooz-backend/commit/366b69c3bf21eb648f3fd724b32df2de3d4cc509))
* **wali:** list NO_CHILD_REGISTERED in the NotFound response and clean up the queue in the e2e test ([7e16d98](https://github.com/SyafaHadyan/kibooz-backend/commit/7e16d9825c47646a55e606ac207d7842e94a561e))


### Performance Improvements

* **guru:** read only the newest mood log of each child per day ([ca94cab](https://github.com/SyafaHadyan/kibooz-backend/commit/ca94cab039d3531e399195b0220c172f969c4912))
* **guru:** read only the newest mood log of each child per day ([#234](https://github.com/SyafaHadyan/kibooz-backend/issues/234)) ([fd1521c](https://github.com/SyafaHadyan/kibooz-backend/commit/fd1521cf2cf2e820b16c5bd4aa8c14115732d8db))

## [0.12.1](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.12.0...v0.12.1) (2026-10-19)


### Bug Fixes

* **auth:** take the used flag off a refresh token on a context of its own ([988f5f7](https://github.com/SyafaHadyan/kibooz-backend/commit/988f5f7527b6299213f6eafd7f11b11ecaf7950c))
* **auth:** take the used flag off a refresh token on a context of its own ([#211](https://github.com/SyafaHadyan/kibooz-backend/issues/211)) ([5b1ea19](https://github.com/SyafaHadyan/kibooz-backend/commit/5b1ea194fa77398d6a134e9e950d5ab0bec50a1e))
* **ci:** drop a test file that does not belong in this change ([7718c5f](https://github.com/SyafaHadyan/kibooz-backend/commit/7718c5f9af1238982fe8713311218ea9e36c8008))
* **ci:** keep Trivy from asking for a USER line in the tools Dockerfile ([2678edd](https://github.com/SyafaHadyan/kibooz-backend/commit/2678edd75d8336a7c9f5f10c4e7c340295d15225))
* **deps:** let Renovate run go mod tidy after a Go update ([9679ad2](https://github.com/SyafaHadyan/kibooz-backend/commit/9679ad21b80ed94e05629bc8833911127bd5d5dc))
* **deps:** let Renovate run go mod tidy after a Go update ([#218](https://github.com/SyafaHadyan/kibooz-backend/issues/218)) ([ee91733](https://github.com/SyafaHadyan/kibooz-backend/commit/ee9173329df21021b735607ab1cca2f032096fe6))
* **user:** lock the parent row before the class locks when deleting a parent ([2f3c4ae](https://github.com/SyafaHadyan/kibooz-backend/commit/2f3c4aecf4c8abcc2e71429addd2efb12ef20db6))
* **wali:** lock the parent row in one order for adding a child and deleting a parent ([#210](https://github.com/SyafaHadyan/kibooz-backend/issues/210)) ([9de4cc2](https://github.com/SyafaHadyan/kibooz-backend/commit/9de4cc2cdbd6aa7f5a45fec814afd211bf6f31cb))
* **wali:** lock the parent row through the model so the lock reports whether it found one ([af69e55](https://github.com/SyafaHadyan/kibooz-backend/commit/af69e55d85a076fbe878a221a123e1ba042317b4))
* **wali:** refuse a child for a parent who was deleted while the request waited ([8fdd40a](https://github.com/SyafaHadyan/kibooz-backend/commit/8fdd40a8e6cea8a4c90aaac7d4558d300b441f61))

## [0.12.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.11.0...v0.12.0) (2026-10-19)


### Features

* **auth:** cap how many passwords are hashed at once ([#200](https://github.com/SyafaHadyan/kibooz-backend/issues/200)) ([0e87700](https://github.com/SyafaHadyan/kibooz-backend/commit/0e877009673327c4984f65ca999fc8d5b9c27d21))


### Bug Fixes

* **auth:** leave one CPU free when sizing the hashing pool ([b7ba85c](https://github.com/SyafaHadyan/kibooz-backend/commit/b7ba85c8f9f17a881ddba12e7d9ba5e37b46b1d2))
* **auth:** lock the user row first when changing a password or deleting an account ([#198](https://github.com/SyafaHadyan/kibooz-backend/issues/198)) ([3511f3c](https://github.com/SyafaHadyan/kibooz-backend/commit/3511f3c2af2dec9efaa1bd96d905e2b4d2961341))
* **auth:** refuse a cancelled request before it takes a hashing slot ([3a44518](https://github.com/SyafaHadyan/kibooz-backend/commit/3a4451844122afa22d564c81f7be0494500d3b55))
* **auth:** store a sign in session only for the password hash it checked ([#195](https://github.com/SyafaHadyan/kibooz-backend/issues/195)) ([2958b39](https://github.com/SyafaHadyan/kibooz-backend/commit/2958b3931e52c95a7f7d0b244f0a2350a95d6390))
* **limiter:** count a request whose identifier is repeated with null ([#190](https://github.com/SyafaHadyan/kibooz-backend/issues/190)) ([7f08386](https://github.com/SyafaHadyan/kibooz-backend/commit/7f0838612d01d4ec1505a9b2dc6aaa64c52b1763))
* **redis:** keep the first deadline of a used refresh marker ([#191](https://github.com/SyafaHadyan/kibooz-backend/issues/191)) ([87eceb8](https://github.com/SyafaHadyan/kibooz-backend/commit/87eceb8117a9b0591efd7f5895deb536d04face2))
* **server:** read every Content-Encoding line and keep the headers on the refusal ([befe4fc](https://github.com/SyafaHadyan/kibooz-backend/commit/befe4fcac4a0eb1b77ccdcf86b2b671a19093455))
* **server:** refuse a request whose body has a content encoding ([#192](https://github.com/SyafaHadyan/kibooz-backend/issues/192)) ([2ce9723](https://github.com/SyafaHadyan/kibooz-backend/commit/2ce9723121a58684175d8e32fbb953c57997b95f))
* **sessions:** delete tokens until a pass finds none ([#194](https://github.com/SyafaHadyan/kibooz-backend/issues/194)) ([475cdb7](https://github.com/SyafaHadyan/kibooz-backend/commit/475cdb7f64ff5e90ab3a2c932c83283f2a5a9833))
* **smoke:** skip the keeper deletion check after a failed check ([#196](https://github.com/SyafaHadyan/kibooz-backend/issues/196)) ([a42809f](https://github.com/SyafaHadyan/kibooz-backend/commit/a42809f0524e50413a31a262ff8747a8f2d4637d))

## [0.11.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.10.0...v0.11.0) (2026-10-18)


### Features

* **classes:** archive a class without children when its last teacher leaves ([f91d0f7](https://github.com/SyafaHadyan/kibooz-backend/commit/f91d0f771274695eaa994d8991bcf6778b88fd4f))
* **classes:** archive a class without children when its last teacher leaves ([#176](https://github.com/SyafaHadyan/kibooz-backend/issues/176)) ([8330a19](https://github.com/SyafaHadyan/kibooz-backend/commit/8330a199b550b8dcbcacccef05efffe421644862))
* **guru:** add a batch mood log for a camera scan of many children ([5e20f4a](https://github.com/SyafaHadyan/kibooz-backend/commit/5e20f4a528c89831b918a5c7eadfd45eaa71d377))
* **guru:** add a batch mood log for a camera scan of many children ([#169](https://github.com/SyafaHadyan/kibooz-backend/issues/169)) ([43feec2](https://github.com/SyafaHadyan/kibooz-backend/commit/43feec21c0b4374068be0e6094c877dcd4d78d5d))
* **idempotency:** remember the answer of a request that carries an Idempotency-Key ([a326eab](https://github.com/SyafaHadyan/kibooz-backend/commit/a326eab129621c620dec1ac407da19d07c1a3e67))
* **idempotency:** remember the answer of a request that carries an Idempotency-Key ([#173](https://github.com/SyafaHadyan/kibooz-backend/issues/173)) ([963f950](https://github.com/SyafaHadyan/kibooz-backend/commit/963f950cd8a4ba0332cc627e167011ffc6986c54))
* **mood:** accept the time a mood was seen so a late replay keeps its place ([c1f67b4](https://github.com/SyafaHadyan/kibooz-backend/commit/c1f67b4f74e7137b13f115a1d79beb696575f99b))
* **mood:** accept the time a mood was seen so a late replay keeps its place ([#175](https://github.com/SyafaHadyan/kibooz-backend/issues/175)) ([1864261](https://github.com/SyafaHadyan/kibooz-backend/commit/1864261fc7aae268a831bfddb55f627810e1ea4b))
* **users:** change the password with the current one and end every session ([ca04919](https://github.com/SyafaHadyan/kibooz-backend/commit/ca04919e749160f5fd8a7188e930d9cd917fd5c9))
* **users:** change the password with the current one and end every session ([#174](https://github.com/SyafaHadyan/kibooz-backend/issues/174)) ([ea9605d](https://github.com/SyafaHadyan/kibooz-backend/commit/ea9605d8eeb202cd5a87a6982095a3ece8e8bc37))


### Bug Fixes

* **classes:** lock the class and refuse an archived one when a teacher is added ([3886343](https://github.com/SyafaHadyan/kibooz-backend/commit/38863432a2a0b101f7163aac1c94491429d5ac61))
* **idempotency:** fence each claim so a late try cannot touch its replacement ([89003e1](https://github.com/SyafaHadyan/kibooz-backend/commit/89003e1f420ea1028399d49bb2d6a8aba2fbd73b))
* **smoke:** bound the cleanup calls and report accounts that stay behind ([1c30317](https://github.com/SyafaHadyan/kibooz-backend/commit/1c303176fa5c3094efc52edf74821ec7e09754ce))
* **smoke:** clean up and report on every exit, including a crash ([4dc2780](https://github.com/SyafaHadyan/kibooz-backend/commit/4dc27800b3c360c0decca10ba4ccd03dd13d2224))
* **users:** end the sessions before the password update, like an account deletion ([1e3d5ec](https://github.com/SyafaHadyan/kibooz-backend/commit/1e3d5ecbebb5d32b9d8fdd4d60eaaa2a13437692))

## [0.10.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.9.1...v0.10.0) (2026-10-18)


### Features

* **classroom:** delete a video file that was uploaded and never added ([#161](https://github.com/SyafaHadyan/kibooz-backend/issues/161)) ([349c5ab](https://github.com/SyafaHadyan/kibooz-backend/commit/349c5ab8c7ec1d9c45865572fc558ac72889fda7))

## [0.9.1](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.9.0...v0.9.1) (2026-10-18)


### Bug Fixes

* **k6:** preallocate more VUs so a runner stall does not drop iterations ([#157](https://github.com/SyafaHadyan/kibooz-backend/issues/157)) ([3bab0b5](https://github.com/SyafaHadyan/kibooz-backend/commit/3bab0b50bd49f62c3ed1ee8477cfc05cadf76209))

## [0.9.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.8.0...v0.9.0) (2026-10-15)


### Features

* **account:** require a successor when the only teacher of a class deletes the account ([f80f06a](https://github.com/SyafaHadyan/kibooz-backend/commit/f80f06aba9811e4afd333d4f39897b56c273e00a))
* **classes:** let the owner remove teachers and hand a class over ([27a55fd](https://github.com/SyafaHadyan/kibooz-backend/commit/27a55fdc7e709c5cd6bdde7c8942708b34c00271))
* **leaderboard:** list the finished semesters of a class ([43cc052](https://github.com/SyafaHadyan/kibooz-backend/commit/43cc052e1a0ff9b3ea84b3270ebe1f598eaa1179))


### Bug Fixes

* **classes:** lock every class of a deleted teacher before handing it over ([2f0a68a](https://github.com/SyafaHadyan/kibooz-backend/commit/2f0a68a444d17042ed8b972affe4d31c63fafb88))
* **leaderboard:** keep a semester listed when its children were deleted ([b8c1089](https://github.com/SyafaHadyan/kibooz-backend/commit/b8c1089e04fad6d2ac649b6a11c90c1f82fec805))

## [0.8.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.7.0...v0.8.0) (2026-10-14)


### Features

* **classroom:** let teachers read the guides that parents applied ([e6873c4](https://github.com/SyafaHadyan/kibooz-backend/commit/e6873c44877c4ad96190e43ff9df6e01cd9509f9))
* **guru:** let a teacher of a class add another teacher ([7246115](https://github.com/SyafaHadyan/kibooz-backend/commit/7246115729d36b6d6b21b55a45d05154f33feaee))
* **leaderboard:** start the ranking over at the beginning of every semester ([15b8447](https://github.com/SyafaHadyan/kibooz-backend/commit/15b84479df3972319b1f92af116bdc0705668c4c))
* **user:** retry removing the bucket files of a deleted account ([79d35a8](https://github.com/SyafaHadyan/kibooz-backend/commit/79d35a81f7db85f9cb1130f918e2bb401ee88924))
* **wali:** let a parent list and add more children ([d210256](https://github.com/SyafaHadyan/kibooz-backend/commit/d2102565031f0d231f223df82220cbcd34d4ba4a))


### Bug Fixes

* **health:** do not cache a storage probe that the caller cancelled ([8e05447](https://github.com/SyafaHadyan/kibooz-backend/commit/8e05447c625d1e0b2395a45d1a73fa5b043c429e))
* **leaderboard:** check the semester every minute to shrink the gap ([381950b](https://github.com/SyafaHadyan/kibooz-backend/commit/381950b69af151b2e3d36fa5f351c5c45d925b81))
* **user:** back off failed cleanups, bound the response wait and lock the rollback ([c4f320c](https://github.com/SyafaHadyan/kibooz-backend/commit/c4f320c93cbcafc0d4943a4016ad3b11e74285bd))
* **user:** queue deleted files in the deletion transaction and sweep fairly ([54078f8](https://github.com/SyafaHadyan/kibooz-backend/commit/54078f8342bec6774237e2928246dcb934ea50d1))
* **user:** return the context error when a cleanup sweep is cut short ([cd2f76c](https://github.com/SyafaHadyan/kibooz-backend/commit/cd2f76ca457c8703187ff9ba13544e4bcf36eb4d))

## [0.7.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.6.0...v0.7.0) (2026-10-14)


### Features

* **classcode:** issue join codes of eight characters ([4f57ab1](https://github.com/SyafaHadyan/kibooz-backend/commit/4f57ab161aef01eecad6e566538483f697813911))
* **db:** give the classes with a short join code a new eight character one ([abef6ea](https://github.com/SyafaHadyan/kibooz-backend/commit/abef6ea65dffc476ac8aaa25e296c9275baa7cae))


### Bug Fixes

* **auth:** end a session without missing the token that a refresh stores at the same moment ([5be951e](https://github.com/SyafaHadyan/kibooz-backend/commit/5be951e1a1cf2ac53ffd855dc31597f9338fc22c))
* **auth:** limit registrations that name a class code for the whole API ([8161ac7](https://github.com/SyafaHadyan/kibooz-backend/commit/8161ac752bca59689a23723f0f3e54ef70774113))
* **auth:** lock every unused token when a session ends, whatever the clocks say ([700bcd3](https://github.com/SyafaHadyan/kibooz-backend/commit/700bcd382281a13eb9e6a14c9355f582319105fa))
* **auth:** look the class up before hashing the password of a parent ([eb1d2ca](https://github.com/SyafaHadyan/kibooz-backend/commit/eb1d2ca097f24cd6b66ba09b9339b1bb20d87b0b))
* **auth:** never let the token cleanup of a refresh wait for a locked row ([c0ec482](https://github.com/SyafaHadyan/kibooz-backend/commit/c0ec4826f38bd181b17f67d43ec2d644c63ae5de))
* **compose:** publish the API on localhost only and let the image tag be chosen ([faab601](https://github.com/SyafaHadyan/kibooz-backend/commit/faab601c20594af62ab0a062bb4af04a3f2dfa21))
* **limiter:** tell a refused client when a request fits again and not when the window ends ([ca72706](https://github.com/SyafaHadyan/kibooz-backend/commit/ca72706b141d2c4467040e5719de8a433ab8f1ac))
* **redis:** count a client timeout as an outage while the caller still waits ([ac33445](https://github.com/SyafaHadyan/kibooz-backend/commit/ac334453565f301da918217b3dc2540b2b10da2f))
* **redis:** test a Redis that was down with one call and keep command errors from opening the breaker ([f048099](https://github.com/SyafaHadyan/kibooz-backend/commit/f04809951bf7b40d33bba31d7001a4f2bd5d41a5))
* **upload:** hold every frame of an animated WebP to the pixel limits ([5a69cfa](https://github.com/SyafaHadyan/kibooz-backend/commit/5a69cfa5bd6532f2381a908d501cb5b3d9d26aac))
* **upload:** keep only the parts of an image that the picture needs ([f56920d](https://github.com/SyafaHadyan/kibooz-backend/commit/f56920d26b22a7b9686e044a112ee1e99def3882))
* **upload:** refuse a PNG that has a critical chunk the server does not know ([b03ecd8](https://github.com/SyafaHadyan/kibooz-backend/commit/b03ecd89e7e257c76b41abc7e3ea7769c60514fd))
* **upload:** refuse an image that announces more pixels than the apps can safely show ([a5eb13f](https://github.com/SyafaHadyan/kibooz-backend/commit/a5eb13fb71a54cdd41b1e00a887094ef1eeec778))
* **validation:** refuse a NUL character in text instead of failing with a 500 ([8bc0394](https://github.com/SyafaHadyan/kibooz-backend/commit/8bc0394e4c27a2104582e8b847b2d5aba1f058db))

## [0.6.0](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.5.2...v0.6.0) (2026-10-10)


### Features

* **auth:** end the session when a used refresh token is shown again and cap how long a session can last ([b86e45b](https://github.com/SyafaHadyan/kibooz-backend/commit/b86e45b4d2b3b7dc90f2178c1193b9923386adb0))
* **guru:** let a teacher replace the join code of a class ([e4ae61d](https://github.com/SyafaHadyan/kibooz-backend/commit/e4ae61d89ac5e6d171e5a38a5d5cb749a2c34746))
* **limiter:** count requests with atomic Redis commands and add a key prefix for a shared Redis ([b797dae](https://github.com/SyafaHadyan/kibooz-backend/commit/b797daed90e45902898e5998a20fa3ecf54ad7f2))


### Bug Fixes

* **auth:** keep a used refresh token for the whole retention and state the grace boundary exactly ([fa15e9a](https://github.com/SyafaHadyan/kibooz-backend/commit/fa15e9ae322c8ed99a2e346892bd9497e74a23ad))
* **limiter:** round the share of the previous window up so a window boundary cannot let an extra request through ([850a3db](https://github.com/SyafaHadyan/kibooz-backend/commit/850a3db99a81ffd8c8cddd98199c790198cc344e))
* **limiter:** sweep the in-memory counters at most once per window ([8476e60](https://github.com/SyafaHadyan/kibooz-backend/commit/8476e60d42cfd984a8931829a9e01d6ebf6a88d4))
* **upload:** keep only the JFIF header of a JPEG and refuse a WebP with a wrong extended header ([9655de4](https://github.com/SyafaHadyan/kibooz-backend/commit/9655de4e335dbcc9b88a9d0015450a08b21c7058))
* **upload:** size the orientation block with constants and explain the one size conversion ([ec82ad7](https://github.com/SyafaHadyan/kibooz-backend/commit/ec82ad7b1d014e39e2a3459f2741ffc79ab52f4f))
* **upload:** strip the EXIF, XMP and text metadata from avatars and trash photos and keep only the orientation ([2737995](https://github.com/SyafaHadyan/kibooz-backend/commit/2737995de36587a23844d92bd6c14db5f4aa6ab1))
* **user:** delete the avatars and trash photos of an account when the account is deleted ([090d516](https://github.com/SyafaHadyan/kibooz-backend/commit/090d516c54d1e441e5b3d107be5afc3782c63ad6))
* **user:** reuse the existing error checks when collecting the files of a deleted account ([d76d2db](https://github.com/SyafaHadyan/kibooz-backend/commit/d76d2dbe364447fa70ed30d760cad4a4ada244e6))

## [0.5.2](https://github.com/SyafaHadyan/kibooz-backend/compare/v0.5.1...v0.5.2) (2026-10-10)


### Bug Fixes

* **ci:** stop the tag run from moving latest so only main publishes it ([8e508b9](https://github.com/SyafaHadyan/kibooz-backend/commit/8e508b93104d637f3e7ec97ca9d6e79005ff0174))

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
