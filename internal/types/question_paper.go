package types

// MaxQuestionPaperQuestions 限制单次出卷大小，避免大量题目占用过多内存。
const MaxQuestionPaperQuestions = 1000

type QuestionPaperSectionRequest struct {
	QuestionType string `json:"question_type"`
	Count        int    `json:"count"`
}

type QuestionPaperRequest struct {
	Sections       []QuestionPaperSectionRequest `json:"sections"`
	IncludeAnswers bool                          `json:"include_answers"`
}

type QuestionPaperOptions struct {
	Counts       map[string]int64 `json:"counts"`
	MaxQuestions int              `json:"max_questions"`
}

// QuestionPaperSection 按配置顺序保留本次抽中的题目快照，供试题与答案共用。
type QuestionPaperSection struct {
	QuestionType string
	Questions    []*Question
}

func QuestionTypeLabel(kind string) string {
	switch kind {
	case QuestionSingle:
		return "单选题"
	case QuestionMultiple:
		return "多选题"
	case QuestionJudgment:
		return "判断题"
	case QuestionFill:
		return "填空题"
	default:
		return kind
	}
}
