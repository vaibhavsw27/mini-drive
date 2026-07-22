# Mini Drive — Backend

A backend system inspired by Google Drive / Dropbox, built to demonstrate backend engineering and distributed systems concepts — authentication, object storage, and database design — rather than a simple CRUD app.

## Current Features

- **User authentication** — signup and login with securely hashed passwords (bcrypt)
- **JWT-based session handling** — stateless auth tokens issued on login, verified via middleware
- **Protected routes** — middleware blocks any request without a valid token
- **File upload** — authenticated users can upload files, stored in MinIO (S3-compatible object storage)
- **File download** — authenticated users can retrieve their own uploaded files
- **Metadata tracking** — file ownership, filename, storage location, and size tracked in PostgreSQL

## Tech Stack

- **Go** — backend language, using the standard `net/http` package
- **PostgreSQL** — relational database for users and file metadata
- **MinIO** — S3-compatible object storage for actual file bytes
- **Docker Compose** — runs Postgres and MinIO as containers for local development
- **JWT (golang-jwt)** — token-based authentication
- **bcrypt** — password hashing

## Architecture Notes

- File **bytes** and file **metadata** are stored separately: MinIO holds the raw file content, Postgres holds ownership/filename/size records. This mirrors how production systems like Dropbox/Google Drive separate storage from metadata.
- Passwords are never stored in plain text — only bcrypt hashes.
- Authentication is stateless: the server doesn't store sessions; each request proves identity via a signed JWT.

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

## API Endpoints

| Method | Route      | Auth Required | Description                  |
|--------|------------|----------------|-------------------------------|
| GET    | /health    | No             | Health check                  |
| POST   | /signup    | No             | Create a new user account     |
| POST   | /login     | No             | Log in, returns a JWT token   |
| GET    | /me        | Yes            | Returns the logged-in user ID |
| POST   | /upload    | Yes            | Upload a file                 |
| GET    | /download  | Yes            | Download a file by filename   |

## Roadmap

- [ ] Folder structure and file sharing between users
- [ ] File versioning
- [ ] Redis caching
- [ ] Background workers for async processing
- [ ] Prometheus + Grafana monitoring
- [ ] Cloud deployment