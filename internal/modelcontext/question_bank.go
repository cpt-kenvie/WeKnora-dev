package modelcontext

import (
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/types"
)

// questionSnapshotValue 同时支持内置工具的类型化快照与经过 JSON 恢复的工具结果。
func questionSnapshotValue(value interface{}) *types.QuestionSnapshot {
	if snapshot, ok := value.(*types.QuestionSnapshot); ok {
		return snapshot
	}
	if row, ok := value.(map[string]interface{}); ok {
		data, err := json.Marshal(row)
		if err != nil {
			return nil
		}
		var snapshot types.QuestionSnapshot
		if json.Unmarshal(data, &snapshot) == nil && snapshot.ID != "" {
			return &snapshot
		}
	}
	return nil
}
