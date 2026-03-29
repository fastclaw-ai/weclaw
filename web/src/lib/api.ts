// --- Types matching Go config structs ---

export interface AgentConfig {
  type: string;
  command?: string;
  args?: string[];
  aliases?: string[];
  cwd?: string;
  env?: Record<string, string>;
  model?: string;
  system_prompt?: string;
  endpoint?: string;
  api_key?: string;
  max_history?: number;
}

export interface Config {
  default_agent: string;
  api_addr?: string;
  save_dir?: string;
  agents: Record<string, AgentConfig>;
}

// --- API functions ---

export async function getConfig(): Promise<Config> {
  const res = await fetch("/api/config");
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function updateConfig(cfg: Partial<Config>): Promise<void> {
  const res = await fetch("/api/config", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(cfg),
  });
  if (!res.ok) throw new Error(await res.text());
}

export async function getAgents(): Promise<Record<string, AgentConfig>> {
  const res = await fetch("/api/config/agents");
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function createAgent(name: string, agent: AgentConfig): Promise<void> {
  const res = await fetch("/api/config/agents", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, ...agent }),
  });
  if (!res.ok) throw new Error(await res.text());
}

export async function updateAgent(name: string, agent: AgentConfig): Promise<void> {
  const res = await fetch(`/api/config/agents/${encodeURIComponent(name)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(agent),
  });
  if (!res.ok) throw new Error(await res.text());
}

export async function deleteAgent(name: string): Promise<void> {
  const res = await fetch(`/api/config/agents/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
  if (!res.ok) throw new Error(await res.text());
}

export async function restartWeclaw(): Promise<void> {
  const res = await fetch("/api/restart", { method: "POST" });
  if (!res.ok) throw new Error(await res.text());
}

export async function getHealth(): Promise<boolean> {
  try {
    const res = await fetch("/health");
    return res.ok;
  } catch {
    return false;
  }
}
