package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func decodeQuestionWithStem(t *testing.T, stem string) *types.Question {
	t.Helper()
	content := types.QuestionContent{
		QuestionType: types.QuestionFill, Stem: stem, BlankCount: 1,
		Answer: types.QuestionAnswer{Blanks: []string{"有效乘车凭证"}}, AnswerRaw: "有效乘车凭证",
	}
	raw, err := json.Marshal(map[string]interface{}{"questions": []types.QuestionContent{content}})
	require.NoError(t, err)
	questions, err := decodeQuestionExtraction(string(raw))
	require.NoError(t, err)
	require.Len(t, questions, 1)
	return questions[0]
}

func TestQuestionExtractionRemovesPageNumberAndScore(t *testing.T) {
	const stem = "旅客应当持有____。"
	for _, input := range []string{
		"1. " + stem,
		"2、" + stem,
		"３．" + stem,
		"（4）" + stem,
		"(5) " + stem,
		"六、" + stem,
		"⑦" + stem,
		"第8题：" + stem,
		"题号：9\n" + stem,
		"【第10题】" + stem,
		"[题号：11]" + stem,
		"12、（2分）" + stem,
		"(2.5分) 13. " + stem + "（本题共2.5分）",
		"14、" + stem + "[分值：2分]",
		"15、" + stem + "【共2分，每空1分】",
		"分值：2分\n16、" + stem + "\n得分：0分",
		"17、" + stem + "\n本题2分\n",
		"18、\u3000（２．５分）\u3000" + stem,
	} {
		t.Run(input, func(t *testing.T) {
			q := decodeQuestionWithStem(t, input)
			require.Equal(t, stem, q.Stem)
			require.Equal(t, stem, q.SearchText(), "评分信息不能进入检索分块")
			require.Equal(t, stem, q.Snapshot().Stem)
			require.Equal(t, types.QuestionReady, q.ReviewStatus)
			require.Empty(t, q.Issues)
		})
	}
	q := decodeQuestionWithStem(t, "19、【单选题】（2分）"+stem)
	require.Equal(t, "【单选题】"+stem, q.Stem)
}

func TestQuestionExtractionPreservesNumbersInContent(t *testing.T) {
	for _, stem := range []string{
		"3.14约等于____。",
		"３．１４约等于____。",
		"1/2等于____。",
		"1、2、3中最大的数是____。",
		"一、二、三分别表示____。",
		"1:2的比值为____。",
		"１：２的比值为____。",
		"2026.10.01是____。",
		"2026年10月1日是____。",
		"第12条规定旅客应当____。",
		"第1题的答案与____相同。",
		"（1）+（2）=____。",
		"（1）求____；（2）说明理由。",
		"( 1 )求____；( 2 )说明理由。",
		"①求____；②说明理由。",
		"⑴求____；⑵说明理由。",
		"1. 求____。\n2. 说明理由。",
		"一、求____。\n二、说明理由。",
		"比赛得了2分，累计____分。",
		"经过2分钟后到达____。",
		"计算下列分数：1/2、2/3，结果为____。",
		"条件如下：\n1. 持有车票\n2. 实名登记\n应当____。",
		"评分标准规定每题2分，共____分。",
	} {
		t.Run(stem, func(t *testing.T) {
			q := decodeQuestionWithStem(t, stem)
			require.Equal(t, stem, q.Stem)
		})
	}
	q := decodeQuestionWithStem(t, "8、（1）求____；（2）说明理由。（2分）")
	require.Equal(t, "（1）求____；（2）说明理由。", q.Stem, "仅去掉外层题号，保留子问题编号")
	q = decodeQuestionWithStem(t, "9. 3.14约等于____。（2分）")
	require.Equal(t, "3.14约等于____。", q.Stem)
	q = decodeQuestionWithStem(t, "第10题：3.14约等于____。（2分）")
	require.Equal(t, "3.14约等于____。", q.Stem)
}

func TestQuestionExtractionCleansScoreBeforeValidation(t *testing.T) {
	raw := `{"questions":[
		{"question_type":"true_false","stem":"1、必须实名乘车。（2分）","options":[{"key":"A","text":"对（2分）"},{"key":"B","text":"错"}],"answer":{"option_keys":["A"],"truth":true},"answer_raw":"A（2分）"},
		{"question_type":"fill_blank","stem":"2、经过____后到达。","blank_count":1,"answer":{"blanks":["2分钟（每空1分）"]},"answer_raw":"2分钟（1分）"},
		{"question_type":"single_choice","stem":"3、比分为多少？","options":[{"key":"A","text":"（2分）"},{"key":"B","text":"3分"}],"answer":{"option_keys":["A"]},"answer_raw":"A"}
	]}`
	questions, err := decodeQuestionExtraction(raw)
	require.NoError(t, err)
	for i, q := range questions {
		require.Equal(t, types.QuestionReady, q.ReviewStatus)
		require.Empty(t, q.Issues)
		require.Equal(t, i, q.SourceIndex, "保留定位原图所需的内部顺序")
	}
	require.Equal(t, "对", questions[0].Options[0].Text)
	require.Equal(t, "A", questions[0].AnswerRaw)
	require.Equal(t, []string{"2分钟"}, questions[1].Answer.Blanks)
	require.Equal(t, "2分钟", questions[1].AnswerRaw)
	require.Equal(t, "（2分）", questions[2].Options[0].Text)
	q := decodeQuestionWithStem(t, "4、（2分）")
	require.Empty(t, q.Stem)
	require.Equal(t, types.QuestionNeedsReview, q.ReviewStatus)
	require.False(t, q.IsEnabled)
}

func TestQuestionExtractionStoresCleanContentAndDuplicateFingerprint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id, tenant_id) VALUES ('bank', 1)").Error)
	require.NoError(t, db.AutoMigrate(&types.Question{}, &types.Knowledge{}, &types.Chunk{}))
	source := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "bank", FilePath: "resource://original", ParseStatus: types.ParseStatusProcessing}
	require.NoError(t, db.Create(source).Error)
	const stem = "旅客应当持有____。"
	first := decodeQuestionWithStem(t, "1、（2分）"+stem)
	second := decodeQuestionWithStem(t, "第20题："+stem+"（3分）")
	second.SourceIndex = 1
	saved, err := repository.NewQuestionRepository(db).SaveExtracted(context.Background(), source, []*types.Question{first, second}, 0)
	require.NoError(t, err)
	require.Len(t, saved, 2)
	for _, q := range saved {
		var stored types.Question
		require.NoError(t, db.First(&stored, "id = ?", q.ID).Error)
		require.Equal(t, stem, stored.Stem)
		require.Equal(t, types.QuestionFingerprint(stem), stored.Fingerprint)
		var chunk types.Chunk
		require.NoError(t, db.First(&chunk, "id = ?", q.ChunkID).Error)
		require.Equal(t, stem, chunk.Content)
		require.Equal(t, stem, chunk.SourceContent)
		var snapshot types.QuestionSnapshot
		require.NoError(t, json.Unmarshal(chunk.Metadata, &snapshot))
		require.Equal(t, stem, snapshot.Stem)
	}
	require.Equal(t, types.QuestionReady, saved[0].ReviewStatus)
	require.Equal(t, types.QuestionNeedsReview, saved[1].ReviewStatus, "同一道题不能因题号或分值不同而绕过重复检查")
}
