import { Cookie } from "lucide-react";
import { useLayoutEffect, useRef } from "react";
import {
  COOKIE_NOTICE_KEY,
  POLICY_VERSION,
  useAcknowledgement,
} from "./consent";
import { policies } from "./legal-content";
import { Presence } from "./motion";

export function LegalPage({ page }: { page: keyof typeof policies }) {
  const policy = policies[page];
  return (
    <article className="legal-page panel-enter" aria-labelledby="legal-title">
      <header className="legal-heading">
        <span className="legal-eyebrow">NaiList</span>
        <h1 id="legal-title">{policy.title}</h1>
        <p>{policy.description}</p>
        <span className="legal-date">
          更新及生效日期：
          <time dateTime={POLICY_VERSION}>{POLICY_VERSION}</time>
        </span>
      </header>
      <div className="legal-content">
        {policy.sections.map((section, index) => (
          <section key={section.title}>
            <h2>
              {index + 1}. {section.title}
            </h2>
            {section.paragraphs.map((paragraph) => (
              <p key={paragraph}>{paragraph}</p>
            ))}
          </section>
        ))}
      </div>
      <nav className="legal-related" aria-label="相关政策">
        <a href={page === "terms" ? "/privacy" : "/terms"}>
          {page === "terms" ? "隐私政策" : "用户协议"}
        </a>
        <a href="/">返回首页</a>
      </nav>
    </article>
  );
}

function CookieNoticeContent({ onConfirm }: { onConfirm: () => void }) {
  const ref = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    const update = () =>
      document.documentElement.style.setProperty(
        "--cookie-notice-space",
        `${ref.current!.getBoundingClientRect().height + 32}px`,
      );
    const observer = new ResizeObserver(update);
    observer.observe(ref.current!);
    update();
    return () => {
      observer.disconnect();
      document.documentElement.style.removeProperty("--cookie-notice-space");
    };
  }, []);
  return (
    <aside
      className="cookie-notice"
      ref={ref}
      aria-label="Cookie 提示"
      role="region"
    >
      <Cookie size={24} aria-hidden="true" />
      <div>
        <strong>关于 Cookie 与本地存储</strong>
        <p>
          用于维持登录、记住协议选择和保留编辑草稿。访问统计及其他数据处理说明见{" "}
          <a href="/privacy" target="_blank" rel="noopener noreferrer">
            隐私政策
          </a>
          。
        </p>
      </div>
      <button className="primary" onClick={onConfirm}>
        知道了
      </button>
    </aside>
  );
}

export function CookieNotice() {
  const [confirmed, setConfirmed] = useAcknowledgement(COOKIE_NOTICE_KEY);
  return (
    <Presence visible={!confirmed}>
      {!confirmed && (
        <CookieNoticeContent onConfirm={() => setConfirmed(true)} />
      )}
    </Presence>
  );
}
