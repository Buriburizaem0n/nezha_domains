//go:build agentcompat

package rpc

import (
	"github.com/nezhahq/nezha/model"
)

func defaultTelemetryOnly() bool {
	return false
}

func autoLockdownAgentIfNeeded(server *model.Server) {
	// agentcompat harness tests full agent control (MCP exec, terminal, FM)
}
