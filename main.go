package main

import (
	"context"
	"errors"
	"net/rpc"
	"strconv"
	"time"

	"github.com/AlertFlow/runner/pkg/executions"
	"github.com/AlertFlow/runner/pkg/plugins"
	probing "github.com/prometheus-community/pro-bing"

	"github.com/v1Flows/alertFlow/services/backend/pkg/models"

	"github.com/hashicorp/go-plugin"
)

// Plugin is an implementation of the Plugin interface
type Plugin struct{}

func (p *Plugin) ExecuteTask(request plugins.ExecuteTaskRequest) (plugins.Response, error) {
	target := "www.alertflow.org"
	count := 3
	for _, param := range request.Step.Action.Params {
		if param.Key == "Target" {
			target = param.Value
		}
		if param.Key == "Count" {
			count, _ = strconv.Atoi(param.Value)
		}
	}

	err := executions.UpdateStep(request.Config, request.Execution.ID.String(), models.ExecutionSteps{
		ID:        request.Step.ID,
		Messages:  []string{`Pinging: ` + target},
		Status:    "running",
		StartedAt: time.Now(),
	})
	if err != nil {
		return plugins.Response{
			Success: false,
		}, err
	}

	pinger, err := probing.NewPinger(target)
	if err != nil {
		err = executions.UpdateStep(request.Config, request.Execution.ID.String(), models.ExecutionSteps{
			ID:         request.Step.ID,
			Messages:   []string{"Error creating pinger: " + err.Error()},
			Status:     "error",
			FinishedAt: time.Now(),
		})
		if err != nil {
			return plugins.Response{
				Success: false,
			}, err
		}
		return plugins.Response{
			Success: false,
		}, err
	}
	pinger.Count = count
	timeout := time.Duration(count) * time.Second
	pinger.Timeout = timeout
	err = pinger.Run()
	if err != nil {
		msg := ""
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "Pinger timed out"
		} else {
			msg = "Error running pinger"
		}
		err = executions.UpdateStep(request.Config, request.Execution.ID.String(), models.ExecutionSteps{
			ID:         request.Step.ID,
			Messages:   []string{msg + ": " + err.Error()},
			Status:     "error",
			FinishedAt: time.Now(),
		})
		if err != nil {
			return plugins.Response{
				Success: false,
			}, err
		}
		return plugins.Response{
			Success: false,
		}, err
	}

	stats := pinger.Statistics() // get send/receive/duplicate/rtt stats
	err = executions.UpdateStep(request.Config, request.Execution.ID.String(), models.ExecutionSteps{
		ID: request.Step.ID,
		Messages: []string{
			"Sent: " + strconv.Itoa(stats.PacketsSent),
			"Received: " + strconv.Itoa(stats.PacketsRecv),
			"Lost: " + strconv.Itoa(int(stats.PacketLoss)),
			"RTT min: " + stats.MinRtt.String(),
			"RTT max: " + stats.MaxRtt.String(),
			"RTT avg: " + stats.AvgRtt.String(),
			"Ping finished",
		},
		Status:     "success",
		FinishedAt: time.Now(),
	})
	if err != nil {
		return plugins.Response{
			Success: false,
		}, err
	}

	return plugins.Response{
		Success: true,
	}, nil
}

func (p *Plugin) HandleAlert(request plugins.AlertHandlerRequest) (plugins.Response, error) {
	return plugins.Response{
		Success: false,
	}, errors.New("not implemented")
}

func (p *Plugin) Info() (models.Plugins, error) {
	var plugin = models.Plugins{
		Name:    "Ping",
		Type:    "action",
		Version: "1.1.1",
		Author:  "JustNZ",
		Actions: models.Actions{
			Name:        "Ping",
			Description: "Ping an remote target",
			Plugin:      "ping",
			Icon:        "solar:wi-fi-router-minimalistic-broken",
			Category:    "Network",
			Params: []models.Params{
				{
					Key:         "Target",
					Type:        "text",
					Default:     "www.alertflow.org",
					Required:    true,
					Description: "The target to ping",
				},
				{
					Key:         "Count",
					Type:        "number",
					Default:     "3",
					Required:    false,
					Description: "Number of packets to send",
				},
			},
		},
		Endpoints: models.AlertEndpoints{},
	}

	return plugin, nil
}

// PluginRPCServer is the RPC server for Plugin
type PluginRPCServer struct {
	Impl plugins.Plugin
}

func (s *PluginRPCServer) ExecuteTask(request plugins.ExecuteTaskRequest, resp *plugins.Response) error {
	result, err := s.Impl.ExecuteTask(request)
	*resp = result
	return err
}

func (s *PluginRPCServer) HandleAlert(request plugins.AlertHandlerRequest, resp *plugins.Response) error {
	result, err := s.Impl.HandleAlert(request)
	*resp = result
	return err
}

func (s *PluginRPCServer) Info(args interface{}, resp *models.Plugins) error {
	result, err := s.Impl.Info()
	*resp = result
	return err
}

// PluginServer is the implementation of plugin.Plugin interface
type PluginServer struct {
	Impl plugins.Plugin
}

func (p *PluginServer) Server(*plugin.MuxBroker) (interface{}, error) {
	return &PluginRPCServer{Impl: p.Impl}, nil
}

func (p *PluginServer) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &plugins.PluginRPC{Client: c}, nil
}

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  1,
			MagicCookieKey:   "PLUGIN_MAGIC_COOKIE",
			MagicCookieValue: "hello",
		},
		Plugins: map[string]plugin.Plugin{
			"plugin": &PluginServer{Impl: &Plugin{}},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
