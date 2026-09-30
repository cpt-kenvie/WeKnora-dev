-- 题目是权威数据，chunks 保存可重新生成的检索投影。
CREATE TABLE questions (
    id VARCHAR(36) PRIMARY KEY,
    chunk_id VARCHAR(36) NOT NULL,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    knowledge_id VARCHAR(36) NOT NULL REFERENCES knowledges(id) ON DELETE CASCADE,
    source_index INTEGER NOT NULL,
    image_ref TEXT NOT NULL,
    question_type VARCHAR(32) NOT NULL,
    stem TEXT NOT NULL,
    options JSON NOT NULL,
    answer JSON NOT NULL,
    answer_raw TEXT NOT NULL,
    blank_count INTEGER NOT NULL DEFAULT 0,
    fingerprint VARCHAR(64) NOT NULL,
    review_status VARCHAR(24) NOT NULL,
    issues JSON NOT NULL,
    is_enabled BOOLEAN NOT NULL,
    revision INTEGER NOT NULL,
    manually_edited BOOLEAN NOT NULL DEFAULT FALSE,
    index_status VARCHAR(16) NOT NULL,
    index_error TEXT NOT NULL,
    storage_size BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    deleted_at TIMESTAMP WITH TIME ZONE,
    UNIQUE (knowledge_id, source_index)
);
CREATE INDEX idx_questions_scope ON questions (tenant_id, knowledge_base_id, review_status, question_type);
CREATE INDEX idx_questions_fingerprint ON questions (tenant_id, knowledge_base_id, fingerprint);
CREATE INDEX idx_questions_source ON questions (tenant_id, knowledge_id);
