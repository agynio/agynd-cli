package daemon

import (
	"fmt"
	"time"

	"github.com/agynio/agynd-cli/internal/config"
)

const mcpsPageSize int32 = 100

type mcpDefinition struct {
	ID        string
	Name      string
	CreatedAt time.Time
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

// mcpDefinitionsFromServers names what the Orchestrator injected. The ports it
// carries are authoritative; the names are all resolveMCPServers needs.
func mcpDefinitionsFromServers(servers []config.MCPServer) []mcpDefinition {
	definitions := make([]mcpDefinition, 0, len(servers))
	for _, server := range servers {
		definitions = append(definitions, mcpDefinition{Name: server.Name})
	}
	return definitions
}
