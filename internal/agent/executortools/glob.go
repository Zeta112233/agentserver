package executortools

import (
	"context"
	"encoding/json"
)

// glob is filled in by Task 4.
func (e *ToolExecutor) glob(ctx context.Context, rawArgs json.RawMessage) ExecuteResponse {
	return errResponse("Glob: not yet implemented")
}
