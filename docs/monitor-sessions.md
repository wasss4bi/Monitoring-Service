# Monitor-owned sessions and execution

JSON and HTML requests use monitor-owned execution. The current migration sequence is described in [migration-baseline.md](migration-baseline.md).
HTML requests now use a private browser context with imported monitor cookies/storage and returned session rotation.
Capacity is shared across private contexts; admission overload defers the job without consuming an upstream retry.

## Accepted choices

- Canonical endpoint remains one URL with shared type/browser_page_url/json_source configuration; page_url is monitor-owned.
- Jobs, credentials, errors and retry belong to individual monitors. Extracted scalar cache is shared for anonymous identities by the later Fetcher decision.
- Store sessions encrypted on monitors. Do not return them from create/get/list/idempotency responses.
- managed guest sessions may bootstrap/refresh once; provided sessions never silently downgrade to guest.
- A rejected provided session waits for replacement. PATCH credentials then POST resume.
- Hostname allow-list/admin activation and the existing aggregate rate limit remain shared.
- Ozon example: ordinary price <= 75000 RUB, not a bank-card price.
- Generic rule source selects exactly one key by literal prefix; decode_json decodes a JSON string without JS execution.
  number_format=money is explicit. No marketplace adapter or implicit guessing is added.

## Contract

Custom monitor `headers` are disabled: POST rejects this field, responses omit it, and both workers ignore legacy saved caller headers. Service-owned browser/session metadata is unchanged. Region support has been dropped from the current scope; no regional session pool is implemented.

POST /me/monitors requires product page_url and description on every rule (1..255 characters). Optional browser_page_url selects native JSON capture independently of the product link. json_source instead fetches the product page over HTTP; combining it with browser_page_url is rejected.

POST /me/monitors accepts session_mode (managed/provided) and an optional opaque v1 session.
If session is supplied, omitted mode defaults to provided; otherwise managed.
The v1 export uses version=1, userAgent, cookieJar, optional clientHints/sessionHeaders/storageState.
Cookies must be scoped to the monitored host or its parent domain; storage origins must match.
The browserProfile identifier is internal and is discarded on user import.
The session payload is limited to 256 KiB. Keep it in a secret variable, never a committed example.
The supplied-session API already exists, but there is no authenticated user-facing collection/export workflow yet. The development noVNC tool is not a multi-user onboarding/export service. See [Bruno session setup](bruno-session-setup.md) for the current variables, v1 export source and security gaps; no new onboarding architecture was selected by that client setup.

PATCH /me/monitors/{id} accepts a credential replacement operation, separate from name/threshold updates.
A replacement invalidates the in-flight generation and increments the browser profile revision.
Replacing credentials does not automatically resume a stopped/error/success monitor.

Rules can contain:
```json
{
  "path": "price",
  "source": {"path": "widgetStates", "key_prefix": "webPrice-", "decode_json": true},
  "number_format": "money",
  "operator": "<=",
  "value": 75000,
  "description": "Обычная цена не выше 75000 ₽"
}
```
price and cardPrice are deliberately different; no automatic fallback is performed.
Default number parsing is unchanged. Ambiguous sources or monetary formats are not selected at random.

## Cutover limits

The initial migrations create the monitoring schema in a new database.
The AES-GCM purpose remains target-session:v1; keep the existing encryption key.
Historical fetches retain NULL ownership and cannot be claimed as monitor jobs.
The (monitor,generation) key replaces the old (endpoint,generation) key; historical
jobs retain a partial unique index. Legacy secret columns are retained for compatibility.
Do not run old and new execution versions together. See [baseline transition](migration-baseline.md).

JSON native profiles are scoped to monitor ID plus credential revision.
The isolated HTML helper is connected to the request path; contexts close on completion, failure and cancellation.
Session export respects the overall deadline and returns only the monitored host's cookies and origin storage.
Session import/expiry can still fail due to remote access policy; a valid export is not a guarantee of site acceptance.

## Verification

Regression tests cover scheduling, credential isolation, lease recovery, stale results,
independent failure/retry, provided-session rejection, native browser and HTML source
flows, matched rule descriptions and notification snapshots. See integration/ and
internal/repository/*integration_test.go. External website acceptance is not a unit-test guarantee.

## Fetcher transition

Retrieval and authoritative session serialization belong to wasss4bi/values-from-body-fetcher. Monitoring-Service keeps encrypted envelopes and generation fencing. Admin reset-session uses managed-only scope. Bootstrap policy is PostgreSQL-owned and admin-only through Casbin. See [Fetcher runbook](fetcher/README.md).
