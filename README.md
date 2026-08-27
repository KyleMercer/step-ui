# Step-CA Web UI

A modern web interface for managing certificates with Smallstep Step-CA.

## Features

- Issue certificates with custom SANs
- Sign CSRs
- Certificate inventory management
- Download certificates in various formats (PEM, PFX)
- Audit logging
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
```

**Important:** Replace the values with your actual Step-CA details:
- `CA_URL`: Your Step-CA URL
- `CA_ROOT_FINGERPRINT`: The fingerprint you obtained in step 1
- `PROVISIONER_NAME`: Your provisioner name
- `PROVISIONER_PASSWORD`: Your provisioner password

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

## Build Versioning

Both images are tagged with a build identity so you can always tell exactly what's running -- useful once you have several local builds, stacks, or Dockhand/Portainer instances in play at once and want to be sure the image you're looking at is the one you think it is.

- `VERSION` at the repo root is the single source of truth for a human-meaningful build number. Bump it yourself whenever you want to mark a new build (a plain integer, like a firmware revision counter, is enough -- it doesn't need to follow any particular scheme).
- `./build.sh` builds both images via `docker compose build --no-cache --progress=plain`, reading `VERSION` plus the current git commit and build timestamp, and passing all three through to both Dockerfiles as build args. It also saves the full unfolded build log to a timestamped file so nothing scrolls by unseen.
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

- **Frontend**: Next.js 14 with TypeScript, TailwindCSS
- **Backend**: Go with Gin framework
- **Database**: SQLite for certificate metadata and audit logs
- **CA Integration**: Smallstep Step-CA via CLI

## Security Notes

- Keep your `.env` file secure and never commit it to version control
- The provisioner password is sensitive - protect it appropriately
- Consider using Docker secrets for production deployments
- Enable TLS for the backend API in production
- Implement proper authentication and authorization for production use

## License

MIT

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.
