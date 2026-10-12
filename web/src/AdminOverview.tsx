import { useEffect, useRef, useState } from "react";
import {
  Building2,
  List,
  RefreshCw,
  UserRoundCheck,
  Users,
} from "lucide-react";
import { api, ApiError } from "./api";
import { notify } from "./notifications";
import type { AdminStats } from "./types";

export function AdminOverview({
  onSessionExpired,
}: {
  onSessionExpired: () => Promise<void>;
}) {
  const [stats, setStats] = useState<AdminStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [revision, setRevision] = useState(0);
  const [days, setDays] = useState<7 | 30>(30);
  const sessionHandler = useRef(onSessionExpired);
  sessionHandler.current = onSessionExpired;
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setFailed(false);
    api<AdminStats>("/api/admin/stats", { signal: controller.signal })
      .then((data) => {
        if (!controller.signal.aborted) setStats(data);
      })
      .catch((error: Error) => {
        if (controller.signal.aborted) return;
        setFailed(true);
        notify.error(error.message);
        if (error instanceof ApiError && error.status === 401)
          void sessionHandler.current();
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [revision]);
  const daily = stats?.daily.slice(-days) ?? [];
  const today = stats?.daily.at(-1);
  const metrics = stats
    ? [
        {
          label: "用户总数",
          value: stats.users,
          note: `今日新增 ${today?.users ?? 0}`,
          Icon: Users,
        },
        {
          label: "有效奶群",
          value: stats.groups,
          note: `今日新增 ${today?.groups ?? 0}`,
          Icon: List,
        },
        {
          label: "上传用户",
          value: stats.contributors,
          note: "拥有有效奶群的用户",
          Icon: UserRoundCheck,
        },
        {
          label: "学校 / 单位",
          value: stats.organizations,
          note: "按学校 / 单位名称去重",
          Icon: Building2,
        },
      ]
    : [];
  return (
    <section
      className="admin-overview"
      aria-label="站点统计"
      aria-busy={loading}
    >
      <div className="overview-toolbar">
        <p>北京时间 · {today?.date ?? "每日新增"}</p>
        <button
          className="secondary"
          disabled={loading}
          onClick={() => setRevision((v) => v + 1)}
        >
          <RefreshCw
            size={16}
            aria-hidden="true"
            className={loading ? "refresh-spinning" : undefined}
          />
          刷新统计
        </button>
      </div>
      {failed ? (
        <div className="empty">
          <p>统计加载失败</p>
          <button
            className="secondary"
            onClick={() => setRevision((v) => v + 1)}
          >
            重试
          </button>
        </div>
      ) : loading ? (
        <div className="skeleton-list" aria-label="加载统计中">
          {[1, 2, 3].map((i) => (
            <div key={i} className="skeleton-row" />
          ))}
        </div>
      ) : (
        stats && (
          <>
            <div className="stat-grid">
              {metrics.map(({ label, value, note, Icon }) => (
                <article key={label} className="stat-card" aria-label={label}>
                  <div className="stat-label">
                    <span>{label}</span>
                    <Icon size={18} aria-hidden="true" />
                  </div>
                  <strong className="stat-value">
                    {value.toLocaleString("zh-CN")}
                  </strong>
                  <p>{note}</p>
                </article>
              ))}
            </div>
            <div className="trend-heading">
              <h2>新增趋势</h2>
              <div
                className="trend-range"
                role="group"
                aria-label="趋势时间范围"
              >
                {([7, 30] as const).map((range) => (
                  <button
                    key={range}
                    aria-pressed={days === range}
                    onClick={() => setDays(range)}
                  >
                    {range} 天
                  </button>
                ))}
              </div>
            </div>
            <div className="trend-grid">
              <TrendChart
                key={`users-${days}`}
                daily={daily}
                field="users"
                label="新增用户"
              />
              <TrendChart
                key={`groups-${days}`}
                daily={daily}
                field="groups"
                label="新增奶群"
              />
            </div>
            <p className="stats-method">
              按北京时间创建日期统计现存用户与有效奶群，已删除数据不计入。
            </p>
          </>
        )
      )}
    </section>
  );
}

function TrendChart({
  daily,
  field,
  label,
}: {
  daily: AdminStats["daily"];
  field: "users" | "groups";
  label: string;
}) {
  const [selected, setSelected] = useState(daily.length - 1);
  const max = Math.max(1, ...daily.map((day) => day[field]));
  const total = daily.reduce((sum, day) => sum + day[field], 0);
  const width = 400;
  const height = 140;
  const step = width / Math.max(1, daily.length);
  const current = daily[selected];
  return (
    <figure className={`trend-card trend-${field}`}>
      <figcaption>
        <h3>{label}</h3>
        <span>
          近 {daily.length} 天 · 共 {total.toLocaleString("zh-CN")} 个
        </span>
      </figcaption>
      <svg viewBox="0 0 448 190" role="group" aria-label={`${label}趋势图`}>
        {[0, 1, 2].map((i) => (
          <g key={i} aria-hidden="true">
            <line
              className="trend-gridline"
              x1="36"
              x2="436"
              y1={16 + (i * height) / 2}
              y2={16 + (i * height) / 2}
            />
            <text
              className="trend-axis"
              x="28"
              y={20 + (i * height) / 2}
              textAnchor="end"
            >
              {i === 0 ? max : i === 2 ? 0 : ""}
            </text>
          </g>
        ))}
        {daily.map((day, index) => {
          const barHeight = (day[field] / max) * height;
          return (
            <g
              key={day.date}
              className="trend-day"
              tabIndex={0}
              role="img"
              aria-label={`${day.date} ${label} ${day[field]} 个`}
              onFocus={() => setSelected(index)}
              onMouseEnter={() => setSelected(index)}
              onClick={() => setSelected(index)}
            >
              <title>
                {day.date} · {day[field]} 个
              </title>
              <rect
                className="trend-hit"
                x={36 + index * step}
                y="10"
                width={step}
                height="152"
                rx="3"
              />
              <rect
                className={`trend-bar${index === selected ? " selected" : ""}`}
                x={36 + index * step + step * 0.2}
                y={16 + height - barHeight}
                width={step * 0.6}
                height={barHeight}
                rx="2"
              />
            </g>
          );
        })}
        <text className="trend-axis" x="36" y="181">
          {daily[0]?.date.slice(5)}
        </text>
        <text className="trend-axis" x="436" y="181" textAnchor="end">
          {daily.at(-1)?.date.slice(5)}
        </text>
        {total === 0 && (
          <text className="trend-axis" x="236" y="82" textAnchor="middle">
            暂无新增
          </text>
        )}
      </svg>
      <div className="trend-detail">
        <span>{current?.date.slice(5) ?? "—"}</span>
        <strong>{current?.[field] ?? 0} 个</strong>
      </div>
    </figure>
  );
}
