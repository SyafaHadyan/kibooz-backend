# Questions and glossary

## Words from the app

The app and the PRD are in Indonesian, and the API keeps a few of those words in its routes, fields and values.

| Word | Meaning |
|:---|:---|
| `guru` | Teacher. The role `GURU` and the routes under `/guru` |
| `wali` | Parent or guardian of a child. The role `WALI` and the routes under `/wali` |
| `TK` | Taman Kanak-kanak, a kindergarten. The default school name is `TK Pertiwi Harapan Bangsa` |
| NIP | Nomor Induk Pegawai, the employee number of a teacher |
| NISN | Nomor Induk Siswa Nasional, the national student number of a child |
| `SENANG`, `SEDIH`, `MARAH`, `BINGUNG` | The four moods. Happy, sad, angry and confused |
| `ORGANIK`, `ANORGANIK`, `B3` | The trash categories. Organic, inorganic, and hazardous waste (Bahan Berbahaya dan Beracun), which earns no points |
| `semester` | A school term. The leaderboard starts again every semester, in January and July by default |
| Join code | The eight character code of a class that a parent enters to add a child to it |
| `WIB` | Western Indonesia Time, which is UTC+7 and the zone of the default `Asia/Jakarta` |

## Questions

**Why does my access token stop working after an hour?** An access token lives for `JWT_ACCESS_EXPIRED_MINUTES`, 60 by default. The app trades its refresh token for a new pair, and each refresh token works once. See the [client guide](client-guide.md).

**Why am I asked to sign in again after a few months even though I use the app every day?** A session ends `JWT_SESSION_MAX_DAYS` after the sign in, 90 by default, however often it is refreshed.

**Why does the app get a `429` on login when I did nothing wrong?** Login is limited per email, and anyone can use up the budget of an email they know. A device that signed in before sends its `deviceToken` and has a bucket of its own, so it keeps working. See [Trusted devices](design.md#trusted-devices).

**Why is a join code refused when it looks right?** The code is trimmed and read in capitals, so the case does not matter, but the characters `I`, `O`, `0` and `1` never appear in a code, so one of them is a typo. A class that was archived answers `CLASS_CODE_NOT_FOUND` too, because it has no teacher and no child any more.

**Why can a teacher delete the account without naming a successor, and another teacher cannot?** A class with children that nobody else teaches needs a successor, so it never loses its last teacher. A class with no children is archived instead. See [Error codes](errors.md#classes-and-teachers).

**Why does a trash claim give no points?** `B3` is worth 0 points by default, set by `POINTS_B3`. The other limit is `TRASH_DAILY_LIMIT` claims for each child each day, 5 by default, and it starts again on the next local day.

**Why did a repeated request not create another entry?** A request that carries the same `Idempotency-Key` as an earlier one that worked gets the first answer again, with the header `Idempotent-Replayed: true`. That is the point of the key, see [the design notes](design.md#idempotency-keys).

**Why does the leaderboard show different points than the profile?** The leaderboard ranks `seasonPoints`, the points of the running semester. The profile also shows `totalPoints`, the points of every semester.

**Can a deleted account be restored?** Not through the API. Deleting hides the rows and ends the sessions, and the email can be registered again as a new account. See [Privacy](privacy.md#what-deleting-an-account-does).

**Does the API send email?** No. There is no email verification and no password reset yet, which is why the password can only be changed with the current one.

**Where is the API documentation?** At <https://docs.kibooz.syafahadyan.com>, built from `openapi.yaml`.

**Who do I ask about something that is not here?** See [SUPPORT.md](../SUPPORT.md).
