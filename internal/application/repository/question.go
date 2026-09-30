package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrQuestionConflict = errors.New("题目已更新或正在建立索引，请刷新后重试")

// QuestionRepository 将题目与检索投影放在同一个事务中更新。
type QuestionRepository struct{ db *gorm.DB }

func NewQuestionRepository(db *gorm.DB) *QuestionRepository { return &QuestionRepository{db: db} }

// ExactCandidates 只在已授权知识库内匹配题干，保留文档和标签的检索范围。
func (r *QuestionRepository) ExactCandidates(ctx context.Context, kb *types.KnowledgeBase, params types.SearchParams) ([]*types.Question, error) {
	query := questionScope(r.db.WithContext(ctx), kb.TenantID, kb.ID).
		Where("questions.fingerprint = ? AND questions.review_status = ? AND questions.is_enabled = ? AND questions.index_status = ?",
			types.QuestionFingerprint(params.QueryText), types.QuestionReady, true, "ready").
		Where("EXISTS (SELECT 1 FROM knowledges k WHERE k.id = questions.knowledge_id AND k.parse_status = ? AND k.enable_status = ?)", types.ParseStatusCompleted, "enabled")
	if len(params.KnowledgeIDs) > 0 {
		query = query.Where("questions.knowledge_id IN ?", params.KnowledgeIDs)
	}
	if len(params.TagIDs) > 0 || len(params.ScopeTagIDs) > 0 {
		// 带标签的调用仍走现有检索范围处理，避免在这里另建一套标签授权逻辑。
		return nil, nil
	}
	var questions []*types.Question
	err := query.Order("questions.id").Limit(20).Find(&questions).Error
	return questions, err
}

func questionScope(db *gorm.DB, tenant uint64, kbID string) *gorm.DB {
	return db.Model(&types.Question{}).
		Where("questions.tenant_id = ? AND questions.knowledge_base_id = ?", tenant, kbID).
		Where("EXISTS (SELECT 1 FROM knowledges k JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id WHERE k.id = questions.knowledge_id AND k.tenant_id = questions.tenant_id AND k.knowledge_base_id = questions.knowledge_base_id AND k.deleted_at IS NULL AND kb.deleted_at IS NULL)")
}

func (r *QuestionRepository) Get(ctx context.Context, tenant uint64, kbID, id string) (*types.Question, error) {
	var q types.Question
	err := questionScope(r.db.WithContext(ctx), tenant, kbID).Where("questions.id = ?", id).First(&q).Error
	return &q, err
}

func (r *QuestionRepository) List(ctx context.Context, tenant uint64, kbID string, f types.QuestionListFilter) (*types.QuestionList, error) {
	query := questionScope(r.db.WithContext(ctx), tenant, kbID)
	if f.Keyword != "" {
		query = query.Where("LOWER(questions.stem) LIKE LOWER(?)", "%"+f.Keyword+"%")
	}
	if f.QuestionType != "" {
		query = query.Where("questions.question_type = ?", f.QuestionType)
	}
	if f.ReviewStatus != "" {
		query = query.Where("questions.review_status = ?", f.ReviewStatus)
	}
	if f.KnowledgeID != "" {
		query = query.Where("questions.knowledge_id = ?", f.KnowledgeID)
	}
	result := &types.QuestionList{Items: []*types.Question{}}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	sources := r.db.WithContext(ctx).Model(&types.Knowledge{}).Where("tenant_id = ? AND knowledge_base_id = ?", tenant, kbID)
	if err := sources.Session(&gorm.Session{}).Where("parse_status IN ?", []string{types.ParseStatusPending, types.ParseStatusProcessing, types.ParseStatusFinalizing}).Count(&result.ProcessingSources).Error; err != nil {
		return nil, err
	}
	if err := sources.Session(&gorm.Session{}).Where("parse_status = ?", types.ParseStatusFailed).Count(&result.FailedSources).Error; err != nil {
		return nil, err
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 20
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}
	err := query.Order("questions.created_at DESC, questions.id ASC").Limit(f.PageSize).Offset((f.Page - 1) * f.PageSize).Find(&result.Items).Error
	return result, err
}

