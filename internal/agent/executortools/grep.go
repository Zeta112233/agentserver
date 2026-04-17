package executortools

import (
	"context"
	"encoding/json"
)

// grep is filled in by Task 4.
func (e *ToolExecutor) grep(ctx context.Context, rawArgs json.RawMessage) ExecuteResponse {
	return errResponse("Grep: not yet implemented")
}
