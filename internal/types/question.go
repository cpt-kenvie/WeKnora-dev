package types

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
)

const (
	KnowledgeBaseTypeQuestionBank = "question_bank"
	ChunkTypeQuestion             = "question"
	QuestionSingle                = "single_choice"
	QuestionMultiple              = "multiple_choice"
	QuestionJudgment              = "true_false"
	QuestionFill                  = "fill_blank"
	QuestionReady                 = "ready"
	QuestionNeedsReview           = "needs_review"
)

// QuestionOption 保留原图中的选项标识、原文和顺序。
type QuestionOption struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// QuestionAnswer 按题型使用对应字段；判断值用指针区分“错误”和“未识别”。
type QuestionAnswer struct {
	OptionKeys []string `json:"option_keys,omitempty"`
	Truth      *bool    `json:"truth,omitempty"`
	Blanks     []string `json:"blanks,omitempty"`
}

// QuestionContent 是识别、编辑和问答共用的题目协议。
type QuestionContent struct {
	QuestionType string           `json:"question_type" gorm:"type:varchar(32);not null"`
	Stem         string           `json:"stem" gorm:"type:text;not null"`
	Options      []QuestionOption `json:"options" gorm:"serializer:json;type:json;not null"`
	Answer       QuestionAnswer   `json:"answer" gorm:"serializer:json;type:json;not null"`
	AnswerRaw    string           `json:"answer_raw" gorm:"type:text;not null"`
	BlankCount   int              `json:"blank_count" gorm:"not null;default:0"`
}

// Question 保存权威题目数据；同名分块仅作为可重新生成的检索投影。
type Question struct {
	ID              string `json:"id" gorm:"type:varchar(36);primaryKey"`
	ChunkID         string `json:"-" gorm:"type:varchar(36);not null"`
	PreviousChunkID string `json:"-" gorm:"-"`
	TenantID        uint64 `json:"tenant_id" gorm:"not null;index"`
	KnowledgeBaseID string `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index"`
	KnowledgeID     string `json:"knowledge_id" gorm:"type:varchar(36);not null;index"`
	SourceIndex     int    `json:"source_index" gorm:"not null"`
	ImageRef        string `json:"image_ref" gorm:"type:text;not null"`
	QuestionContent `gorm:"embedded"`
	Fingerprint     string   `json:"fingerprint" gorm:"type:varchar(64);not null;index"`
	ReviewStatus    string   `json:"review_status" gorm:"type:varchar(24);not null;index"`
	Issues          []string `json:"issues" gorm:"serializer:json;type:json;not null"`
	IsEnabled       bool     `json:"is_enabled" gorm:"not null"`
	Revision        int      `json:"revision" gorm:"not null"`
	ManuallyEdited  bool     `json:"manually_edited" gorm:"not null;default:false"`
	IndexStatus     string   `json:"index_status" gorm:"type:varchar(16);not null"`
	IndexError      string   `json:"index_error" gorm:"type:text;not null"`
	// 索引失败可能已写入部分数据，预留额度在重试、停用或删除时结算。
	StorageSize int64          `json:"-" gorm:"not null;default:0"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

// QuestionSnapshot 随引用保存在聊天记录中，后续修改题库不会改写历史答案。
type QuestionSnapshot struct {
	ID              string `json:"id"`
	Revision        int    `json:"revision"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	KnowledgeID     string `json:"knowledge_id"`
	ImageRef        string `json:"image_ref"`
	QuestionContent
}

func (q *Question) Snapshot() QuestionSnapshot {
	return QuestionSnapshot{ID: q.ID, Revision: q.Revision, KnowledgeBaseID: q.KnowledgeBaseID,
		KnowledgeID: q.KnowledgeID, ImageRef: q.ImageRef, QuestionContent: q.QuestionContent}
}

func IsQuestionType(kind string) bool {
	return kind == QuestionSingle || kind == QuestionMultiple || kind == QuestionJudgment || kind == QuestionFill
}

// NormalizeQuestionStem 仅移除排版差异，保留“不、除外”等会改变题意的文字。
func NormalizeQuestionStem(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		// 仅统一全角排版，保留小数点、正负号、空位和括号等有语义的符号。
		if r >= '！' && r <= '～' {
			r -= 0xfee0
		}
		return unicode.ToLower(r)
	}, s)
}

func (q *QuestionContent) Normalize() {
	q.Stem = strings.TrimSpace(q.Stem)
	q.AnswerRaw = strings.TrimSpace(q.AnswerRaw)
	if q.Options == nil {
		q.Options = []QuestionOption{}
	}
	for i := range q.Options {
		q.Options[i].Key = strings.ToUpper(strings.TrimSpace(q.Options[i].Key))
		q.Options[i].Text = strings.TrimSpace(q.Options[i].Text)
	}
	for i := range q.Answer.OptionKeys {
		q.Answer.OptionKeys[i] = strings.ToUpper(strings.TrimSpace(q.Answer.OptionKeys[i]))
	}
	for i := range q.Answer.Blanks {
		q.Answer.Blanks[i] = strings.TrimSpace(q.Answer.Blanks[i])
	}
}

func judgmentValue(s string) *bool {
	switch strings.TrimSpace(s) {
	case "对", "正确", "是", "√", "true":
		v := true
		return &v
	case "错", "错误", "否", "×", "false":
		v := false
		return &v
	default:
		return nil
	}
}

