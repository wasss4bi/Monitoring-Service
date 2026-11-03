# Manual browser diagnostics moved

The implementation, overlay and operator helper now belong to
[values-from-body-fetcher](https://github.com/wasss4bi/values-from-body-fetcher/blob/v0.1.1/docs/manual-browser.md).
Use its checkout and deployment configuration. The helper pauses the facade and
opens a separate diagnostic volume, not active Browserless profiles. Keep old
local profiles and volumes; do not open Chrome 121 state with a different Chrome
version. Monitoring-Service has no browser build or direct Node integration.
