import { SITE_VERSION } from "./runtime";
import { useWrappingSeparators } from "./useWrappingSeparators";

export function FooterMeta() {
  const ref = useWrappingSeparators();

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
