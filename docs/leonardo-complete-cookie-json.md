# Leonardo Complete Cookie JSON

Leonardo browser sessions are stored as encrypted complete Cookie JSON, never as
an AT or a manually assembled Cookie Header. The JSON must be a Chrome or
Patchright cookie array for `leonardo.ai` and include the Better Auth
`session_token` plus all `session_data` fragments.

## Admin import

Use the account-row shield upload action for an existing account, or choose a
Cookie JSON file while creating an account. The API accepts the same body:

```json
{
  "cookie_json": [
    {
      "name": "__Secure-better-auth.session_token",
      "value": "REDACTED",
      "domain": ".leonardo.ai",
      "path": "/",
      "httpOnly": true,
      "secure": true,
      "sameSite": "Lax"
    }
  ]
}
```

`PUT /admin/api/accounts/{id}/cookie-json` validates scope and required cookie
parts, encrypts the upload, and queues a browser verification. The existing
active session remains untouched until the worker calls Leonardo
`get-session` and receives a valid AT. A successful verification atomically
promotes the new full JSON, refreshed AT, and compatibility Cookie Header.

The worker deletes temporary profiles, JSON files, screenshots, and HAR files
after each job. API responses, account lists, and audit metadata expose only
whether a complete Cookie JSON is saved or pending; they never return its
contents.

## Legacy records

Existing accounts with only an encrypted Cookie Header remain readable during
the migration period. New account creation and recovery imports require full
Cookie JSON. Once a browser refresh succeeds, the worker stores the full JSON
for all future restores.
