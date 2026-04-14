// pkg/governance/registry.go
package governance

import (
	"errors"
	"fmt"
	"github.com/tidwall/gjson"
)

// TargetPathRegistry maps deterministic tool names to their JSON path containing the target file.
var TargetPathRegistry = map[string]string{
	"write_file":      "params.arguments.path",
	"replace_in_file": "params.arguments.path",
	"edit_file":       "params.arguments.target",
}

// ExtractTarget parses the JSON-RPC payload and returns the file path the agent intends to modify.
func ExtractTarget(toolName string, payload []byte) (string, error) {
	jsonPath, exists := TargetPathRegistry[toolName]
	if !exists {
		return "", fmt.Errorf("tool '%s' is not registered for deterministic OS locking", toolName)
	}

	result := gjson.GetBytes(payload, jsonPath)
	if !result.Exists() || result.Type != gjson.String {
		return "", errors.New("target path not found in payload or invalid format")
	}

	return result.String(), nil
}