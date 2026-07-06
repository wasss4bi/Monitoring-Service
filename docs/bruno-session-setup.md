# Bruno: environment and user-provided sessions

Workspace: `docs/bruno/workspace.yml`. Select environment `monitoring_service` in collection `gO`.

## Request workflow

1. The local ignored environment retains the user's existing email/password and now includes the service JWT, valid monitor/contact IDs and example API/page URLs.
2. `Authentication/Login` stores a successful response's `access_token` as `accessToken`. It never prints the token or overwrites it on a failed login.
3. In `Monitors/Create monitor.yml`, all six bodies are commented. Uncomment exactly one complete JSON object below `// DNS`, `// OZON`, `// WB`, `// AVITO`, `// LAMODA`, or `// LEMANAPRO`; leave the label and all other blocks commented.
4. The pre-request script removes full-line comments. All six examples use automatic managed guest sessions, with no session variable to fill. Go receives one JSON object without comments.
5. The collection generates a fresh UUID for POST/PUT/PATCH/DELETE when Idempotency-Key is absent or blank. A manually specified header is preserved, including an explicit {{idempotencyKey}} variable reference. No environment key is required. For a retry after a lost response, copy the previous key from Timeline into Headers; an automatic new key represents a new operation.
6. Every monitor requires page_url and every rule requires description. browser_page_url independently selects native JSON capture; the six examples set it only for DNS/WB. The separate provided-session example uses monitorPageUrl alongside monitorApiUrl.
7. A successful creation updates `monitorId` for subsequent get/resume/stop requests.

The example thresholds are historical values, not re-fetched by the pre-request script. Remote site access is not guaranteed; see [request configurations and limitations](bruno-price-monitors.md).

## Two different kinds of credentials

- `accessToken` authenticates the user of THIS Go API. Obtain it with `Authentication/Login`.
- `dnsSession`, `ozonSession`, `wildberriesSession`, `avitoSession` are optional legacy export variables. The six marketplace bodies no longer use them. They are not Go API JWTs and must not contain encryption keys.
- `monitorSession` is the same v1 export for the separate generic `Create monitor with session` example; its default API URL is Ozon.
- All six marketplace examples use `session_mode=managed` and need no supplied session variable.

At the user's request all five session variables remain empty and marked `secret: true`. Only if deliberately using the separate provided-session example, enter the complete v1 JSON object in Bruno's Secrets tab, without enclosing the entire object in another pair of quotes. A Cookie header, one token value, or a raw Playwright cookie array is not the required export.
The local environment file is ignored by Git. Its existing development login credentials and service accessToken remain local environment values, not an OS vault. Do not publish that file or use this development storage arrangement for production account secrets.

## Where a marketplace session comes from

The marketplace creates cookies and browser state after the user opens its site and, if necessary, signs in or manually completes its verification. Only anonymous guest profiles are supported. Do not export a shopping account profile with payment/address/account access.

The internal v1 format is assembled from the real browser context's User-Agent, cookies and required same-origin storage. [Fetcher session.js](https://github.com/wasss4bi/values-from-body-fetcher/blob/main/services/parser/lib/session.js) provides `browserCookiesToJar`, `serializeSession` and `browserStorageState`; it is the current format source of truth. There is no user-facing v1 export endpoint or per-user onboarding wizard in this project yet.

`document.cookie` alone is insufficient because it omits HttpOnly cookies. Standard Playwright `storageState()` is useful input, but is not itself this service's v1 format; it must be converted and restricted to the intended hostname/origin. Do not export cookies or localStorage belonging to unrelated sites.

The noVNC tool now belongs to the Fetcher repository and uses a separate administrator/development diagnostic profile. It is not a secure multi-user session-creation interface. A local exporter or an authenticated per-user temporary Docker browser would be separate implementation choices, not existing API commands.

Sites may bind acceptance to IP/browser/time and may rotate cookies. An export valid on a laptop is not guaranteed to work from Docker. A provided rejected session is not silently replaced by an anonymous guest; replace it with PATCH then resume the error monitor.

## Security boundaries

Already implemented: authenticated ownership checks for monitor operations, AES-256-GCM at rest, no session in normal API responses, session-size/domain/origin checks, per-monitor jobs and a shared anonymous scalar cache, isolated HTML contexts/native profile revisions, and same-origin redirect guards for private request context.

These controls do not make a full account session read-only. Possession of accepted account cookies can authorize the account's actions. The service must decrypt them to make requests; application compromise or a privileged operator with the key can still expose/use them.

Before public multi-user use, decisions/implementation are still needed for:

- HTTP edge TLS and deployment/network/browser sandbox review. Hostname management is now protected by Casbin admin-role; Fetcher uses mandatory mTLS.
- Explicit retention/revocation and credential deletion. `SoftDelete` currently retains the monitor ciphertext; deletion is not immediate secret erasure. Legacy hostname/endpoint copies also remain from the additive migration.
- Key/backup protection separately from the database, exclusion of credentials from logs/error diagnostics/analytics, and minimal accepted state.
- A user-owned session collection/export flow. Do not expose a general server endpoint returning arbitrary users' stored sessions.

If the marketplace offers an official narrowly scoped read-only API, prefer it to an account cookie. No new security architecture or session-export API was introduced by this Bruno-only change.

Sources: [OWASP session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html), [Playwright authentication state warning](https://playwright.dev/docs/auth), [Bruno secret variables](https://docs.usebruno.com/secrets-management/secret-variables), [Bruno scripting reference](https://docs.usebruno.com/testing/script/javascript-reference).
