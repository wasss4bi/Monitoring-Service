# Price monitors in Bruno

Workspace: `docs/bruno/workspace.yml`. Requests are in `collections/gO/Monitors`.
`Create monitor.yml` contains six commented bodies separated by site labels.
Uncomment one complete object. The pre-request script removes full-line comments.
The six separate site requests can also be run independently.

| Example | Retrieval |
| --- | --- |
| DNS | Product page and exact microdata JSON capture via browser_page_url |
| Wildberries | Product page and exact JSON response capture via browser_page_url; price is in kopecks |
| Ozon | Composer JSON; explicit widget key prefix, decoded JSON and money parsing for ordinary price |
| Avito | HTML product price selector |
| Lamoda | Explicit json_source from product HTML over HTTP/Squid |
| Lemanapro | HTML numeric price attribute; the selected unit is square metre |

Examples use managed guest sessions. Browserless v1 is the default browser backend;
Chrome/noVNC remains available for manual diagnosis. Access is controlled by the
remote site: guest Ozon and Lemanapro can reject requests. Historical successful
runs with exported sessions do not guarantee automatic guest access.

Before running:
- Configure baseUrl and accessToken; Login can populate accessToken. Idempotency-Key is generated automatically when no manual header is set.
- Log in with Login admin to populate adminAccessToken, then register/activate the hostname with an appropriate request budget.
- Configure an active verified notification contact.
- Set a price threshold appropriate to the current product and its units.
- Each send without a manual Idempotency-Key creates a new operation. For an exact retry, specify the previous key from Timeline explicitly.

Every request requires product page_url and descriptions for its rules.
Shared endpoint type/browser_page_url/json_source must agree for a canonical URL.
Custom caller headers are disabled. Optional provided-session exports are credentials:
keep them outside Git and logs. See [session setup](bruno-session-setup.md).

The source examples contain historical price thresholds, not live price guarantees.
A successful check sends a notification only after enough rules match. A remote
access failure is reported separately and must not be treated as a price match.
