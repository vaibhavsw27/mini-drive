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

CREATE TABLE file_shares (
    id SERIAL PRIMARY KEY,
    file_id INTEGER REFERENCES files(id),
    shared_with_user_id INTEGER REFERENCES users(id),
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(file_id, shared_with_user_id)
);