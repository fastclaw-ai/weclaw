"use client";

import { useEffect, useState } from "react";
import { SidebarLayout } from "@/components/sidebar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import { Save, RotateCcw, Check } from "lucide-react";
import {
  getConfig,
  updateConfig,
  restartWeclaw,
  type Config,
} from "@/lib/api";

export default function SettingsPage() {
  const [config, setConfig] = useState<Config | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [restarting, setRestarting] = useState(false);

  const [defaultAgent, setDefaultAgent] = useState("");
  const [apiAddr, setApiAddr] = useState("");
  const [saveDir, setSaveDir] = useState("");

  useEffect(() => {
    setLoading(true);
    getConfig()
      .then((cfg) => {
        setConfig(cfg);
        setDefaultAgent(cfg.default_agent || "");
        setApiAddr(cfg.api_addr || "");
        setSaveDir(cfg.save_dir || "");
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setSaved(false);
    try {
      await updateConfig({
        default_agent: defaultAgent,
        api_addr: apiAddr,
        save_dir: saveDir,
      });
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch (e) {
      alert("Failed to save settings: " + e);
    } finally {
      setSaving(false);
    }
  };

  const handleRestart = async () => {
    if (!confirm("Restart WeClaw? This will briefly disconnect all sessions.")) return;
    setRestarting(true);
    try {
      await restartWeclaw();
    } catch {
      // Expected: server goes down during restart
    }
    // Wait for server to come back
    setTimeout(() => {
      setRestarting(false);
      window.location.reload();
    }, 3000);
  };

  const agentNames = config ? Object.keys(config.agents) : [];

  return (
    <SidebarLayout>
      <div className="p-6 space-y-6 max-w-3xl mx-auto">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Settings</h2>
          <p className="text-sm text-muted-foreground mt-1">
            Global WeClaw configuration
          </p>
        </div>

        {loading ? (
          <div className="space-y-4">
            <Skeleton className="h-48" />
            <Skeleton className="h-32" />
          </div>
        ) : (
          <>
            <Card>
              <CardHeader>
                <CardTitle className="text-base">General</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label>Default Agent</Label>
                  <Select
                    value={defaultAgent}
                    onValueChange={(v) => v && setDefaultAgent(v)}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select default agent" />
                    </SelectTrigger>
                    <SelectContent>
                      {agentNames.map((name) => (
                        <SelectItem key={name} value={name}>
                          {name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">
                    Agent used when no specific agent is mentioned
                  </p>
                </div>

                <Separator />

                <div className="space-y-2">
                  <Label>API Address</Label>
                  <Input
                    value={apiAddr}
                    onChange={(e) => setApiAddr(e.target.value)}
                    placeholder="127.0.0.1:18011"
                  />
                  <p className="text-xs text-muted-foreground">
                    HTTP API and Web UI listen address
                  </p>
                </div>

                <div className="space-y-2">
                  <Label>Save Directory</Label>
                  <Input
                    value={saveDir}
                    onChange={(e) => setSaveDir(e.target.value)}
                    placeholder="~/.weclaw/workspace"
                  />
                  <p className="text-xs text-muted-foreground">
                    Directory for saving downloaded images and files
                  </p>
                </div>

                <div className="flex gap-3 pt-2">
                  <Button onClick={handleSave} disabled={saving}>
                    {saved ? (
                      <>
                        <Check className="h-4 w-4 mr-2" />
                        Saved
                      </>
                    ) : (
                      <>
                        <Save className="h-4 w-4 mr-2" />
                        {saving ? "Saving..." : "Save Settings"}
                      </>
                    )}
                  </Button>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-base">Service Control</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground mb-4">
                  Restart WeClaw to apply configuration changes. All active
                  sessions will be reset.
                </p>
                <Button
                  variant="outline"
                  onClick={handleRestart}
                  disabled={restarting}
                >
                  <RotateCcw className="h-4 w-4 mr-2" />
                  {restarting ? "Restarting..." : "Restart WeClaw"}
                </Button>
              </CardContent>
            </Card>
          </>
        )}
      </div>
    </SidebarLayout>
  );
}
