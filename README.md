# form-mailer

> env-only with multi-site support

A tiny Go HTTP service to handle contact forms for multiple websites and email you the submissions.

- All configuration via environment variables (no config files or DB)
- Multi-tenant: one container → many sites, each with its own recipient and options
- SMTP: global fallback + optional per-site overrides
- Lightweight: single static binary, ~10–20 MB RAM
- Formats: accepts JSON and form-encoded
- Safety: per-IP rate limiting, optional HMAC signature, honeypot field, CORS per site

## Endpoints

### Health

- GET /health — Liveness probe.
- 200 {"ok": true} on success

### Contact

- POST /v1/contact/{siteKey} — Submits a contact form for the specified site.
- Body: either application/json or application/x-www-form-urlencoded
- Required fields: name, email, message
- Honeypot field: website (must be empty)
- Optional header if HMAC is enabled per-site: `X-Signature: <hex(hmac_sha256(raw_body, SECRET))>`
- 400 invalid submission / bad input
- 401 HMAC required or mismatch
- 413 payload too large (see MAX_BODY_KB)
- 429 rate limited
- 500 SMTP send failed (check logs & SMTP settings)

## Environment Variables

### Global (required)

| Name      | Description                                                       |
| --------- | ----------------------------------------------------------------- |
| SMTP_HOST | SMTP server host (e.g., smtp.postmarkapp.com)                     |
| SMTP_PORT | SMTP port (e.g., 587 for STARTTLS or 465 for SMTPS)               |
| SMTP_USER | SMTP username / sender identity                                   |
| SMTP_PASS | SMTP password / token                                             |
| SMTP_SSL  | true for SMTPS on 465, false for STARTTLS/plain on 587/25         |
| SITES     | Comma-separated list of site keys (e.g., my-site1,product-site-2) |

### Global (optional)

| Name                      | Description                                                           | Default Value |
| ------------------------- | --------------------------------------------------------------------- | ------------- |
| LISTEN_ADDR               | Address to listen for endpoints                                       | `:3000`       |
| FROM_ADDR                 | Explicit “From” address (use a domain verified at your SMTP provider) | `SMTP_USER`   |
| SUBJECT_PREFIX            | Default email subject prefix                                          | `[Contact]`   |
| RATE_LIMIT_BURST          | Tokens per IP+site                                                    | 3             |
| RATE_LIMIT_REFILL_MINUTES | Refill rate                                                           | 1             |
| ALLOW_JSON                | Allow accepting JSON input                                            | true          |
| ALLOW_FORM                | Allow `application/x-www-form-urlencoded` input                       | true          |
| MAX_BODY_KB               | Max request size in KB                                                | 1024          |

### Per-Site (Required)

For each site key listed in SITES, define variables by upper-casing the key and replacing non-alphanumeric characters with \_. e.g. For site key `my-site`, it will be ``.

| Name                      | Description                                            | Example                                           |
| ------------------------- | ------------------------------------------------------ | ------------------------------------------------- |
| `<SITE>`\_TO              | Recipient email for that site                          | MY_SITE_TO="[email protected]"                    |
| `<SITE>`\_ALLOWED_ORIGINS | Comma-separated list of exact origins allowed for CORS | e.g., https://my-site.com,https://www.my-site.com |
| `<SITE>`\_SUBJECT_PREFIX  | Subject prefix override for that site                  | [MySite]                                          |

### Per-Site (Optional)

| Name                | Description                                                                   |
| ------------------- | ----------------------------------------------------------------------------- |
| `<SITE>`\_SECRET    | If set, requests must include X-Signature: hex(hmac_sha256(raw_body, SECRET)) |
| `<SITE>`\_SMTP_HOST | SMTP Host for that particular site                                            |
| `<SITE>`\_SMTP_PORT | SMTP Port for that particular site                                            |
| `<SITE>`\_SMTP_USER | SMTP user for that particular site                                            |
| `<SITE>`\_SMTP_PASS | SMTP password for that particular site                                        |
| `<SITE>`\_SMTP_SSL  | SMTP SSL certificate to use for that particular site                          |

