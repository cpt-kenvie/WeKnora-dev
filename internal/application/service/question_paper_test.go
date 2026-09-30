package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateQuestionPaperAggregatesRepeatedTypes(t *testing.T) {
	counts, err := validateQuestionPaper(types.QuestionPaperRequest{Sections: []types.QuestionPaperSectionRequest{
		{QuestionType: types.QuestionSingle, Count: 10}, {QuestionType: types.QuestionFill, Count: 5}, {QuestionType: types.QuestionSingle, Count: 7},
	}})
	require.NoError(t, err)
	require.Equal(t, map[string]int{types.QuestionSingle: 17, types.QuestionFill: 5}, counts)
}

type paperKBService struct {
	interfaces.KnowledgeBaseService
	bank *types.KnowledgeBase
	err  error
}

func (s *paperKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.bank, s.err
}

func setupPaperService(t *testing.T) (*QuestionBankService, *gorm.DB, *paperKBService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("CREATE TABLE knowledges (id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id, tenant_id) VALUES ('bank', 7)").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledges (id, tenant_id, knowledge_base_id) VALUES ('source', 7, 'bank')").Error)
	require.NoError(t, db.AutoMigrate(&types.Question{}))
	for i := 0; i < 4; i++ {
		question := &types.Question{ID: strconv.Itoa(i), TenantID: 7, KnowledgeBaseID: "bank", KnowledgeID: "source", SourceIndex: i, ReviewStatus: types.QuestionReady,
			QuestionContent: types.QuestionContent{QuestionType: types.QuestionFill, Stem: "题干" + strconv.Itoa(i), BlankCount: 1, Answer: types.QuestionAnswer{Blanks: []string{"答案" + strconv.Itoa(i)}}}}
		require.NoError(t, db.Create(question).Error)
	}
	kbs := &paperKBService{bank: &types.KnowledgeBase{ID: "bank", TenantID: 7, Type: types.KnowledgeBaseTypeQuestionBank, Name: "安全题库"}}
	return &QuestionBankService{repo: repository.NewQuestionRepository(db), kbs: kbs}, db, kbs
}

func TestExportQuestionPaperUsesOneSnapshotAcrossRepeatedRows(t *testing.T) {
	service, _, _ := setupPaperService(t)
	data, name, err := service.ExportPaper(context.Background(), "bank", types.QuestionPaperRequest{IncludeAnswers: true, Sections: []types.QuestionPaperSectionRequest{
		{QuestionType: types.QuestionFill, Count: 2}, {QuestionType: types.QuestionFill, Count: 2},
	}})
	require.NoError(t, err)
	require.Equal(t, "安全题库-试卷-含答案.docx", name)
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	reader, err := archive.Open("word/document.xml")
	require.NoError(t, err)
	defer reader.Close()
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	document := string(content)
	for i := 0; i < 4; i++ {
		require.Equal(t, 1, strings.Count(document, "题干"+strconv.Itoa(i)))
		for number := 1; number <= 4; number++ {
			if strings.Contains(document, strconv.Itoa(number)+". 题干"+strconv.Itoa(i)) {
				require.Contains(t, document, strconv.Itoa(number)+". 答案"+strconv.Itoa(i))
			}
		}
	}
}

func TestExportQuestionPaperRechecksReviewStatusAndDoesNotReturnPartialPaper(t *testing.T) {
	service, db, _ := setupPaperService(t)
	options, err := service.PaperOptions(context.Background(), "bank")
	require.NoError(t, err)
	require.EqualValues(t, 4, options.Counts[types.QuestionFill])
	require.NoError(t, db.Model(&types.Question{}).Where("id = ?", "0").Update("review_status", types.QuestionNeedsReview).Error)
	data, name, err := service.ExportPaper(context.Background(), "bank", types.QuestionPaperRequest{Sections: []types.QuestionPaperSectionRequest{
		{QuestionType: types.QuestionFill, Count: 2}, {QuestionType: types.QuestionFill, Count: 2},
	}})
	require.ErrorContains(t, err, "填空题已核对题目不足，需要 4 道，当前可用 3 道")
	require.Empty(t, data)
	require.Empty(t, name)
}

func TestQuestionPaperRequiresAccessibleQuestionBank(t *testing.T) {
	service, _, kbs := setupPaperService(t)
	kbs.err = errors.New("forbidden")
	_, err := service.PaperOptions(context.Background(), "bank")
	require.ErrorIs(t, err, kbs.err)
	_, _, err = service.ExportPaper(context.Background(), "bank", types.QuestionPaperRequest{Sections: []types.QuestionPaperSectionRequest{{QuestionType: types.QuestionFill, Count: 1}}})
	require.ErrorIs(t, err, kbs.err)
	kbs.err = nil
	kbs.bank.Type = "document"
	_, err = service.PaperOptions(context.Background(), "bank")
	require.ErrorContains(t, err, "该知识库不是题库")
}

func TestValidateQuestionPaperRejectsInvalidCountsAndTypes(t *testing.T) {
	for _, sections := range [][]types.QuestionPaperSectionRequest{
		nil,
		{{QuestionType: "essay", Count: 1}},
		{{QuestionType: types.QuestionSingle, Count: 0}},
		{{QuestionType: types.QuestionSingle, Count: -1}},
		{{QuestionType: types.QuestionSingle, Count: types.MaxQuestionPaperQuestions + 1}},
		{{QuestionType: types.QuestionSingle, Count: types.MaxQuestionPaperQuestions}, {QuestionType: types.QuestionFill, Count: 1}},
	} {
		_, err := validateQuestionPaper(types.QuestionPaperRequest{Sections: sections})
		require.Error(t, err)
	}
	_, err := validateQuestionPaper(types.QuestionPaperRequest{Sections: []types.QuestionPaperSectionRequest{{QuestionType: types.QuestionFill, Count: types.MaxQuestionPaperQuestions}}})
	require.NoError(t, err)
}
