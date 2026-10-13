import { Globe, Users } from "lucide-react";

export type ListKind = "groups" | "sites";

export function ListTabs({
  kind,
  onChange,
}: {
  kind: ListKind;
  onChange: (kind: ListKind) => void;
}) {
  return (
    <div className="list-tabs" role="tablist" aria-label="列表类型">
      {(["groups", "sites"] as const).map((tab) => {
        const Icon = tab === "groups" ? Users : Globe;
        return (
          <button
            key={tab}
            type="button"
            role="tab"
            id={`tab-${tab}`}
            aria-selected={kind === tab}
            aria-controls="directory-panel"
            className={kind === tab ? "active" : undefined}
            onClick={() => onChange(tab)}
          >
            <Icon size={17} aria-hidden="true" />
            {tab === "groups" ? "奶群列表" : "奶站列表"}
          </button>
        );
      })}
    </div>
  );
}
