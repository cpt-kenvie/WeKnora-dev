-- 与 PostgreSQL 共用题目协议，JSON 由应用层进行强类型校验。
CREATE TABLE questions (
    id TEXT PRIMARY KEY,
    chunk_id TEXT NOT NULL,
    tenant_id INTEGER NOT NULL,
    knowledge_base_id TEXT NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    knowledge_id TEXT NOT NULL REFERENCES knowledges(id) ON DELETE CASCADE,
    source_index INTEGER NOT NULL,
    image_ref TEXT NOT NULL,
    question_type TEXT NOT NULL,
    stem TEXT NOT NULL,
    options TEXT NOT NULL,
    answer TEXT NOT NULL,
    answer_raw TEXT NOT NULL,
    blank_count INTEGER NOT NULL DEFAULT 0,
    fingerprint TEXT NOT NULL,
    review_status TEXT NOT NULL,
    issues TEXT NOT NULL,
    is_enabled BOOLEAN NOT NULL,
    revision INTEGER NOT NULL,
    manually_edited BOOLEAN NOT NULL DEFAULT FALSE,
    index_status TEXT NOT NULL,
    index_error TEXT NOT NULL,
    storage_size BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    deleted_at DATETIME,
    UNIQUE (knowledge_id, source_index)
);
CREATE INDEX idx_questions_scope ON questions (tenant_id, knowledge_base_id, review_status, question_type);
CREATE INDEX idx_questions_fingerprint ON questions (tenant_id, knowledge_base_id, fingerprint);
CREATE INDEX idx_questions_source ON questions (tenant_id, knowledge_id);
