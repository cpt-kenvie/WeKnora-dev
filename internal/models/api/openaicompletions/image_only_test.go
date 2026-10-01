package openaicompletions

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/stretchr/testify/require"
)

func TestImageOnlyRequestKeepsRequiredTextField(t *testing.T) {
	for _, message := range []api.Message{
		{Role: "user", Images: []string{"https://example.com/question.png"}},
		{Role: "user", MultiContent: []api.MessageContentPart{
			{Type: "text"}, {Type: "image_url", ImageURL: &api.ImageURL{URL: "https://example.com/question.png"}},
		}},
	} {
		for _, streaming := range []bool{false, true} {
			body := bodyJSON(t, newClient(t, nil), []api.Message{message}, nil, streaming)
			parts := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
			require.Len(t, parts, 2)
			for _, value := range parts {
				part := value.(map[string]any)
				if part["type"] == "text" {
					require.Contains(t, part, "text", "空文本片段也必须符合接口协议")
					require.Equal(t, "", part["text"])
				} else {
					require.NotContains(t, part, "text")
				}
			}
		}
	}
}
