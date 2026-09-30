package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// questionIndexSource 与识别入库采用相同锁顺序，禁止已取消或删除的原图继续发布索引。
func questionIndexSource(tx *gorm.DB, q *types.Question) (*types.Knowledge, error) {
	var source types.Knowledge
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ? AND knowledge_base_id = ? AND file_path = ?", q.KnowledgeID, q.TenantID, q.KnowledgeBaseID, q.ImageRef).
		Where("parse_status NOT IN ?", []string{types.ParseStatusCancelled, types.ParseStatusDeleting}).First(&source).Error
	return &source, err
}

// ReserveIndexStorage 在模型写入前预留额度；相同版本重试只结算差值，不重复累计。
// 失败可能留下部分索引，因此保留预留量，后续重试、停用或删除会再次结算。
func (r *QuestionRepository) ReserveIndexStorage(ctx context.Context, q *types.Question, size int64) error {
	if size < 0 {
		return fmt.Errorf("题目索引大小不合法")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		source, err := questionIndexSource(tx, q)
		if err != nil {
			return err
		}
		var stored types.Question
		if err := questionScope(tx.Clauses(clause.Locking{Strength: "UPDATE"}), q.TenantID, q.KnowledgeBaseID).
			Where("questions.id = ? AND revision = ? AND chunk_id = ? AND index_status = ?", q.ID, q.Revision, q.ChunkID, "processing").First(&stored).Error; err != nil {
			return err
		}
		if err := adjustQuestionStorage(tx, source, size-stored.StorageSize); err != nil {
			return err
		}
		if err := tx.Model(&stored).UpdateColumn("storage_size", size).Error; err != nil {
			return err
		}
		q.StorageSize = size
		return nil
	})
}

func adjustQuestionStorage(tx *gorm.DB, source *types.Knowledge, delta int64) error {
	if delta == 0 {
		return nil
	}
	var tenant types.Tenant
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&tenant, source.TenantID).Error; err != nil {
		return err
	}
	if delta > 0 && tenant.StorageQuota > 0 && tenant.StorageUsed+delta > tenant.StorageQuota {
		return fmt.Errorf("存储空间不足，请释放空间后重试索引")
	}
	if err := tx.Model(&types.Tenant{}).Where("id = ?", tenant.ID).
		UpdateColumn("storage_used", max(int64(0), tenant.StorageUsed+delta)).Error; err != nil {
		return err
	}
	return tx.Model(source).UpdateColumn("storage_size", max(int64(0), source.StorageSize+delta)).Error
}

// UpdateSourceState 只写任务状态，避免慢识别任务用旧对象覆盖并发更新的存储用量。
func (r *QuestionRepository) UpdateSourceState(ctx context.Context, source *types.Knowledge, attempt int) error {
	query := r.db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("id = ? AND tenant_id = ? AND file_path = ?", source.ID, source.TenantID, source.FilePath).
		Where("parse_status NOT IN ?", []string{types.ParseStatusCancelled, types.ParseStatusDeleting})
	if attempt > 0 {
		query = query.Where("NOT EXISTS (SELECT 1 FROM knowledge_processing_spans WHERE knowledge_id = ? AND attempt > ?)", source.ID, attempt)
	}
	updated := query.Updates(map[string]interface{}{
		"parse_status": source.ParseStatus, "enable_status": source.EnableStatus,
		"error_message": source.ErrorMessage, "processed_at": source.ProcessedAt,
		"updated_at": time.Now(), "pending_subtasks_count": 0,
	})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrQuestionConflict
	}
	return nil
}
