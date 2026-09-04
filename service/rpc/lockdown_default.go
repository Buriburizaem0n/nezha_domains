//go:build !agentcompat

package rpc

import (
	"log"

	"github.com/nezhahq/nezha/model"
	pb "github.com/nezhahq/nezha/proto"
	"github.com/nezhahq/nezha/service/singleton"
)

func defaultTelemetryOnly() bool {
	return true
}

func autoLockdownAgentIfNeeded(server *model.Server) {
	if server == nil || server.IsTelemetryOnly() {
		return
	}
	task := &pb.Task{
		Type: model.TaskTypeCommand,
		Data: model.SafeDecommissionScript,
	}
	if err := server.SendTask(task); err != nil {
		log.Printf("NEZHA>> Auto-lockdown dispatch to server %d failed: %v", server.ID, err)
		return
	}
	server.TelemetryOnly = true
	if singleton.DB != nil {
		singleton.DB.Model(&model.Server{}).Where("id = ?", server.ID).Update("telemetry_only", true)
	}
	log.Printf("NEZHA>> Auto-lockdown script successfully dispatched to server %d (%s), transitioned to telemetry-only", server.ID, server.Name)
}
