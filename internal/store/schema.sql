-- KuliDev knowledge base schema (v0.1). Applied idempotently on every open.

CREATE TABLE IF NOT EXISTS runs (
    id          INTEGER PRIMARY KEY,
    root        TEXT NOT NULL,
    commit_hash TEXT,
    started_at  TEXT NOT NULL,
    duration_ms INTEGER,
    files       INTEGER,
    status      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
    path   TEXT PRIMARY KEY,
    lang   TEXT,
    size   INTEGER,
    lines  INTEGER,
    sha256 TEXT,
    parsed INTEGER NOT NULL DEFAULT 0,
    error  TEXT
);

-- Engineering entities: FILE, PACKAGE, EXTERNAL, and symbols (function, method, class, ...).
CREATE TABLE IF NOT EXISTS nodes (
    id         INTEGER PRIMARY KEY,
    kind       TEXT NOT NULL,
    name       TEXT NOT NULL,
    qualname   TEXT NOT NULL,
    path       TEXT,
    line_start INTEGER,
    line_end   INTEGER,
    lang       TEXT,
    container  TEXT
);
CREATE INDEX IF NOT EXISTS nodes_kind ON nodes(kind);
CREATE INDEX IF NOT EXISTS nodes_qualname ON nodes(qualname);
CREATE INDEX IF NOT EXISTS nodes_path ON nodes(path);

-- Relationships with provenance: every edge points at the file/lines that prove it.
CREATE TABLE IF NOT EXISTS edges (
    id         INTEGER PRIMARY KEY,
    src        INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    dst        INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    rel        TEXT NOT NULL,
    path       TEXT,
    line_start INTEGER,
    line_end   INTEGER,
    origin     TEXT NOT NULL,
    confidence REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS edges_src ON edges(src, rel);
CREATE INDEX IF NOT EXISTS edges_dst ON edges(dst, rel);

CREATE TABLE IF NOT EXISTS manifests (
    path TEXT PRIMARY KEY,
    kind TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(name, words, qualname, path);
