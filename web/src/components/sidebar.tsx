"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
  Bot,
  Settings,
  Menu,
  X,
  Sun,
  Moon,
} from "lucide-react";
import { useState, useEffect } from "react";
import { useTheme } from "@/components/theme-provider";
import { getHealth } from "@/lib/api";

const navItems = [
  { href: "/", label: "Overview", icon: LayoutDashboard },
  { href: "/agents/", label: "Agents", icon: Bot },
  { href: "/settings/", label: "Settings", icon: Settings },
];

export function SidebarLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const [mobileOpen, setMobileOpen] = useState(false);
  const { theme, toggleTheme } = useTheme();
  const [healthy, setHealthy] = useState(false);

  useEffect(() => {
    getHealth().then(setHealthy);
    const interval = setInterval(() => {
      getHealth().then(setHealthy);
    }, 15000);
    return () => clearInterval(interval);
  }, []);

  const NavLinks = ({ onClick }: { onClick?: () => void }) => (
    <>
      {navItems.map((item) => {
        const isActive =
          item.href === "/"
            ? pathname === "/" || pathname === "/index.html"
            : pathname.startsWith(item.href);
        return (
          <Link
            key={item.href}
            href={item.href}
            onClick={onClick}
            className={`flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
              isActive
                ? "bg-primary/10 text-primary"
                : "text-muted-foreground hover:bg-muted/50 hover:text-foreground"
            }`}
          >
            <item.icon className="h-4 w-4" />
            {item.label}
          </Link>
        );
      })}
    </>
  );

  return (
    <div className="flex min-h-screen bg-background">
      {/* Desktop sidebar */}
      <aside className="hidden w-56 flex-col border-r border-border bg-card/50 md:flex">
        <div className="flex h-14 items-center gap-2.5 border-b border-border px-4">
          <div className="relative flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10">
            <Bot className="h-5 w-5 text-primary" />
            <span
              className={`absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full border-2 border-card ${
                healthy
                  ? "bg-emerald-500 animate-pulse"
                  : "bg-muted-foreground/40"
              }`}
            />
          </div>
          <div>
            <span className="text-sm font-semibold text-foreground">
              WeClaw
            </span>
            <p className="text-[10px] text-muted-foreground leading-none">
              {healthy ? "Running" : "Stopped"}
            </p>
          </div>
        </div>

        <nav className="flex-1 space-y-1 p-3 overflow-y-auto">
          <NavLinks />
        </nav>

        <div className="border-t border-border p-3">
          <div className="flex items-center justify-between">
            <span className="text-[11px] text-muted-foreground/60 font-mono">
              v0.1.0
            </span>
            <button
              onClick={toggleTheme}
              className="rounded-md p-1.5 text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors"
            >
              {theme === "dark" ? (
                <Sun className="h-3.5 w-3.5" />
              ) : (
                <Moon className="h-3.5 w-3.5" />
              )}
            </button>
          </div>
        </div>
      </aside>

      {/* Mobile header */}
      <div className="fixed top-0 left-0 right-0 z-40 flex h-12 items-center justify-between border-b border-border bg-card/95 px-4 backdrop-blur-sm md:hidden">
        <div className="flex items-center gap-2">
          <div className="relative flex h-7 w-7 items-center justify-center rounded-md bg-primary/10">
            <Bot className="h-4 w-4 text-primary" />
            <span
              className={`absolute -bottom-0.5 -right-0.5 h-2 w-2 rounded-full border-[1.5px] border-card ${
                healthy ? "bg-emerald-500" : "bg-muted-foreground/40"
              }`}
            />
          </div>
          <span className="text-sm font-semibold text-foreground">WeClaw</span>
        </div>
        <button
          onClick={() => setMobileOpen(!mobileOpen)}
          className="rounded-md p-2 text-muted-foreground hover:text-foreground"
        >
          {mobileOpen ? (
            <X className="h-5 w-5" />
          ) : (
            <Menu className="h-5 w-5" />
          )}
        </button>
      </div>

      {/* Mobile menu overlay */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-30 bg-background/80 backdrop-blur-sm md:hidden"
          onClick={() => setMobileOpen(false)}
        >
          <div
            className="absolute top-12 right-0 w-64 border-l border-border bg-card p-3 space-y-1 h-[calc(100vh-3rem)] overflow-y-auto"
            onClick={(e) => e.stopPropagation()}
          >
            <NavLinks onClick={() => setMobileOpen(false)} />
          </div>
        </div>
      )}

      {/* Main content */}
      <main className="flex-1 pt-12 md:pt-0 overflow-y-auto">{children}</main>
    </div>
  );
}
