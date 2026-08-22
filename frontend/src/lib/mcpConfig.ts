export const mcpClients = ["claude-code", "cursor", "generic"] as const;

export type McpClient = (typeof mcpClients)[number];

const localStdioConfig = JSON.stringify({
  mcpServers: {
    argus: {
      command: "uv",
      args: ["run", "argus-mcp"],
      env: { ARGUS_BASE_URL: "http://127.0.0.1:8000" },
    },
  },
}, null, 2);

const clientConfigs: Record<McpClient, string> = {
  "claude-code": localStdioConfig,
  cursor: localStdioConfig,
  generic: localStdioConfig,
};

export function getMcpConfig(client: McpClient) {
  return clientConfigs[client];
}