// SaveExtracted 以原图和题目序号实现幂等；人工修正结果不会被重新识别覆盖。
func (r *QuestionRepository) SaveExtracted(ctx context.Context, source *types.Knowledge, questions []*types.Question, attempt int) ([]*types.Question, error) {
	result := []*types.Question{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.Knowledge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", source.ID, source.TenantID).First(&current).Error; err != nil {
			return err
		}
		if current.FilePath != source.FilePath || current.KnowledgeBaseID != source.KnowledgeBaseID || current.ParseStatus != types.ParseStatusProcessing {
			return ErrQuestionConflict
		}
		if attempt > 0 {
			var newer int64
			if err := tx.Model(&types.KnowledgeProcessingSpan{}).Where("knowledge_id = ? AND attempt > ?", source.ID, attempt).Count(&newer).Error; err != nil {
				return err
			}
			if newer > 0 {
				return ErrQuestionConflict
			}
		}
		// 原图未变时，以原图中的题序维护身份；被人工删除的题目不因重试复活。
		for _, q := range questions {
			var existing types.Question
			err := tx.Unscoped().Where("tenant_id = ? AND knowledge_id = ? AND source_index = ?", source.TenantID, source.ID, q.SourceIndex).First(&existing).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil {
				if existing.DeletedAt.Valid {
					continue
				}
				if existing.ManuallyEdited {
					q = &existing
				}
				q.ID, q.CreatedAt, q.Revision, q.ChunkID = existing.ID, existing.CreatedAt, existing.Revision+1, existing.ChunkID
				q.StorageSize = existing.StorageSize
			} else {
				q.ID, q.CreatedAt, q.Revision = uuid.NewString(), time.Now(), 1
			}
			q.TenantID, q.KnowledgeBaseID, q.KnowledgeID = source.TenantID, source.KnowledgeBaseID, source.ID
			q.ImageRef = source.FilePath
			q.Fingerprint = types.QuestionFingerprint(q.Stem)
			var duplicateCount int64
			if err := questionScope(tx, q.TenantID, q.KnowledgeBaseID).Where("questions.fingerprint = ? AND questions.id <> ?", q.Fingerprint, q.ID).Count(&duplicateCount).Error; err != nil {
				return err
			}
			if duplicateCount > 0 && !q.ManuallyEdited {
				q.Issues = append(q.Issues, "存在同题干记录，请对照选项和答案确认是否重复或冲突")
			}
			if len(q.Issues) > 0 {
				q.ReviewStatus, q.IsEnabled = types.QuestionNeedsReview, false
			}
			q.IndexStatus, q.IndexError, q.UpdatedAt = "processing", "", time.Now()
			if err := saveQuestionProjection(tx, q); err != nil {
				return err
			}
			result = append(result, q)
		}
		// 再次识别漏掉的题目保留供核对，人工内容继续可用，不静默丢失。
		var missing []*types.Question
		if err := tx.Where("tenant_id = ? AND knowledge_id = ? AND source_index >= ?", source.TenantID, source.ID, len(questions)).Find(&missing).Error; err != nil {
			return err
		}
		for _, q := range missing {
			if !q.ManuallyEdited {
				q.ReviewStatus, q.IsEnabled = types.QuestionNeedsReview, false
				q.Issues = []string{"重新识别时未发现该题，请对照原图核对"}
			}
			q.Revision++
			q.IndexStatus, q.IndexError, q.UpdatedAt = "processing", "", time.Now()
			if err := saveQuestionProjection(tx, q); err != nil {
				return err
			}
			result = append(result, q)
		}
		return nil
	})
	return result, err
}

