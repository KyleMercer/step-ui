# Step-CA Web UI

A modern web interface for managing certificates with Smallstep Step-CA.

## Acknowledgement

This user interface would not be possible without the effort by @marcin-kruszynski in the first place.
The enhancements made to the original step-ui were largely the work of Claude AI.

## Disclaimer

Any use of step-ui should be confined to personal, non-mission critical applications such as a homelab. While
additional security hardening has been incorporated, the security posture of this product should be considered
unsuitable for production use in a business/enterprise environment.  

## Features

- Issue certificates with custom SANs
- Sign CSRs
- Certificate inventory management
- Download certificates in various formats (PEM, PFX)
- Audit logging
- Expiry alerts by Pushover, MQTT, webhook (JSON, ntfy, Discord) or email, configured from the web UI
- Modern, responsive UI

## Prerequisites

- Docker and Docker Compose
- A running Step-CA instance
- Step-CA provisioner credentials

## Configuration

### 1. Get the CA Root Fingerprint

The CA root fingerprint is required for secure communication with Step-CA. You can obtain it in several ways:

**Option A: From the Step-CA server directly**
```bash
# SSH into your Step-CA server and run:
step certificate fingerprint $(step path)/certs/root_ca.crt
```

**Option B: From the Step-CA logs**
When Step-CA starts, it prints the root fingerprint in the logs. Look for a line like:
```
Root fingerprint: 1a2b3c4d5e6f7g8h9i0j1k2l3m4n5o6p7q8r9s0t1u2v3w4x5y6z7a8b9c0d1e2f
```

**Option C: Using curl and openssl**
```bash
# Download the root certificate
curl -sk https://your-ca-url:9000/root > /tmp/root_ca.crt

# Calculate the fingerprint
openssl x509 -in /tmp/root_ca.crt -noout -fingerprint -sha256 | cut -d= -f2 | tr -d ':'
```

### 2. Create Environment File

Create a `.env` file in the project root with your Step-CA configuration:

```bash
# Step-CA Configuration
CA_URL=https://ca.home:9000
CA_ROOT_FINGERPRINT=your_root_fingerprint_here
PROVISIONER_NAME=ui-admin
PROVISIONER_PASSWORD=your_provisioner_password

# Optional but recommended -- see Security Notes below
API_AUTH_TOKEN=
NEXT_PUBLIC_API_AUTH_TOKEN=

# Creates the first admin account -- see "User Accounts & Roles" below
INITIAL_ADMIN_USERNAME=admin
INITIAL_ADMIN_PASSWORD=
```

**Important:** Replace the values with your actual Step-CA details:
- `CA_URL`: Your Step-CA URL
- `CA_ROOT_FINGERPRINT`: The fingerprint you obtained in step 1
- `PROVISIONER_NAME`: Your provisioner name
- `PROVISIONER_PASSWORD`: Your provisioner password
- `API_AUTH_TOKEN` / `NEXT_PUBLIC_API_AUTH_TOKEN`: set both to the same
  value (e.g. output of `openssl rand -hex 32`) to require a bearer token
  on the API; see Security Notes
- `INITIAL_ADMIN_USERNAME` / `INITIAL_ADMIN_PASSWORD`: creates the first
  login account on first boot; see "User Accounts & Roles" below

### 3. Frontend API URL

