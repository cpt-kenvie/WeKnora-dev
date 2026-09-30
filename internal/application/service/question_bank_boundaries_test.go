package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestQuestionBankRejectsUnsupportedImagesAtUpload(t *testing.T) {
	kb := &types.KnowledgeBase{Type: types.KnowledgeBaseTypeQuestionBank}
	for _, kind := range []string{"pdf", "gif", "svg", "tiff", "docx"} {
		_, err := resolveFileImportProcessConfig(context.Background(), kb, kind, nil, nil)
		require.ErrorContains(t, err, "JPG、PNG、WebP、BMP")
	}
}

func TestQuestionBankReparsePreservesOriginalAndQuota(t *testing.T) {
	f := newDocumentWriteFixture(t)
	f.kbs.values["kb"].Type = types.KnowledgeBaseTypeQuestionBank
	source, err := f.repo.GetKnowledgeByID(f.ctx, 7, "doc")
	require.NoError(t, err)
	source.FilePath = "resource://original-question-image"
	source.ParseStatus = types.ParseStatusCompleted
	source.StorageSize = 123
	require.NoError(t, f.svc.cleanupKnowledgeResources(f.ctx, source))
	require.Empty(t, f.files.deleted)
	require.Empty(t, f.tenants.adjustments)
	require.Zero(t, f.chunkRepo.writes)
	require.EqualValues(t, 123, source.StorageSize)
}

func TestQuestionBankRejectsGenericChunkWrites(t *testing.T) {
	f := newDocumentWriteFixture(t)
	f.kbs.values["kb"].Type = types.KnowledgeBaseTypeQuestionBank
	require.NoError(t, f.db.Model(&types.Chunk{}).Where("id = ?", "chunk").Update("chunk_type", types.ChunkTypeQuestion).Error)
	require.ErrorContains(t, f.chunks.DeleteChunk(f.ctx, "chunk"), "题库")
	require.ErrorContains(t, f.chunks.DeleteChunksByKnowledgeID(f.ctx, "doc"), "题库")
	content := "绕过题目版本直接改写"
	_, err := f.chunks.UpdateDocumentChunk(f.ctx, "chunk", &content, nil, nil)
	require.ErrorContains(t, err, "题库")
	require.Zero(t, f.chunkRepo.writes)
}
