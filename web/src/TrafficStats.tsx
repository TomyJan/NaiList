import { useEffect, useRef, useState } from "react";
import { RefreshIcon } from "./motion";
import { api } from "./api";
import { notify } from "./notifications";
import { useWrappingSeparators } from "./useWrappingSeparators";

type Counts = { visitors: number; pageviews: number };
type Stats = {
  today: Counts;
  total: Counts;
  online: number;
  updatedAt: string;
};

export function TrafficStats() {
  const ref = useWrappingSeparators();
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const busy = useRef(false);
  const controller = useRef<AbortController | null>(null);
  async function load(signal: AbortSignal, manual = false) {
    if (busy.current) return;
    busy.current = true;
    if (manual) setRevision((value) => value + 1);
    setLoading(true);
    try {
      const data = await api<Stats>("/api/traffic", {
        signal,
        cache: "no-store",
      });
      if (!signal.aborted) setStats(data);
    } catch (error) {
      if (!signal.aborted && manual) notify.error((error as Error).message);
    } finally {
      if (!signal.aborted) {
        busy.current = false;
        setLoading(false);
      }
    }
  }
  useEffect(() => {
    const request = new AbortController();
    controller.current = request;
    void load(request.signal);
    return () => {
      request.abort();
      busy.current = false;
    };
  }, []);
  const number = (value: number | undefined) => value ?? "—";
  return (
    <div
      className="traffic-stats"
      ref={ref}
      aria-label="访问统计"
      aria-live="polite"
      aria-busy={loading}
    >
      <span className="traffic-group">
        <span>今日</span> <span>访客 {number(stats?.today.visitors)}</span>{" "}
        <span>浏览 {number(stats?.today.pageviews)}</span>
      </span>
      <span className="traffic-group">
        <span>总计</span> <span>访客 {number(stats?.total.visitors)}</span>{" "}
        <span>浏览 {number(stats?.total.pageviews)}</span>
      </span>
      <span className="traffic-group">
        <span title="最近五分钟内的活跃访客">
          当前在线 {number(stats?.online)}
        </span>
        <button
          className="traffic-refresh"
          type="button"
          aria-label="刷新访问统计"
          disabled={loading}
          onClick={() => {
            if (controller.current) void load(controller.current.signal, true);
          }}
        >
          <RefreshIcon size={12} active={loading} cycle={revision} />
          刷新
        </button>
      </span>
    </div>
  );
}