`NEXT_PUBLIC_API_URL` (in `docker-compose.yml`/`.env`) controls the backend URL the browser talks to. It behaves like a normal runtime environment variable: change it and recreate the frontend container (`docker compose up -d`, or Dockhand's "save and redeploy"/"force recreate") -- no image rebuild needed. Under the hood the value still gets substituted into the compiled frontend at container start (`frontend/docker-entrypoint.sh`), because Next.js normally bakes `NEXT_PUBLIC_*` variables into the JS bundle at build time and would otherwise ignore anything set afterwards.

Make sure `NEXT_PUBLIC_API_URL` is actually set in your `.env` file (or Dockhand's own env var UI) -- `docker-compose.yml` references it as plain `${NEXT_PUBLIC_API_URL}` with no compose-level default, since some Docker management UIs (Dockhand included) don't support the `${VAR:-default}` bash-style fallback syntax in their own variable fields. If it's ever left unset, the entrypoint script fails safe rather than breaking the container: it logs a warning and leaves the frontend serving its build-time placeholder instead of guessing at a value.

### 4. Setup Step-CA Provisioner (if not already done)

If you haven't created a provisioner for the UI yet, run this on your Step-CA server:

```bash
step ca provisioner add ui-admin --type=JWK --create
```

Save the password that's generated - you'll need it for the `PROVISIONER_PASSWORD` in your `.env` file.

## Quick Start

1. Clone this repository
2. Configure your `.env` file (see Configuration section above)
3. Start the services:

```bash
./setup.sh
```

Or manually:

```bash
docker compose build
docker compose up -d
```

4. Access the web interface at `http://localhost:3000`

## User Accounts & Roles

step-ui requires signing in. There's no self-registration -- accounts are
created and managed by an admin from the Settings page.

**First login:** on first boot, if no accounts exist yet, the backend
creates one admin account from `INITIAL_ADMIN_USERNAME` /
`INITIAL_ADMIN_PASSWORD` in your `.env`. This only ever happens once --
those two variables are checked only when the accounts table is completely
empty, and are ignored on every start after that (safe to leave set, blank
them out, or change them; none of it matters once an account exists). If
the table is empty and they're unset, the backend logs a startup warning
and nobody can log in until you set them and restart.

**Roles:**
- **admin** -- full access: issue, sign, renew, and revoke certificates,
  view CA settings, and manage user accounts (Settings → User Accounts).
- **viewer** -- read-only: can view the certificate inventory and CA
  settings, but not issue/sign/renew/revoke, and can't manage users. The
  nav bar hides links to pages a viewer can't use, but that's just UX --
  the backend independently rejects those requests for a viewer account
  regardless of what the frontend shows.

**Sessions:** logging in sets a session cookie good for 3 days, with no
idle timeout (it's a flat expiry from login, not extended by activity).
Logging out, changing a user's password, or deleting a user account all
immediately invalidate any session tied to that account -- there's nothing
client-side to expire, the session is just gone from the backend's
database. The last remaining admin account can't be demoted to viewer or
deleted, so there's no way to lock yourself out entirely once at least one
admin exists.

This layers on top of `API_AUTH_TOKEN` (see Security Notes below), it
doesn't replace it -- that token is a separate container-to-container
shared secret between the frontend and backend, unrelated to user logins.

## Expiry Alerts

step-ui can warn you before certificates expire, and again when they do,
instead of you finding out from a dashboard (or a broken service). It's off
until an admin turns it on from the **Alerts** page; everything is set up
there, with no environment variables or restarts.

**What gets sent, and when:**
- One alert as each active certificate reaches each threshold -- 30, 7 and
  1 day(s) before expiry by default (editable) -- and one when it expires.
  Each alert goes out once. Renewing a certificate gives it a new expiry
  date, so its warnings start over. Revoked certificates are ignored.
- Turning alerts on late doesn't flood you: a certificate already 3 days
  from expiry gets the 7-day alert only, not the 30-day one too.
- Certificates are checked every hour. Everything due in one check is
  combined into one message per destination.
- If every destination fails, nothing is marked as sent and the next check
  tries again. If at least one succeeds, the alert counts as sent; failures
  show on the Alerts page and in the backend log.

**Destinations** (add as many as you like, each with its own Test button):

| Type | Notes |
|---|---|
| Pushover | App token + user/group key. Separate priorities for warnings and for expired certificates (default: normal and high). |
| MQTT | `tcp://` or `ssl://` broker. Publishes an event per alert to `<prefix>/alerts`, and retained status for every certificate to `<prefix>/certs/<name>` plus `<prefix>/summary` on every check -- handy for Node-RED, Home Assistant or a status display. |
| Webhook | Generic JSON (`alerts` array with cn, serial, not_after, days_left, level, expired), ntfy, or Discord. Optional Authorization header. |
| Email | SMTP with STARTTLS, TLS or none; optional login. Plain-text message. |

**Other settings on the Alerts page:**
- *step-ui address*: used for the "open step-ui" link in messages.
- *Time zone*: for dates in messages (e.g. `America/Edmonton`); defaults to
  the container's, usually UTC.
- *Trust this CA's root*: lets destinations use certificates issued by
  your own step-ca (an internal ntfy server, broker or mail server). The
  root is fetched from `CA_URL` and checked against `CA_ROOT_FINGERPRINT`;
  it's only trusted if they match.

Admins can change everything; viewers see the page read-only, without
secret values or delivery details. Changes to the settings and every alert
sent are recorded in the audit log.

### Settings store and secrets

Settings changed from the web UI (currently the alert settings) are kept
in step-ui's database, not in environment variables. Secrets in them
(tokens, passwords, webhook URLs) are encrypted with AES-256-GCM and are
never sent back to the browser: the page only shows "saved", with Replace
and Clear buttons.

The encryption key comes from a file:
- **Default:** `settings.key`, created automatically on first start in the
  same folder as the database (the `data` volume). Back it up together
  with the database -- without it, saved secrets can't be decrypted and
  have to be re-entered (the Alerts page says which).
- **Docker secret (optional):** set `SETTINGS_KEY_FILE` to the secret's
  path, e.g. `/run/secrets/step_ui_settings_key`, containing a long random
  string (`openssl rand -hex 32`). This keeps the key out of the data
  folder, so a copy of the data folder alone doesn't expose the secrets.
  Switching from the default key to a secret (or changing the secret) means
  re-entering saved secrets once.

## Build Versioning

Both images are tagged with a build identity so you can always tell exactly what's running -- useful once you have several local builds, stacks, or Dockhand/Portainer instances in play at once and want to be sure the image you're looking at is the one you think it is.

- `VERSION` at the repo root is the single source of truth for a human-meaningful build number. Bump it yourself whenever you want to mark a new build (a plain integer, like a firmware revision counter, is enough -- it doesn't need to follow any particular scheme).
- `./build.sh` builds both images via `docker compose --progress=plain build --no-cache`, reading `VERSION` plus the current git commit and build timestamp, and passing all three through to both Dockerfiles as build args. It also creates `data/` and `logs/` if they don't exist yet, and saves the full unfolded build log to a timestamped file under `logs/` so nothing scrolls by unseen.
- Both Dockerfiles print the version/commit/date early in the build (visible in the log) and bake them into the image as standard OCI labels (`org.opencontainers.image.version`, `.revision`, `.created`). Docker UIs that read image labels -- Portainer confirmed, likely Dockhand too -- will surface these directly; otherwise `docker inspect <container-or-image> --format '{{json .Config.Labels}}'` always shows the truth regardless of what tag the image happens to be under.
- The same identity is also visible right in the web UI, no `docker inspect` needed: every page's footer shows the version number (e.g. `step-ui v1`), and the Settings page has a "System Information" section with the full detail for both images -- version, commit, build date, plus the Node.js/Next.js versions the frontend is running and the Go version the backend is running. The backend also serves this itself at `GET /api/version`.

If you build with plain `docker compose build` instead of `./build.sh`, these all default to `unknown` rather than silently showing stale or misleading info.

## Usage

### Issue a Certificate

1. Navigate to "Issue Certificate" in the navigation menu
2. Enter the Common Name (CN) and any Subject Alternative Names (SANs)
3. Set the validity period
4. Choose the download format (PEM or PFX)
5. Click "Issue Certificate"
6. Download the certificate bundle

### Sign a CSR

1. Navigate to "Sign CSR" in the navigation menu
2. Paste your Certificate Signing Request (CSR) in PEM format
3. Set the validity period
4. Click "Sign CSR"
5. Download the signed certificate

### View Certificate Inventory

1. Navigate to "Inventory" to see all issued certificates
2. View certificate details, status, and expiration dates
3. Filter by status (active, expired, expiring soon)

## Troubleshooting

### Error: 'step ca token' requires the '--root' flag

This error means the `CA_ROOT_FINGERPRINT` is missing or incorrect in your `.env` file. Follow the configuration steps above to obtain and set the correct fingerprint.

### Error: connection refused

- Verify your Step-CA is running and accessible at the configured `CA_URL`
- Check that the CA_URL is correct in your `.env` file
- Ensure there are no firewall rules blocking access

### Error: unauthorized

- Verify your provisioner name and password are correct
- Ensure the provisioner exists on your Step-CA server
- Check the provisioner hasn't been disabled

### The UI keeps calling the wrong backend URL after changing NEXT_PUBLIC_API_URL

If a plain restart or redeploy doesn't pick up a new `NEXT_PUBLIC_API_URL`, the frontend image predates the runtime-substitution fix described above and still bakes the value in at build time. Rebuild the frontend image once (`docker compose build frontend`, or Dockhand's "build images" rather than "save and redeploy" -- the latter recreates the container from the existing image without rerunning the build, so it can't pick up a value that's compiled into the JS). After that one rebuild, changing `NEXT_PUBLIC_API_URL` and recreating the container is enough going forward.

## Development

### Backend

```bash
cd backend
go run ./cmd
```

### Frontend

```bash
cd frontend
npm install
npm run dev
```

## Architecture

- **Frontend**: Next.js 16 with TypeScript, TailwindCSS
- **Backend**: Go with Gin framework
- **Database**: SQLite for certificate metadata and audit logs
- **CA Integration**: Smallstep Step-CA via CLI

## HTTPS

Both containers serve plain HTTP to each other and to whatever's mounted -- there's no TLS code in step-ui itself, deliberately. Certificate storage, permissions, and renewal are a well-solved problem one layer up; a reverse proxy in front of the stack handles it far more robustly than anything hand-rolled here would, and it's the standard pattern for exactly this kind of setup. Terminate TLS at the proxy and step-ui never needs to know about certificates at all.

Once you put a reverse proxy in front, two settings need to point at the proxy's HTTPS origin instead of the containers' plain-HTTP ones:

- `NEXT_PUBLIC_API_URL` -- the URL the browser calls directly, e.g. `https://step-ui-api.home.arpa`
- `CORS_ALLOWED_ORIGIN` -- the frontend's own origin as the browser now sees it, e.g. `https://step-ui.home.arpa`

Any reverse proxy works (Caddy, Traefik, NGINX Proxy Manager, Synology's built-in one, etc.) -- point it at the frontend and backend containers' plain ports (3000 and 8080) and give it whatever certificate you want the browser to see. If you're already running step-ca for other things, **Caddy** is worth a look specifically: it can request and renew real certificates from your own step-ca automatically over ACME, instead of a public CA -- no self-signed browser warnings, and no paid wildcard needed. That does mean adding an ACME provisioner to step-ca (separate from the JWK provisioner step-ui itself uses): `step ca provisioner add acme --type ACME` on the CA host.

Since step-ca's own ACME endpoint is itself serving a certificate signed by your CA (not a publicly trusted one), Caddy also needs to be told to trust your CA's root when it calls that endpoint -- via `trusted_roots`, pointed at your CA's root certificate (`step ca root` prints it, or it's at `$(step path)/certs/root_ca.crt` on the CA host). A minimal example:

```
step-ui.example.com {
    tls {
        issuer acme https://ca.example.com:9000/acme/acme/directory {
            trusted_roots /etc/caddy/ca-root.pem
        }
    }
    reverse_proxy step-ui-frontend:3000
}

step-ui-api.example.com {
    tls {
        issuer acme https://ca.example.com:9000/acme/acme/directory {
            trusted_roots /etc/caddy/ca-root.pem
        }
    }
    reverse_proxy step-ui-backend:8080
}
```

(`acme/acme` in the URL is `/acme/<provisioner name>/directory` -- adjust to match your own provisioner's name. `/etc/caddy/ca-root.pem` is wherever you mount the CA's root cert into the Caddy container -- e.g. bind-mounting it read-only straight from the CA host's filesystem if Caddy runs there too. If Caddy runs in Docker, put it on the same network as the step-ui containers so it can reach them by service name, as above; otherwise reverse-proxy to the host's published `3000`/`8080` instead. Both hostnames need to actually resolve, internally, to wherever Caddy is listening -- step-ca has to be able to reach Caddy at that name to complete the ACME challenge, same as any ACME issuance.)

If Caddy runs on the same host as step-ca itself, port 443 is likely already taken by step-ca -- move Caddy's own HTTPS listener to another port (append it to the site address, e.g. `step-ui.example.com:8443 { ... }`, browsers then need that port in the URL too) and add `disable_tlsalpn_challenge` inside the `issuer acme` block, since the TLS-ALPN-01 challenge is hardcoded by the ACME spec to port 443 and would otherwise collide. That leaves HTTP-01 (port 80) as the challenge method, which is fine as long as step-ca isn't also using port 80.

NGINX Proxy Manager and most GUI reverse proxies don't support pointing at a custom/private ACME server -- only Let's Encrypt or a manually uploaded certificate -- so with those you're back to bring-your-own-cert with manual renewal, same as if step-ui handled it directly. Fine if you already have a wildcard cert you're managing that way regardless.

## Security Notes

- Keep your `.env` file secure and never commit it to version control
- The provisioner password is sensitive - protect it appropriately
- Consider using Docker secrets for production deployments
- Set `API_AUTH_TOKEN` (and matching `NEXT_PUBLIC_API_AUTH_TOKEN`) in
  `.env` if this is reachable beyond localhost. Without it, anyone who can
  reach the backend port can issue, sign, and revoke certificates with no
  access control at all. This is a shared secret between the frontend and
  backend containers, not full user authentication -- it's shipped in the
  frontend's client-side JS bundle, so treat it accordingly. See the
  comments in `example.env` for details and generating a token.
- `CORS_ALLOWED_ORIGIN` restricts which origin the backend accepts
  browser requests from (defaults to `http://localhost:3000`). Set it if
  you access the frontend at a different host/port.
- User accounts (admin/viewer roles, session cookies) gate both the web
  pages and the API itself -- see "User Accounts & Roles" above. Session
  cookies are set without `Secure`, matching every other design choice
  here around running over plain HTTP on a home LAN; if you put this
  behind real TLS, consider that when judging your own exposure.

## License

MIT

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.
