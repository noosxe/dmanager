import { Link } from "@tanstack/react-router";
import type { LucideIcon } from "lucide-react";

// Shared tab bar (design.md §9.4 / §7.5, #192). The bar owns no vertical
// margin — the page root's gap carries the spacing.
export interface PageTabItem {
  to: string;
  params: Record<string, string>;
  icon: LucideIcon;
  label: string;
  active: boolean;
  // Optional optimistic local state (e.g. Settings switches tabs before the
  // route settles); the route remains the source of truth.
  onClick?: () => void;
}

export function PageTabs({ tabs }: { tabs: PageTabItem[] }) {
  return (
    <div className="page-tabs">
      {tabs.map((tab) => (
        <Link
          key={tab.label}
          to={tab.to}
          params={tab.params}
          className={`page-tab ${tab.active ? "active" : ""}`}
          onClick={tab.onClick}
        >
          <tab.icon size={16} />
          <span>{tab.label}</span>
        </Link>
      ))}
    </div>
  );
}
