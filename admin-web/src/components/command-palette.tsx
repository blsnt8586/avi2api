import type React from "react";
import { Search } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "./ui";

export type CommandPaletteItem = {
  key: string;
  label: string;
  detail: string;
  icon: React.ComponentType<{ size?: number }>;
  group: "search" | "navigation";
  onSelect: () => void;
};

export function CommandPalette({
  open,
  query,
  items,
  onOpenChange,
  onQueryChange,
}: {
  open: boolean;
  query: string;
  items: CommandPaletteItem[];
  onOpenChange: (open: boolean) => void;
  onQueryChange: (query: string) => void;
}) {
  const hasSearchItems = items.some((item) => item.group === "search");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="admin-command !gap-0 !p-0" showClose={false}>
        <DialogTitle className="sr-only">全局搜索</DialogTitle>
        <label className="admin-command-search">
          <Search />
          <input
            autoFocus
            aria-label="搜索模块、任务、请求或账号"
            placeholder="搜索页面、任务 ID、请求 ID 或账号"
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
          />
        </label>
        <div className="admin-command-list">
          {items.map((item, index) => {
            const Icon = item.icon;
            const showDivider = hasSearchItems && item.group === "navigation" && items[index - 1]?.group === "search";
            return (
              <div className="admin-command-item" key={item.key}>
                {showDivider && <span className="admin-command-divider">页面导航</span>}
                <button onClick={item.onSelect}>
                  <Icon size={17} />
                  <span>{item.label}</span>
                  <small>{item.detail}</small>
                </button>
              </div>
            );
          })}
          {items.length === 0 && <p className="admin-command-empty">没有匹配的页面或搜索入口</p>}
        </div>
      </DialogContent>
    </Dialog>
  );
}

export default CommandPalette;
