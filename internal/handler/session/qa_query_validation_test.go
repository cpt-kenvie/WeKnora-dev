package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type queryValidationSessionStub struct {
	interfaces.SessionService
	lookupCalls int
	session     *types.Session
}

func (s *queryValidationSessionStub) GetOwnedSession(context.Context, string) (*types.Session, error) {
	s.lookupCalls++
	if s.session != nil {
		return s.session, nil
	}
	return nil, errors.New("stop at session lookup")
}

func TestQAQueryValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name               string
		query              string
		images             []ImageAttachment
		attachmentIDs      []string
		uploads            []AttachmentUpload
		rejectBeforeLookup bool
	}{
		{name: "empty", rejectBeforeLookup: true},
		{name: "whitespace", query: " \t\r\n ", rejectBeforeLookup: true},
		{name: "unicode whitespace", query: "\u3000\u00a0", rejectBeforeLookup: true},
		{name: "null byte", query: "hello\x00world", rejectBeforeLookup: true},
		{name: "escape character", query: "hello\x1bworld", rejectBeforeLookup: true},
		{name: "script element", query: "<script>alert(1)</script>"},
		{name: "event handler", query: `<img src=x onerror="alert(1)">`},
		{name: "script URL", query: "javascript:alert(1)"},
		{name: "agent frontend snippet", query: `Why doesn't my <button onclick="save()">Save</button> fire?`},
		{name: "agent javascript snippet", query: "What does javascript:void(0) do?"},
		{name: "English", query: "What is retrieval augmented generation?"},
		{name: "Unicode", query: "请解释厄尔尼诺现象 🌊"},
		{name: "multiline", query: "Compare:\n\tGo\r\n\tPython"},
		{name: "padded text", query: "  Explain retrieval.\n"},
		{name: "image only", images: []ImageAttachment{{Data: "data:image/png;base64,aW1hZ2U="}}},
		{name: "image with whitespace", query: " \n\u3000", images: []ImageAttachment{{Data: "data:image/png;base64,aW1hZ2U="}}},
		{name: "uploaded image ID", attachmentIDs: []string{"image-1"}},
		{name: "inline image file", uploads: []AttachmentUpload{{Data: "aW1hZ2U=", FileName: "question.PNG"}}},
		{name: "empty image", images: []ImageAttachment{{}}, rejectBeforeLookup: true},
		{name: "image URL is not upload", images: []ImageAttachment{{URL: "http://example.com/image.png", Caption: "caption"}}, rejectBeforeLookup: true},
		{name: "blank image data", images: []ImageAttachment{{Data: " \t"}}, rejectBeforeLookup: true},
		{name: "blank attachment IDs", attachmentIDs: []string{"", " \u3000"}, rejectBeforeLookup: true},
		{name: "empty inline upload", uploads: []AttachmentUpload{{FileName: "question.png"}}, rejectBeforeLookup: true},
		{name: "document alone still needs query", uploads: []AttachmentUpload{{Data: "cGRm", FileName: "report.pdf"}}, rejectBeforeLookup: true},
		{name: "image does not bypass invalid query", query: "hello\x00world", images: []ImageAttachment{{Data: "data:image/png;base64,aW1hZ2U="}}, rejectBeforeLookup: true},
	}
	for _, endpoint := range []string{"knowledge-chat", "agent-chat"} {
		t.Run(endpoint, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					sessions := &queryValidationSessionStub{}
					h := &Handler{sessionService: sessions}
					router := gin.New()
					router.Use(middleware.ErrorHandler())
					router.POST("/knowledge-chat/:session_id", h.KnowledgeQA)
					router.POST("/agent-chat/:session_id", h.AgentQA)

					body, err := json.Marshal(CreateKnowledgeQARequest{
						Query: tt.query, Images: tt.images, AttachmentIDs: tt.attachmentIDs, AttachmentUploads: tt.uploads,
					})
					require.NoError(t, err)
					request := httptest.NewRequest(http.MethodPost, "/"+endpoint+"/session-1", bytes.NewReader(body))
					request.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)

					if tt.rejectBeforeLookup {
						assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
						assert.Contains(t, response.Header().Get("Content-Type"), "application/json")
						assert.Zero(t, sessions.lookupCalls, "reject the query before session lookup or QA work")
					} else {
						// Stop at a controlled not-found response: valid queries must
						// reach the ordinary session lookup without invoking a model.
						assert.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
						assert.Equal(t, 1, sessions.lookupCalls)
					}
				})
			}
		})
	}
}

type imageOnlyDocuments struct {
	interfaces.TemporaryDocumentService
	document *types.TemporaryDocument
}

func (s *imageOnlyDocuments) Get(_ context.Context, tenantID uint64, sessionID, id string) (*types.TemporaryDocument, error) {
	if s.document == nil || tenantID != 42 || sessionID != "session-1" || id != s.document.ID {
		return nil, errors.New("attachment not found in this session")
	}
	return s.document, nil
}

func TestQAImageOnlyRequestChecksSessionAttachment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name      string
		fileType  string
		missing   bool
		wantError bool
	}{
		{name: "PNG image", fileType: ".png"},
		{name: "uppercase JPEG image", fileType: ".JPG"},
		{name: "document needs text", fileType: ".pdf", wantError: true},
		{name: "missing or foreign image", missing: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions := &queryValidationSessionStub{session: &types.Session{ID: "session-1", TenantID: 42}}
			documents := &imageOnlyDocuments{}
			if !tc.missing {
				documents.document = &types.TemporaryDocument{ID: "image-1", FileType: tc.fileType, ResourceRef: "resource://original"}
			}
			h := &Handler{sessionService: sessions, temporaryDocuments: documents}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			// 省略 query，验证接口绑定和附件检查可以保留真正的纯图片请求。
			c.Request = httptest.NewRequest(http.MethodPost, "/knowledge-chat/session-1", bytes.NewBufferString(`{"attachment_ids":[" image-1 ","image-1"]}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "session_id", Value: "session-1"}}
			rc, request, err := h.parseQARequest(c, "test")
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, rc)
				return
			}
			require.NoError(t, err)
			require.Empty(t, request.Query)
			require.Empty(t, rc.query)
			require.Equal(t, []string{"image-1"}, rc.attachmentIDs)
			require.Len(t, rc.attachmentMetas, 1)
			require.Equal(t, "resource://original", rc.attachmentMetas[0].URL)
		})
	}
}
