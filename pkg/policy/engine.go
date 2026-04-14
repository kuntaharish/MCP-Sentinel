// pkg/policy/engine.go
package policy

import (
	"github.com/tidwall/gjson"
)

// RouteType defines how the governance engine handles the tool.
type RouteType string

const (
	RouteDeterministic RouteType = "DETERMINISTIC_LOCK"
	RouteArbitrary     RouteType = "ARBITRARY_SHELL"
	RoutePassThrough   RouteType = "PASS_THROUGH"
	RouteDeny          RouteType = "DENY"
)

// EvaluatePayload determines the routing strategy for an incoming JSON-RPC tool call.
func EvaluatePayload(payload []byte) (RouteType, string) {
	method := gjson.GetBytes(payload, "method").String()
	
	// We only intercept tool calls. Schema requests (tools/list) pass through 
	// (handled separately by MITM schema injection).
	if method != "tools/call" {
		return RoutePassThrough, ""
	}

	toolName := gjson.GetBytes(payload, "params.name").String()

	// 1. Check for Arbitrary Execution (The Shell Loophole)
	if toolName == "bash_command" || toolName == "execute_script" {
		// Strict Gateway: We route to HITL but flag it as arbitrary
		return RouteArbitrary, toolName
	}

	// 2. Check for Deterministic File Mutations
	if toolName == "write_file" || toolName == "replace_in_file" || toolName == "edit_file" {
		return RouteDeterministic, toolName
	}

	// 3. Safe read-only tools or unmonitored tools pass through
	return RoutePassThrough, toolName
}