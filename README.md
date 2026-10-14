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
- User accounts with admin/viewer roles
- Optional OIDC single sign-on (tested with Authentik), with local login kept as a fallback
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
database. The last remaining **local** (password) admin account can't be
demoted to viewer or deleted, so there's always a way in -- even when
single sign-on is configured but the identity provider is down.

This layers on top of `API_AUTH_TOKEN` (see Security Notes below), it
doesn't replace it -- that token is a separate container-to-container
shared secret between the frontend and backend, unrelated to user logins.

## Single Sign-On (OIDC)

step-ui can sign people in through any OpenID Connect provider. It's built
and tested against **Authentik**; Keycloak, Authelia, Zitadel and similar
should work the same way. SSO is optional and off until configured, and the
local username/password form always stays on the login page as a
break-glass path.

**How it works:** the login page shows a "Sign in with ..." button. The
backend runs a standard authorization-code flow with PKCE as a confidential
client (the client secret never reaches the browser), verifies the ID
token, and then starts an ordinary step-ui session -- the same cookie a
password login produces.

- **Accounts are created automatically** at a person's first SSO sign-in.
  They show in Settings → User Accounts marked **SSO**. They have no
  password in step-ui and can't use the password form.
- **Roles come from groups**, re-read at every sign-in: membership in
  `OIDC_ADMIN_GROUP` gives admin, `OIDC_VIEWER_GROUP` gives viewer (admin
  wins if both). Anyone in neither group is refused, even if the provider
  let them through. An SSO account's role can't be edited in step-ui --
  change the person's groups at the provider; it applies at their next
  sign-in.
- **No account takeover by name:** an SSO identity is matched by the
  provider's stable `sub` claim, never by username. If an SSO username
  matches an existing step-ui account (for example a local `admin`), the
  sign-in is refused and logged rather than merged.
- **Signing out** of step-ui ends only the step-ui session; you stay signed
  in to the provider and its other apps.
- **Deleting an SSO account** ends its sessions, but if the person is still
  in an allowed group the account is recreated at their next sign-in. To
  remove access, remove them from the group at the provider.

### Authentik setup

1. **Groups:** create `stepui-admins` and `stepui-viewers` (any names;
   they must match the env vars exactly, case included) and add people.
2. **Provider:** Applications → Providers → Create → *OAuth2/OpenID
   Provider*.
   - Authorization flow: `default-provider-authorization-implicit-consent`
   - Client type: **Confidential**. Note the Client ID and Client Secret.
   - Redirect URI: one entry, **Strict**, set to the backend's callback as
     the browser reaches it, e.g.
     `https://step-ui-api.example.com:8443/api/auth/oidc/callback`
   - Signing key: *authentik Self-signed Certificate* (any RSA key -- don't
     leave it blank).
   - Scopes: keep the defaults (`openid`, `profile`, `email`). Authentik's
     default `profile` mapping already includes a `groups` claim.
3. **Application:** Applications → Create, slug e.g. `step-ui`, linked to
   the provider. Optionally bind `stepui-admins` and `stepui-viewers` on
   its *Policy / Group / User Bindings* tab so Authentik itself refuses
   everyone else before they ever reach step-ui.
4. **step-ui backend environment:**

```
OIDC_ISSUER_URL=https://authentik.example.com/application/o/step-ui/
OIDC_CLIENT_ID=<client id>
OIDC_CLIENT_SECRET=<client secret>
OIDC_REDIRECT_URL=https://step-ui-api.example.com:8443/api/auth/oidc/callback
OIDC_ADMIN_GROUP=stepui-admins
OIDC_VIEWER_GROUP=stepui-viewers
OIDC_PROVIDER_NAME=Authentik
```

The issuer URL must match Authentik's exactly, **including the trailing
slash** (it's shown on the provider's overview page as "OpenID
Configuration Issuer"). `CORS_ALLOWED_ORIGIN` must be the frontend's real
browser-facing origin, because that's where the backend sends people after
signing in (and back to `/login` with an error message if it fails).
Recreate the backend container; no rebuild needed, and the frontend needs no
changes.

**Network requirements:** the backend container itself talks to the
provider (discovery, signing keys, code exchange), so it must be able to
resolve and reach the provider's hostname and trust its certificate. A
publicly trusted certificate works out of the box. If the provider's
certificate comes from your own CA (e.g. step-ca), mount that root into the
backend container and point `OIDC_CA_FILE` at it.

**Troubleshooting:** failures land back on the login page with a short
message; the real reason is in the backend log (lines start with `OIDC:`).
At startup the backend logs either `OIDC provider discovered` or a warning
explaining why discovery failed -- it retries at the next sign-in, and local
login is never affected.

| Login page says | Usual cause |
|---|---|
| Couldn't reach the identity provider | Backend can't resolve/reach/trust the issuer, or the issuer URL doesn't match exactly (check the trailing slash) |
| Not in a group that's allowed | User isn't in either group, or group names differ (case-sensitive). The log lists the groups the provider sent |
| Identity provider refused the sign-in | Authentik's application binding doesn't include the user |
| Sign-in could not be verified / expired | Took more than 10 minutes, backend restarted mid-sign-in, or the browser blocked cookies for the backend's hostname |
| Username matches an existing account | Rename or delete the conflicting local account, or set `OIDC_USERNAME_CLAIM=email` |

Authentik's own errors (e.g. `redirect_uri` mismatch) appear on Authentik's
page instead: the redirect URI there must equal `OIDC_REDIRECT_URL`
character for character (scheme, host, port, no trailing slash).

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
