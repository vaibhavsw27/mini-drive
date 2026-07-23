# Mini Drive — Backend

A backend system inspired by Google Drive / Dropbox, built to demonstrate backend engineering and distributed systems concepts — authentication, object storage, caching, async processing, and observability — rather than a simple CRUD app.

## Features

- **User authentication** — signup and login with bcrypt-hashed passwords
- **JWT-based sessions** — stateless auth tokens issued on login, verified via middleware
- **Protected routes** — middleware blocks any request without a valid token
- **File upload/download** — authenticated users can upload and retrieve files, stored in MinIO (S3-compatible object storage)
- **File sharing** — file owners can grant other users access to specific files by email
- **File versioning** — re-uploading a filename creates a new version instead of overwriting; old versions remain retrievable
- **Redis caching** — file metadata lookups are cached (cache-aside pattern, 5-minute TTL) to reduce database load on repeated downloads
- **Background workers** — file uploads trigger an async job (via a Redis-backed queue) that computes a SHA-256 checksum without blocking the upload response
- **Prometheus metrics** — request counts and latency exposed at `/metrics`, scraped by Prometheus for observability

## Tech Stack

- **Go** — backend language (`net/http`, no framework)
- **PostgreSQL** — relational database for users, file metadata, and sharing records
- **MinIO** — S3-compatible object storage for file bytes
- **Redis** — caching layer and background job queue
- **Docker Compose** — runs Postgres, MinIO, Redis, Prometheus, and Grafana locally
- **JWT (golang-jwt)** — token-based authentication
- **bcrypt** — password hashing
- **Prometheus + Grafana** — metrics collection and visualization

## Architecture Notes

- **Storage vs. metadata separation**: file bytes live in MinIO; ownership, filenames, sizes, and versions live in Postgres. This mirrors how production systems like Dropbox separate storage from metadata, and means the storage layer could be swapped for real AWS S3 or Cloudflare R2 with no code changes, since MinIO is S3-API-compatible.
- **Stateless auth**: the server holds no session state — each request proves identity via a signed JWT, which is how most systems handle auth at scale.
- **Authorization, not just authentication**: file access checks whether a user is the owner OR has been explicitly shared the file, enforced at the database query level.
- **Cache-aside pattern**: file downloads check Redis first; on a miss, Postgres is queried and the result is cached for 5 minutes. This introduces a deliberate trade-off — a file re-shared or modified may briefly serve a stale cache entry until the TTL expires.
- **Async processing**: uploads push a lightweight job onto a Redis list; a background goroutine consumes jobs independently (checksum computation), decoupling slow work from the request/response cycle.

## Running Locally

1. Start dependencies:
   ```
   docker compose up -d
   ```
2. Build and run the server:
   ```
   go build -o server.exe main.go
   .\server.exe
   ```
3. Server runs on `http://localhost:8080`

## Deployment

The authentication and API layer is deployed on **Railway**, using Railway-managed Postgres and Redis instances, with connection details injected via environment variables (`DATABASE_URL`, `REDIS_URL`) — the code falls back to local Docker addresses when these aren't set, so the same binary runs unmodified in both environments.

Object storage (MinIO) currently runs local-only in this deployment. In a production setup, this would point to a real S3-compatible provider (AWS S3, Cloudflare R2) — no application code changes required, only swapping endpoint/credentials, since the app talks to storage exclusively through the S3 API.

## API Endpoints

| Method | Route      | Auth Required | Description                          |
|--------|------------|----------------|---------------------------------------|
| GET    | /health    | No             | Health check                          |
| POST   | /signup    | No             | Create a new user account             |
| POST   | /login     | No             | Log in, returns a JWT token           |
| GET    | /me        | Yes            | Returns the logged-in user ID         |
| POST   | /upload    | Yes            | Upload a file (creates a new version) |
| GET    | /download  | Yes            | Download a file (`?version=N` optional) |
| POST   | /share     | Yes            | Share a file with another user by email |
| GET    | /metrics   | No             | Prometheus metrics endpoint           |

## Roadmap

- [ ] Reorganize codebase into standard Go project layout (`/cmd`, `/internal`)
- [ ] Load testing with published latency/throughput numbers
- [ ] Rate limiting per user
- [ ] Outbox pattern for storage/database consistency guarantees
- [ ] Full cloud object storage integration