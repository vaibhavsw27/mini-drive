CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE files (
    id SERIAL PRIMARY KEY,
    owner_id INTEGER REFERENCES users(id),
    filename TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    size BIGINT,
    uploaded_at TIMESTAMP DEFAULT NOW()
);