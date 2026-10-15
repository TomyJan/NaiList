import { useLayoutEffect, useRef } from "react";
import { SITE_VERSION } from "./runtime";

export function FooterMeta() {
  const ref = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const container = ref.current;
    if (!container) return;
    const items = Array.from(container.children) as HTMLElement[];
    const updateSeparators = () => {
      for (const [index, item] of items.entries()) {
        const next = items[index + 1];
        item.dataset.separator = String(
          !!next && item.offsetTop === next.offsetTop,
        );
      }
    };
    const observer = new ResizeObserver(updateSeparators);
    observer.observe(container);
    items.forEach((item) => observer.observe(item));
    updateSeparators();
    return () => observer.disconnect();
  }, []);

  return (
    <div className="footer-meta" ref={ref}>
      <span>
        Powered by{" "}
        <a href="https://vov.moe" target="_blank" rel="noopener noreferrer">
          TomyJan
        </a>
      </span>
      <span className="footer-version">
        <a
          href="https://github.com/TomyJan/NaiList"
          target="_blank"
          rel="noopener noreferrer"
        >
          V{SITE_VERSION}
        </a>
      </span>
      <span>
        <a href="/terms">用户协议</a>
      </span>
      <span>
        <a href="/privacy">隐私政策</a>
      </span>
    </div>
  );
}