If SMTP settings are not provided, the global SMTP settings are used.

## Examples

### HTML Form (form-encoded)

```html
<form method="POST" action="https://forms.example.com/v1/contact/my-site">
  <input name="name" required />
  <input name="email" type="email" required />
  <textarea name="message" required></textarea>
  <!-- honeypot: must remain empty -->
  <input type="text" name="website" style="display:none" />
  <button type="submit">Send</button>
</form>
```

### JSON Submit (fetch)

```js
async function sendContact(siteKey, data) {
  const res = await fetch(`https://forms.example.com/v1/contact/${siteKey}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}
```

### HMAC Signature (recommended for public endpoints)

```js
// Node.js example
import crypto from "node:crypto";

function hexHmacSha256(bodyString, secret) {
  return crypto.createHmac("sha256", secret).update(bodyString).digest("hex");
}

const body = JSON.stringify({
  name: "Alice",
  email: "[email protected]",
  message: "Hi!",
});
const sig = hexHmacSha256(body, process.env.MY_SITE_SECRET);

await fetch("https://forms.example.com/v1/contact/my-site", {
  method: "POST",
  headers: { "Content-Type": "application/json", "X-Signature": sig },
  body,
});
```

### cURL Test

```bash
curl -X POST https://forms.example.com/v1/contact/my-site \
 -H "Content-Type: application/json" \
 -d '{"name":"Alice","email":"[email protected]","message":"Hello!"}'
```

### Minimal HTML Example (with fetch + fallback)

```html
<form
  id="contact"
  action="https://forms.example.com/v1/contact/my-site"
  method="POST"
>
  <input name="name" required />
  <input name="email" type="email" required />
  <textarea name="message" required></textarea>
  <input type="text" name="website" style="display:none" />
  <button type="submit">Send</button>
</form>
<script>
  document.getElementById("contact").addEventListener("submit", async (e) => {
    e.preventDefault();
    const f = e.target;
    const data = {
      name: f.name.value,
      email: f.email.value,
      message: f.message.value,
      website: f.website.value,
    };
    const res = await fetch(f.action, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
    alert(res.ok ? "Thanks!" : "Failed: " + (await res.text()));
  });
</script>
```

## Setup

### Setup via Coolify

1. Add → Application in Coolify.
2. Repository: your repo containing the provided Dockerfile and main.go.
3. Build Method: Dockerfile. Port: 3000.
4. Environment: set all variables [described below](#environment-variables).
5. Domain: e.g., forms.example.com. Traefik handles HTTPS automatically.
6. Deploy, then post to `https://forms.example.com/v1/contact/<siteKey>`.

### Docker (local)

```bash
docker build -t form-mailer:latest .

docker run --rm -p 3000:3000 \
 -e SITES="my-site,my-product" \
 -e SMTP_HOST="smtp.postmarkapp.com" \
 -e SMTP_PORT="587" \
 -e SMTP_USER="postmark-user" \
 -e SMTP_PASS="postmark-pass" \
 -e SMTP_SSL="false" \
 -e MT_SITE_TO="[email protected]" \
 -e MY_SITE_ALLOWED_ORIGINS="https://mysite.com,https://www.mysite.com" \
 -e MY_PRODUCT_TO="[email protected]" \
 -e MY_PRODUCT_ALLOWED_ORIGINS="https://my-product.com" \
 form-mailer:latest
```

### Troubleshooting

- 400 invalid submission: missing name/email/message, invalid email, or honeypot filled.
- 401 unauthorized: HMAC required by site but X-Signature missing or wrong.
- 413 payload too large: increase `MAX_BODY_KB` or reduce content size.
- 429 rate limited: reduce frequency per IP or increase `RATE_LIMIT_BURST`.
- 500 failed to send: check SMTP host/port/credentials, `FROM_ADDR` domain verification, provider logs.
- CORS blocked: ensure `<SITE>`\_ALLOWED_ORIGINS matches the requesting page’s exact origin (https://domain.tld).