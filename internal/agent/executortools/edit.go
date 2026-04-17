package executortools

import (
	"context"
	"encoding/json"
)

// edit is filled in by Task 4.
func (e *ToolExecutor) edit(ctx context.Context, rawArgs json.RawMessage) ExecuteResponse {
	return errResponse("Edit: not yet implemented")
}
