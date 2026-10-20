# Dashboard authentication

The dashboard exchanges an API key for a browser session with `POST /v1/auth/session` and JSON `{"api_key":"cbx_…"}`. The key is verified through the API and kept only in server memory. The browser receives a 12 hour `cellbox_session` cookie marked `HttpOnly`, `SameSite=Strict`, and `Secure` whenever the request uses TLS. The response also includes a random CSRF token and sets a matching `cellbox_csrf` cookie. The UI keeps the CSRF value in tab memory and sends it as `X-CSRF-Token` for POST, PUT, PATCH, and DELETE requests.

`DELETE /v1/auth/session` invalidates the server-side session and expires both cookies. Session cookies stop working after a service restart; the user signs in again with the API key. API clients can continue to use bearer keys directly. The dashboard does not put keys in localStorage or sessionStorage.

The current dashboard implements overview/session listing, create and destroy, streamed command output, file upload/download, and text metrics. It does not yet implement the full specified detail tabs, live SSE session table refresh, metrics charts, keyboard command palette, terminal, or accessibility audit. Network, snapshot, and replay views depend on later phases.
