package chatpipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type questionInputChat struct {
	*openStreamChat
	messages []chat.Message
	response string
	err      error
	options  *chat.ChatOptions
}

func TestQuestionImagePrefersConfiguredVLMAndEmitsTitleInput(t *testing.T) {
	model := &questionInputChat{response: `{"rewrite_query":"火灾时能否使用电梯疏散？"}`}
	models := &questionInputModels{model: model}
	p := &PluginQueryUnderstand{modelService: models, config: &config.Config{}}
	bus := event.NewEventBus()
	var titleInput string
	bus.On(event.EventQueryRewritten, func(_ context.Context, evt event.Event) error {
		titleInput = evt.Data.(event.QueryData).RewrittenQuery
		return nil
	})
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		ChatModelID: "answer", ChatModelSupportsVision: true,
		VLMModelID: "configured-vision", Images: []string{"image"},
	}, PipelineContext: types.PipelineContext{EventBus: bus.AsEventBusInterface()}, QuestionBankOnly: true}
	require.Nil(t, p.OnEvent(context.Background(), types.QUERY_UNDERSTAND, cm, func() *PluginError { return nil }))
	require.Equal(t, "configured-vision", models.id)
	require.JSONEq(t, questionQueryFormat, string(model.options.Format))
	require.Equal(t, "火灾时能否使用电梯疏散？", titleInput)
}

func (m *questionInputChat) Chat(_ context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	m.messages = messages
	m.options = options
	return &types.ChatResponse{Content: m.response}, m.err
}

type questionInputModels struct {
	interfaces.ModelService
	model  chat.Chat
	id     string
	tenant uint64
}

func (m *questionInputModels) GetChatModel(ctx context.Context, id string) (chat.Chat, error) {
	m.id = id
	m.tenant, _ = types.TenantIDFromContext(ctx)
	return m.model, nil
}

func TestQuestionInputUsesScreenshotBeforeRetrieval(t *testing.T) {
	for _, query := range []string{"回答", ""} {
		t.Run(query, func(t *testing.T) {
			model := &questionInputChat{response: `{"rewrite_query":"遇到火灾爆炸、地震等突发情况时，关于电梯使用的规定包括（）。\nA. 不能使用扶梯或直梯组织旅客应急疏散\nB. 立即关闭电梯\nC. 入口防护\nD. 防止旅客通过电梯疏散","intent":"image_only"}`}
			models := &questionInputModels{model: model}
			p := &PluginQueryUnderstand{modelService: models, config: &config.Config{}}
			cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: query, Images: []string{"resource://original"}, VLMModelID: "vision", VLMModelTenantID: 42}, QuestionBankOnly: true}
			continued := false
			require.True(t, ShouldEmitQueryUnderstandProgress(cm))
			require.Nil(t, p.OnEvent(context.Background(), types.QUERY_UNDERSTAND, cm, func() *PluginError { continued = true; return nil }))
			require.True(t, continued)
			require.Equal(t, "vision", models.id)
			require.Equal(t, uint64(42), models.tenant)
			require.Equal(t, []string{"resource://original"}, model.messages[1].Images)
			require.Contains(t, cm.RewriteQuery, "不能使用扶梯")
			require.Equal(t, cm.RewriteQuery, cm.ImageDescription)
			require.Equal(t, types.IntentKBSearch, cm.Intent)
		})
	}
}

func TestQuestionInputReusesParsedImageText(t *testing.T) {
	model := &questionInputChat{response: `{"rewrite_query":"不属于应急疏散措施的是（）。 A. 使用电梯 B. 走楼梯"}`}
	models := &questionInputModels{model: model}
	p := &PluginQueryUnderstand{modelService: models, config: &config.Config{}}
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		Query: "回答", ChatModelID: "text", Images: []string{"resource://original"},
		Attachments: types.MessageAttachments{{URL: "resource://original", FileType: ".png", Content: "![原图](resource://original)\n不属于应急疏散措施的是（）。 A. 使用电梯 B. 走楼梯"}},
	}, QuestionBankOnly: true}
	require.Nil(t, p.OnEvent(context.Background(), types.QUERY_UNDERSTAND, cm, func() *PluginError { return nil }))
	require.Equal(t, "text", models.id)
	require.Empty(t, model.messages[1].Images)
	require.Contains(t, model.messages[1].Content, "不属于应急疏散")
	require.Contains(t, cm.RewriteQuery, "不属于")
}

func TestQuestionInputStopsWhenRecognitionFails(t *testing.T) {
	for _, test := range []struct {
		name, vlm, output string
		err               error
	}{
		{name: "no vision"},
		{name: "model unavailable", vlm: "vision", err: errors.New("unavailable")},
		{name: "unreadable", vlm: "vision", output: `{"rewrite_query":""}`},
		{name: "truncated", vlm: "vision", output: `{"rewrite_query":"题干","image_description":"半截`},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := &questionInputChat{response: test.output, err: test.err}
			p := &PluginQueryUnderstand{modelService: &questionInputModels{model: model}, config: &config.Config{}}
			cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "回答", Images: []string{"image"}, VLMModelID: test.vlm}, QuestionBankOnly: true}
			err := p.OnEvent(context.Background(), types.QUERY_UNDERSTAND, cm, func() *PluginError { t.Fatal("识别失败后不应开始检索"); return nil })
			require.NotNil(t, err)
			require.Equal(t, "question_input_failed", err.ErrorType)
		})
	}
}

func TestQuestionInputKeepsUnparsedImagesInMixedAttachments(t *testing.T) {
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		Images: []string{"parsed", "unparsed", "inline"},
		Attachments: types.MessageAttachments{
			{URL: "parsed", FileType: ".png", Content: "第一题：1+1=？"},
			{URL: "unparsed", FileType: ".png", Content: "![图](unparsed)"},
		},
	}}
	require.Equal(t, []string{"unparsed", "inline"}, questionImagesToAnalyze(cm))
}

func TestQuestionInputLeavesTextOnlyQuestionUnchanged(t *testing.T) {
	p := &PluginQueryUnderstand{}
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "火灾时能否使用电梯？"}, QuestionBankOnly: true}
	require.False(t, ShouldEmitQueryUnderstandProgress(cm))
	require.Nil(t, p.OnEvent(context.Background(), types.QUERY_UNDERSTAND, cm, func() *PluginError { return nil }))
	require.Equal(t, cm.Query, cm.RewriteQuery)
}
