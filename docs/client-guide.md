# Client guide

This page is for the people who build an app against the API. The full contract is the [API reference](https://docs.kibooz.syafahadyan.com), and this page only explains the parts that an app has to do on its own.

## Sign in and keep the tokens

Registration and login return an access token and a refresh token. Use the access token on every request and keep both tokens in secure storage, which is the Keychain on iOS and the Keystore on Android.

A refresh token works once. Every call to `POST /api/v1/auth/refresh-token` returns a new refresh token, so replace the stored one each time and never reuse the old one. Send a refresh request when the access token expires and not on every launch.

## Keep the device token

Registration and login also return a `deviceToken`. It is not a credential and the app never needs it to sign in. It only gives this device its own login budget for that email, so someone who types wrong passwords for the same email cannot keep the owner from signing in.

- Store it next to the account, one token for each email, because a token only works for the email it was issued for.
- Send it as `deviceToken` in the next login request for that email.
- Replace the stored token with the one in every login response, because each login renews it for another 90 days by default.
- Keep it after logout. A token that was kept lets the device sign in again even while a stranger is using up the shared budget of the email.
- Do not worry about a stale one. A token that expired or does not match the email is ignored and the login is counted in the shared bucket, so sending it never causes an error.

A token refresh does not return a device token, so leave the stored one as it is.

## Handle rate limits

A request over a limit gets `429` with the code `RATE_LIMITED` and a `Retry-After` header. Wait that long before the next try and show the person a short message. Do not retry in a loop, because every retry counts against the same limit. A `429` that has no JSON body comes from a proxy in front of the API and means the same thing.

The API never limits by network address, so a whole classroom on one network does not slow each other down. Routes for a signed-in user are limited per user. Registration and login are limited per email, or per device when the login carries a valid device token, and each of the two routes has its own budget. Refresh and logout are limited per refresh token, again with a budget for each route. A proxy or CDN in front of the API can add its own limit per address, and a very busy shared network can still get a `429` from that. The numbers and the reasons are in the [design notes](design.md#rate-limits).
