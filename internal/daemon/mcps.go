package daemon

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	"github.com/agynio/agynd-cli/internal/config"
	"google.golang.org/grpc"
)

const mcpsPageSize int32 = 100

type mcpsClient interface {
	ListMcps(ctx context.Context, in *agentsv1.ListMcpsRequest, opts ...grpc.CallOption) (*agentsv1.ListMcpsResponse, error)
}

type mcpDefinition struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

func listMCPs(ctx context.Context, client mcpsClient, agentID string) ([]mcpDefinition, error) {
	if client == nil {
		return nil, fmt.Errorf("mcps client is required")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	var mcps []mcpDefinition
	pageToken := ""
	for {
		resp, err := client.ListMcps(ctx, &agentsv1.ListMcpsRequest{
			AgentId:   agentID,
			PageSize:  mcpsPageSize,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, entry := range resp.GetMcps() {
			parsed, err := mcpFromProto(entry)
			if err != nil {
				return nil, err
			}
			mcps = append(mcps, parsed)
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			break
		}
	}
	sort.Slice(mcps, func(i, j int) bool {
		if mcps[i].CreatedAt.Equal(mcps[j].CreatedAt) {
			return mcps[i].ID < mcps[j].ID
		}
		return mcps[i].CreatedAt.Before(mcps[j].CreatedAt)
	})
	return mcps, nil
}

func mcpFromProto(entry *agentsv1.Mcp) (mcpDefinition, error) {
	if entry == nil {
		return mcpDefinition{}, fmt.Errorf("mcp is required")
	}
	meta := entry.GetMeta()
	if meta == nil {
		return mcpDefinition{}, fmt.Errorf("mcp meta is required")
	}
	id := strings.TrimSpace(meta.GetId())
	if id == "" {
		return mcpDefinition{}, fmt.Errorf("mcp id is required")
	}
	createdAt := meta.GetCreatedAt()
	if createdAt == nil {
		return mcpDefinition{}, fmt.Errorf("mcp created at is required")
	}
	name := strings.TrimSpace(entry.GetName())
	if name == "" {
		return mcpDefinition{}, fmt.Errorf("mcp name is required")
	}
	return mcpDefinition{
		ID:        id,
		Name:      name,
		CreatedAt: createdAt.AsTime(),
	}, nil
}

func resolveMCPServers(definitions []mcpDefinition, envServers []config.MCPServer, fallbackPort *int) ([]config.MCPServer, error) {
	if len(definitions) == 0 {
		if len(envServers) > 0 {
			return nil, fmt.Errorf("AGENT_MCP_SERVERS provided but no MCPs configured")
		}
		if fallbackPort != nil {
			return nil, fmt.Errorf("MCP_PORT provided but no MCPs configured")
		}
		return nil, nil
	}
	portByName := make(map[string]int, len(envServers))
	for _, server := range envServers {
		if server.Name == "" {
			return nil, fmt.Errorf("MCP server name is required")
		}
		if _, ok := portByName[server.Name]; ok {
			return nil, fmt.Errorf("duplicate MCP server name %q", server.Name)
		}
		portByName[server.Name] = server.Port
	}
	if len(portByName) == 0 {
		if fallbackPort == nil {
			return nil, fmt.Errorf("AGENT_MCP_SERVERS is required for %d MCPs", len(definitions))
		}
		if len(definitions) != 1 {
			return nil, fmt.Errorf("MCP_PORT set but %d MCPs configured", len(definitions))
		}
		return []config.MCPServer{{Name: definitions[0].Name, Port: *fallbackPort}}, nil
	}
	seen := make(map[string]struct{}, len(definitions))
	resolved := make([]config.MCPServer, 0, len(definitions))
	for _, definition := range definitions {
		port, ok := portByName[definition.Name]
		if !ok {
			return nil, fmt.Errorf("missing MCP port for %q", definition.Name)
		}
		seen[definition.Name] = struct{}{}
		resolved = append(resolved, config.MCPServer{Name: definition.Name, Port: port})
	}
	if len(seen) != len(portByName) {
		for name := range portByName {
			if _, ok := seen[name]; !ok {
				return nil, fmt.Errorf("AGENT_MCP_SERVERS has unknown MCP %q", name)
			}
		}
	}
	return resolved, nil
}
