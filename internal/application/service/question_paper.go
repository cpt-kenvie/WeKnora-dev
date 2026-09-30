package service

import (
	"context"
	"fmt"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/questionpaper"
	"github.com/Tencent/WeKnora/internal/types"
)

func (s *QuestionBankService) PaperOptions(ctx context.Context, kbID string) (*types.QuestionPaperOptions, error) {
	kb, err := s.bank(ctx, kbID)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.PaperCounts(ctx, kb.TenantID, kb.ID)
	if err != nil {
		return nil, err
	}
	return &types.QuestionPaperOptions{Counts: counts, MaxQuestions: types.MaxQuestionPaperQuestions}, nil
}

func validateQuestionPaper(request types.QuestionPaperRequest) (map[string]int, error) {
	if len(request.Sections) == 0 || len(request.Sections) > types.MaxQuestionPaperQuestions {
		return nil, apperrors.NewBadRequestError("请添加题型并设置出题数量")
	}
	counts := make(map[string]int)
	total := 0
	for _, section := range request.Sections {
		if !types.IsQuestionType(section.QuestionType) {
			return nil, apperrors.NewBadRequestError("题型不合法")
		}
		if section.Count < 1 || section.Count > types.MaxQuestionPaperQuestions-total {
			return nil, apperrors.NewBadRequestError(fmt.Sprintf("每行数量必须为正整数，一份试卷最多 %d 道题", types.MaxQuestionPaperQuestions))
		}
		total += section.Count
		counts[section.QuestionType] += section.Count
	}
	return counts, nil
}

func (s *QuestionBankService) ExportPaper(ctx context.Context, kbID string, request types.QuestionPaperRequest) ([]byte, string, error) {
	counts, err := validateQuestionPaper(request)
	if err != nil {
		return nil, "", err
	}
	kb, err := s.bank(ctx, kbID)
	if err != nil {
		return nil, "", err
	}
	sampled, err := s.repo.SamplePaper(ctx, kb.TenantID, kb.ID, counts)
	if err != nil {
		return nil, "", err
	}
	sections := make([]types.QuestionPaperSection, 0, len(request.Sections))
	// 先核对同一题型的合计数量，任何一项不足都不输出部分试卷。
	for _, section := range request.Sections {
		available := len(sampled[section.QuestionType])
		if available < counts[section.QuestionType] {
			return nil, "", apperrors.NewBadRequestError(fmt.Sprintf("%s已核对题目不足，需要 %d 道，当前可用 %d 道", types.QuestionTypeLabel(section.QuestionType), counts[section.QuestionType], available))
		}
	}
	for _, section := range request.Sections {
		questions := sampled[section.QuestionType]
		sections = append(sections, types.QuestionPaperSection{QuestionType: section.QuestionType, Questions: questions[:section.Count]})
		sampled[section.QuestionType] = questions[section.Count:]
	}
	data, err := questionpaper.Build(kb.Name+" 试卷", sections, request.IncludeAnswers)
	if err != nil {
		return nil, "", err
	}
	name := kb.Name + "-试卷"
	if request.IncludeAnswers {
		name += "-含答案"
	}
	return data, name + ".docx", nil
}
