"use client";

import { useEffect, useState } from "react";
import { SidebarLayout } from "@/components/sidebar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { Bot, Plus, Pencil, Trash2 } from "lucide-react";
import {
  getAgents,
  createAgent,
  updateAgent,
  deleteAgent,
  type AgentConfig,
} from "@/lib/api";

const agentTypes = ["acp", "cli", "http"];

const defaultAgent: AgentConfig = {
  type: "acp",
  command: "",
  args: [],
  aliases: [],
  model: "",
  system_prompt: "",
  endpoint: "",
  api_key: "",
  cwd: "",
  max_history: 0,
};

export default function AgentsPage() {
  const [agents, setAgents] = useState<Record<string, AgentConfig>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  // Create dialog state
  const [createOpen, setCreateOpen] = useState(false);
  const [newName, setNewName] = useState("");
  const [newAgent, setNewAgent] = useState<AgentConfig>({ ...defaultAgent });

  // Edit dialog state
  const [editOpen, setEditOpen] = useState(false);
  const [editName, setEditName] = useState("");
  const [editAgent, setEditAgent] = useState<AgentConfig>({ ...defaultAgent });

  // Delete dialog state
  const [deleteName, setDeleteName] = useState<string | null>(null);

  const fetchAgents = () => {
    setLoading(true);
    getAgents()
      .then(setAgents)
      .catch(() => setAgents({}))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    fetchAgents();
  }, []);

  const handleCreate = async () => {
    if (!newName.trim()) return;
    setSaving(true);
    try {
      await createAgent(newName.trim(), newAgent);
      setCreateOpen(false);
      setNewName("");
      setNewAgent({ ...defaultAgent });
      fetchAgents();
    } catch (e) {
      alert("Failed to create agent: " + e);
    } finally {
      setSaving(false);
    }
  };

  const handleEdit = (name: string) => {
    setEditName(name);
    setEditAgent({ ...agents[name] });
    setEditOpen(true);
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateAgent(editName, editAgent);
      setEditOpen(false);
      fetchAgents();
    } catch (e) {
      alert("Failed to update agent: " + e);
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteName) return;
    try {
      await deleteAgent(deleteName);
      setDeleteName(null);
      fetchAgents();
    } catch (e) {
      alert("Failed to delete agent: " + e);
    }
  };

  const AgentForm = ({
    agent,
    onChange,
  }: {
    agent: AgentConfig;
    onChange: (a: AgentConfig) => void;
  }) => (
    <div className="space-y-4 py-2">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div className="space-y-2">
          <Label>Type</Label>
          <Select
            value={agent.type}
            onValueChange={(v) => v && onChange({ ...agent, type: v })}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {agentTypes.map((t) => (
                <SelectItem key={t} value={t}>
                  {t}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>Model</Label>
          <Input
            value={agent.model || ""}
            onChange={(e) => onChange({ ...agent, model: e.target.value })}
            placeholder="e.g. sonnet, gpt-4o"
          />
        </div>
      </div>

      {(agent.type === "acp" || agent.type === "cli") && (
        <>
          <div className="space-y-2">
            <Label>Command</Label>
            <Input
              value={agent.command || ""}
              onChange={(e) => onChange({ ...agent, command: e.target.value })}
              placeholder="Binary path, e.g. /usr/local/bin/claude-agent-acp"
            />
          </div>
          <div className="space-y-2">
            <Label>Args (one per line)</Label>
            <Textarea
              value={(agent.args || []).join("\n")}
              onChange={(e) =>
                onChange({
                  ...agent,
                  args: e.target.value
                    .split("\n")
                    .filter((s) => s.trim() !== ""),
                })
              }
              placeholder="acp&#10;--url&#10;ws://127.0.0.1:18789"
              rows={3}
              className="resize-none font-mono text-sm"
            />
          </div>
          <div className="space-y-2">
            <Label>Working Directory</Label>
            <Input
              value={agent.cwd || ""}
              onChange={(e) => onChange({ ...agent, cwd: e.target.value })}
              placeholder="/path/to/workspace"
            />
          </div>
        </>
      )}

      {agent.type === "http" && (
        <>
          <div className="space-y-2">
            <Label>Endpoint</Label>
            <Input
              value={agent.endpoint || ""}
              onChange={(e) => onChange({ ...agent, endpoint: e.target.value })}
              placeholder="http://127.0.0.1:18789/v1/chat/completions"
            />
          </div>
          <div className="space-y-2">
            <Label>API Key</Label>
            <Input
              type="password"
              value={agent.api_key || ""}
              onChange={(e) => onChange({ ...agent, api_key: e.target.value })}
              placeholder="Bearer token"
            />
          </div>
          <div className="space-y-2">
            <Label>Max History</Label>
            <Input
              type="number"
              value={agent.max_history || ""}
              onChange={(e) =>
                onChange({
                  ...agent,
                  max_history: parseInt(e.target.value) || 0,
                })
              }
              placeholder="20"
            />
          </div>
        </>
      )}

      <div className="space-y-2">
        <Label>Aliases (comma separated)</Label>
        <Input
          value={(agent.aliases || []).join(", ")}
          onChange={(e) =>
            onChange({
              ...agent,
              aliases: e.target.value
                .split(",")
                .map((s) => s.trim())
                .filter(Boolean),
            })
          }
          placeholder="ai, c"
        />
      </div>

      <div className="space-y-2">
        <Label>System Prompt</Label>
        <Textarea
          value={agent.system_prompt || ""}
          onChange={(e) =>
            onChange({ ...agent, system_prompt: e.target.value })
          }
          placeholder="You are a helpful AI assistant..."
          rows={3}
          className="resize-none"
        />
      </div>
    </div>
  );

  return (
    <SidebarLayout>
      <div className="p-6 space-y-6 max-w-5xl mx-auto">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-2xl font-semibold tracking-tight">Agents</h2>
            <p className="text-sm text-muted-foreground mt-1">
              Manage your AI agents and their configurations
            </p>
          </div>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4 mr-2" />
            New Agent
          </Button>
        </div>

        <div className="rounded-lg border border-border bg-card">
          {loading ? (
            <div className="p-6 space-y-3">
              {[1, 2].map((i) => (
                <Skeleton key={i} className="h-14 w-full" />
              ))}
            </div>
          ) : Object.keys(agents).length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16 text-center">
              <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10 mb-4">
                <Bot className="h-7 w-7 text-primary" />
              </div>
              <p className="text-sm text-muted-foreground">
                No agents configured yet
              </p>
              <Button
                onClick={() => setCreateOpen(true)}
                variant="outline"
                className="mt-4"
              >
                Create your first agent
              </Button>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Model</TableHead>
                  <TableHead>Aliases</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {Object.entries(agents).map(([name, agent]) => (
                  <TableRow
                    key={name}
                    className="cursor-pointer hover:bg-muted/50 transition-colors"
                    onClick={() => handleEdit(name)}
                  >
                    <TableCell>
                      <div className="flex items-center gap-3">
                        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10">
                          <Bot className="h-4 w-4 text-primary" />
                        </div>
                        <span className="font-medium">{name}</span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant="secondary">{agent.type}</Badge>
                    </TableCell>
                    <TableCell>
                      {agent.model ? (
                        <code className="bg-muted px-2 py-0.5 rounded font-mono text-xs">
                          {agent.model}
                        </code>
                      ) : (
                        <span className="text-muted-foreground text-xs">-</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {agent.aliases && agent.aliases.length > 0 ? (
                        <div className="flex gap-1 flex-wrap">
                          {agent.aliases.map((a) => (
                            <Badge key={a} variant="outline" className="text-xs">
                              {a}
                            </Badge>
                          ))}
                        </div>
                      ) : (
                        <span className="text-muted-foreground text-xs">-</span>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-muted-foreground hover:text-foreground"
                          onClick={(e) => {
                            e.stopPropagation();
                            handleEdit(name);
                          }}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-muted-foreground hover:text-destructive"
                          onClick={(e) => {
                            e.stopPropagation();
                            setDeleteName(name);
                          }}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>

        {/* Create Dialog */}
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogContent className="sm:max-w-2xl max-h-[85vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>Create New Agent</DialogTitle>
              <DialogDescription>
                Configure a new AI agent for WeClaw
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-2">
              <div className="space-y-2">
                <Label>Agent Name</Label>
                <Input
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  placeholder="my-agent"
                />
              </div>
            </div>
            <AgentForm agent={newAgent} onChange={setNewAgent} />
            <DialogFooter>
              <Button variant="outline" onClick={() => setCreateOpen(false)}>
                Cancel
              </Button>
              <Button
                onClick={handleCreate}
                disabled={!newName.trim() || saving}
              >
                {saving ? "Creating..." : "Create Agent"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Edit Dialog */}
        <Dialog open={editOpen} onOpenChange={setEditOpen}>
          <DialogContent className="sm:max-w-2xl max-h-[85vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <Bot className="h-5 w-5 text-primary" />
                {editName}
              </DialogTitle>
              <DialogDescription>Edit agent configuration</DialogDescription>
            </DialogHeader>
            <AgentForm agent={editAgent} onChange={setEditAgent} />
            <DialogFooter>
              <Button variant="outline" onClick={() => setEditOpen(false)}>
                Cancel
              </Button>
              <Button onClick={handleSave} disabled={saving}>
                {saving ? "Saving..." : "Save Changes"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Delete Confirmation */}
        <AlertDialog
          open={!!deleteName}
          onOpenChange={() => setDeleteName(null)}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete Agent</AlertDialogTitle>
              <AlertDialogDescription>
                Are you sure you want to delete <strong>{deleteName}</strong>?
                This action cannot be undone.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction
                onClick={handleDelete}
                className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              >
                Delete
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </SidebarLayout>
  );
}
