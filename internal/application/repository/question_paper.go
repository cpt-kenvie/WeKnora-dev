package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// 出卷依据核对状态读取权威题目，问答开关与检索索引状态不影响纸质试卷。
func reviewedPaperScope(db *gorm.DB, tenant uint64, kbID string) *gorm.DB {
	return questionScope(db, tenant, kbID).Where("questions.review_status = ?", types.QuestionReady)
}

func (r *QuestionRepository) PaperCounts(ctx context.Context, tenant uint64, kbID string) (map[string]int64, error) {
	var rows []struct {
		QuestionType string
		Count        int64
	}
	err := reviewedPaperScope(r.db.WithContext(ctx), tenant, kbID).
		Select("questions.question_type, COUNT(*) AS count").Group("questions.question_type").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{types.QuestionSingle: 0, types.QuestionMultiple: 0, types.QuestionJudgment: 0, types.QuestionFill: 0}
	for _, row := range rows {
		if types.IsQuestionType(row.QuestionType) {
			counts[row.QuestionType] = row.Count
		}
	}
	return counts, nil
}

// SamplePaper 在整个题库中随机抽取，不受题目列表分页或搜索条件影响。
func (r *QuestionRepository) SamplePaper(ctx context.Context, tenant uint64, kbID string, counts map[string]int) (map[string][]*types.Question, error) {
	result := make(map[string][]*types.Question, len(counts))
	selected := []string{}
	for _, kind := range []string{types.QuestionSingle, types.QuestionMultiple, types.QuestionJudgment, types.QuestionFill} {
		count := counts[kind]
		if count == 0 {
			continue
		}
		query := reviewedPaperScope(r.db.WithContext(ctx), tenant, kbID).Where("questions.question_type = ?", kind)
		// 并发编辑可能改变题型；排除本次已抽中的 ID，保证跨题型也不会重复。
		if len(selected) > 0 {
			query = query.Where("questions.id NOT IN ?", selected)
		}
		var questions []*types.Question
		// 项目支持的 PostgreSQL 与 SQLite 均使用 RANDOM()。
		if err := query.Order("RANDOM()").Limit(count).Find(&questions).Error; err != nil {
			return nil, err
		}
		result[kind] = questions
		for _, question := range questions {
			selected = append(selected, question.ID)
		}
	}
	return result, nil
}