// ValidationIssues 不推测缺失答案，也不使用固定的 A/B 判断映射。
func (q QuestionContent) ValidationIssues() []string {
	issues := []string{}
	if !IsQuestionType(q.QuestionType) {
		issues = append(issues, "无法确认题型")
	}
	if q.Stem == "" {
		issues = append(issues, "题干为空")
	}
	if len([]rune(q.Stem)) > 20000 {
		issues = append(issues, "题干过长")
	}
	if len(q.Options) > 26 {
		issues = append(issues, "选项超过 26 个")
	}
	keys := make(map[string]string, len(q.Options))
	for _, o := range q.Options {
		if o.Key == "" || o.Text == "" {
			issues = append(issues, "选项标识或内容为空")
		}
		if _, exists := keys[o.Key]; exists {
			issues = append(issues, "选项标识重复")
		}
		keys[o.Key] = o.Text
	}
	seen := make(map[string]bool)
	for _, key := range q.Answer.OptionKeys {
		if _, exists := keys[key]; !exists {
			issues = append(issues, "答案引用了不存在的选项")
		}
		if seen[key] {
			issues = append(issues, "正确选项重复")
		}
		seen[key] = true
	}
	switch q.QuestionType {
	case QuestionSingle, QuestionMultiple:
		if len(q.Options) < 2 {
			issues = append(issues, "选择题至少需要两个选项")
		}
		if len(q.Answer.OptionKeys) == 0 || (q.QuestionType == QuestionSingle && len(q.Answer.OptionKeys) != 1) {
			issues = append(issues, "正确选项数量与题型不符")
		}
		if q.Answer.Truth != nil || len(q.Answer.Blanks) != 0 {
			issues = append(issues, "选择题包含其他题型的答案")
		}
	case QuestionJudgment:
		if q.Answer.Truth == nil {
			issues = append(issues, "无法确认判断题答案")
		}
		if len(q.Answer.OptionKeys) > 1 || len(q.Answer.Blanks) != 0 {
			issues = append(issues, "判断题答案格式错误")
		}
		if len(q.Options) > 0 && len(q.Answer.OptionKeys) != 1 {
			issues = append(issues, "判断题缺少对应的正确选项")
		}
		for _, key := range q.Answer.OptionKeys {
			truth := judgmentValue(keys[key])
			if truth == nil || q.Answer.Truth == nil || *truth != *q.Answer.Truth {
				issues = append(issues, "判断值与选项原文不一致")
			}
		}
	case QuestionFill:
		if len(q.Options) > 0 || len(q.Answer.OptionKeys) > 0 || q.Answer.Truth != nil {
			issues = append(issues, "填空题包含选项或判断值")
		}
		if q.BlankCount < 1 || q.BlankCount != len(q.Answer.Blanks) {
			issues = append(issues, "空位数量与答案数量不一致")
		}
		for _, answer := range q.Answer.Blanks {
			if answer == "" {
				issues = append(issues, "存在未识别的填空答案")
			}
		}
	}
	return issues
}

// QuestionFingerprint 将题干相同的记录归组，答案或选项差异仍由用户对照确认。
func QuestionFingerprint(stem string) string {
	digest := sha256.Sum256([]byte(NormalizeQuestionStem(stem)))
	return hex.EncodeToString(digest[:])
}

func (q QuestionContent) SearchText() string {
	var b strings.Builder
	b.WriteString(q.Stem)
	for _, o := range q.Options {
		fmt.Fprintf(&b, "\n%s. %s", o.Key, o.Text)
	}
	return b.String()
}

func (q QuestionContent) AnswerText() string {
	if q.QuestionType == QuestionFill {
		return strings.Join(q.Answer.Blanks, "；")
	}
	if q.QuestionType == QuestionJudgment && q.Answer.Truth != nil {
		if *q.Answer.Truth {
			return "正确"
		}
		return "错误"
	}
	parts := []string{}
	for _, key := range q.Answer.OptionKeys {
		for _, option := range q.Options {
			if option.Key == key {
				parts = append(parts, key+"："+option.Text)
			}
		}
	}
	return strings.Join(parts, "；")
}

// QuestionFromChunk 只接受题目分块的完整快照，普通文档的同名元数据不会被解释为题目。
func QuestionFromChunk(kind string, metadata JSON) *QuestionSnapshot {
	if kind != ChunkTypeQuestion {
		return nil
	}
	var snapshot QuestionSnapshot
	if json.Unmarshal(metadata, &snapshot) != nil || snapshot.ID == "" {
		return nil
	}
	return &snapshot
}

type QuestionListFilter struct {
	Page         int    `form:"page"`
	PageSize     int    `form:"page_size"`
	Keyword      string `form:"keyword"`
	QuestionType string `form:"question_type"`
	ReviewStatus string `form:"review_status"`
	KnowledgeID  string `form:"knowledge_id"`
}

type QuestionList struct {
	Items             []*Question `json:"items"`
	Total             int64       `json:"total"`
	ProcessingSources int64       `json:"processing_sources"`
	FailedSources     int64       `json:"failed_sources"`
}

type QuestionUpdate struct {
	QuestionContent
	Revision     int    `json:"revision" binding:"required,min=1"`
	ReviewStatus string `json:"review_status"`
	IsEnabled    bool   `json:"is_enabled"`
}