func saveQuestionProjection(tx *gorm.DB, q *types.Question) error {
	// 每个版本有独立分块身份；旧任务即便晚写向量，也无法命中新版本的答案。
	q.PreviousChunkID, q.ChunkID = q.ChunkID, uuid.NewString()
	if q.PreviousChunkID != "" {
		if err := tx.Where("id = ? AND tenant_id = ?", q.PreviousChunkID, q.TenantID).Delete(&types.Chunk{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Save(q).Error; err != nil {
		return err
	}
	snapshot, err := json.Marshal(q.Snapshot())
	if err != nil {
		return err
	}
	images, err := json.Marshal([]types.ImageInfo{{URL: q.ImageRef, OriginalURL: q.ImageRef}})
	if err != nil {
		return err
	}
	chunk := &types.Chunk{ID: q.ChunkID, TenantID: q.TenantID, KnowledgeBaseID: q.KnowledgeBaseID, KnowledgeID: q.KnowledgeID,
		ChunkIndex: q.SourceIndex, Content: q.SearchText(), SourceContent: q.SearchText(), ChunkType: types.ChunkTypeQuestion,
		Metadata: types.JSON(snapshot), ImageInfo: string(images), IsEnabled: q.IsEnabled && q.ReviewStatus == types.QuestionReady,
		ContentRevision: q.Revision, IndexStatus: q.IndexStatus, Status: int(types.ChunkStatusStored)}
	if tx.Dialector.Name() == "sqlite" {
		if err := types.AssignChunkSeqIDs(tx, []*types.Chunk{chunk}); err != nil {
			return err
		}
	}
	return tx.Create(chunk).Error
}

func (r *QuestionRepository) Update(ctx context.Context, q *types.Question, expected int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stored types.Question
		if err := questionScope(tx.Clauses(clause.Locking{Strength: "UPDATE"}), q.TenantID, q.KnowledgeBaseID).Where("questions.id = ?", q.ID).First(&stored).Error; err != nil {
			return err
		}
		if stored.Revision != expected || stored.IndexStatus == "processing" {
			return ErrQuestionConflict
		}
		q.Revision, q.IndexStatus, q.IndexError = expected+1, "processing", ""
		q.Fingerprint, q.ManuallyEdited, q.UpdatedAt = types.QuestionFingerprint(q.Stem), true, time.Now()
		return saveQuestionProjection(tx, q)
	})
}

// FinishIndex 仅公布当前版本；外部索引写入不持有数据库事务，兼容 SQLite 检索引擎。
func (r *QuestionRepository) FinishIndex(ctx context.Context, q *types.Question, indexErr error) error {
	status, detail := "ready", ""
	if indexErr != nil {
		status, detail = "failed", indexErr.Error()
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := questionIndexSource(tx, q); err != nil {
			return err
		}
		updated := questionScope(tx, q.TenantID, q.KnowledgeBaseID).Where("questions.id = ? AND revision = ? AND chunk_id = ? AND index_status = ?", q.ID, q.Revision, q.ChunkID, "processing").
			Updates(map[string]interface{}{"index_status": status, "index_error": detail})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrQuestionConflict
		}
		q.IndexStatus, q.IndexError = status, detail
		published := tx.Model(&types.Chunk{}).Where("id = ? AND tenant_id = ? AND content_revision = ?", q.ChunkID, q.TenantID, q.Revision).
			Updates(map[string]interface{}{"index_status": status, "status": int(types.ChunkStatusIndexed)})
		if published.Error != nil {
			return published.Error
		}
		if published.RowsAffected != 1 {
			return ErrQuestionConflict
		}
		if status == "ready" {
			// 显式重试修复最后一个索引后，解除原图的失败状态，恢复题库可用性。
			if err := tx.Model(&types.Knowledge{}).Where("id = ? AND tenant_id = ? AND file_path = ? AND parse_status = ?", q.KnowledgeID, q.TenantID, q.ImageRef, types.ParseStatusFailed).
				Where("NOT EXISTS (SELECT 1 FROM questions WHERE knowledge_id = ? AND deleted_at IS NULL AND index_status <> ?)", q.KnowledgeID, "ready").
				Updates(map[string]interface{}{"parse_status": types.ParseStatusCompleted, "enable_status": "enabled", "error_message": "", "processed_at": time.Now()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// RetryIndex 允许显式修复失败或进程中断留下的索引任务；版本递增使旧任务失效。
func (r *QuestionRepository) RetryIndex(ctx context.Context, q *types.Question) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if q.IndexStatus == "processing" && time.Since(q.UpdatedAt) < 10*time.Minute {
			return ErrQuestionConflict
		}
		expected := q.Revision
		q.Revision++
		q.IndexStatus, q.IndexError, q.UpdatedAt = "processing", "", time.Now()
		result := tx.Model(&types.Question{}).Where("id = ? AND tenant_id = ? AND revision = ?", q.ID, q.TenantID, expected).Update("revision", q.Revision)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrQuestionConflict
		}
		return saveQuestionProjection(tx, q)
	})
}

func (r *QuestionRepository) Delete(ctx context.Context, q *types.Question, expected int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		source, err := questionIndexSource(tx, q)
		if err != nil {
			return err
		}
		result := tx.Where("id = ? AND tenant_id = ? AND revision = ? AND index_status <> ?", q.ID, q.TenantID, expected, "processing").Delete(&types.Question{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrQuestionConflict
		}
		if err := tx.Where("id = ? AND tenant_id = ?", q.ChunkID, q.TenantID).Delete(&types.Chunk{}).Error; err != nil {
			return fmt.Errorf("delete question projection: %w", err)
		}
		return adjustQuestionStorage(tx, source, -q.StorageSize)
	})
}
