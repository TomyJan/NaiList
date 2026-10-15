import { useRef, useState } from "react";
import { MotionDialog, Presence } from "./motion";
import { Check, LogIn, ShieldCheck, X } from "lucide-react";
import { api, ApiError, writeOptions } from "./api";
import { notify } from "./notifications";
import type { LoginProvider, User } from "./types";
import { AGREEMENT_KEY, useAcknowledgement } from "./consent";

function ProviderIcon({ provider }: { provider: LoginProvider }) {
  const icon =
    provider.id === "qq"
      ? "https://static-res.qq.com/static-res/imqq/qq-logo.png"
      : provider.icon;
  return icon ? (
    <img
      className="provider-icon"
      src={icon}
      alt=""
      referrerPolicy="no-referrer"
      onError={(event) => {
        event.currentTarget.hidden = true;
      }}
    />
  ) : (
    <LogIn size={20} aria-hidden="true" />
  );
}

export function LoginDialog({
  providers,
  onClose,
  onRetry,
}: {
  providers: LoginProvider[] | null | undefined;
  onClose: () => void;
  onRetry: () => Promise<void>;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const [agreed, setAgreed] = useAcknowledgement(AGREEMENT_KEY);
  return (
    <MotionDialog
      ref={ref}
      className="modal login-modal"
      aria-labelledby="login-title"
      onCancel={onClose}
      onClick={(e) => {
        if (e.target === ref.current) onClose();
      }}
    >
      <div className="modal-head">
        <h2 id="login-title">登录</h2>
        <button className="icon-button" aria-label="关闭" onClick={onClose}>
          <X size={20} />
        </button>
      </div>
      {providers === undefined ? (
        <p>加载中</p>
      ) : providers === null ? (
        <button className="secondary" onClick={onRetry}>
          重试
        </button>
      ) : providers.length === 0 ? (
        <p>暂无可用登录方式</p>
      ) : (
        <div className="login-options">
          {providers.map((provider) => (
            <a
              className="provider-login"
              key={provider.id}
              href={agreed ? provider.loginUrl : undefined}
              role="link"
              aria-disabled={!agreed}
              tabIndex={agreed ? 0 : -1}
              onClick={(event) => {
                if (!agreed) event.preventDefault();
              }}
            >
              <ProviderIcon provider={provider} />
              <span>{provider.name} 登录</span>
            </a>
          ))}
        </div>
      )}
      <div className="login-agreement">
        <input
          id="login-agreement"
          type="checkbox"
          checked={agreed}
          onChange={(event) => setAgreed(event.target.checked)}
        />
        <div>
          <label htmlFor="login-agreement">我已阅读并同意</label>
          <a href="/terms" target="_blank" rel="noopener noreferrer">
            用户协议
          </a>
          <span>和</span>
          <a href="/privacy" target="_blank" rel="noopener noreferrer">
            隐私政策
          </a>
        </div>
      </div>
    </MotionDialog>
  );
}

export function AccountBindings({
  user,
  providers,
  onRetry,
  onSessionExpired,
}: {
  user: User;
  onRetry: () => Promise<void>;
  providers: LoginProvider[] | null | undefined;
  onSessionExpired: () => Promise<void>;
}) {
  const [pending, setPending] = useState<LoginProvider | null>(null);
  const [busy, setBusy] = useState(false);
  const ref = useRef<HTMLDialogElement>(null);
  const identities = user.identities || [];
  const choices = [...(providers || [])];
  for (const identity of identities) {
    if (!choices.some((p) => p.id === identity.providerId))
      choices.push({
        id: identity.providerId,
        name: identity.providerId === "qq" ? "QQ" : identity.providerId,
        icon: "",
        loginUrl: "",
      });
  }
  const bind = async () => {
    if (!pending) return;
    setBusy(true);
    try {
      const result = await api<{ url: string }>(
        `/api/auth/bind/${pending.id}`,
        writeOptions("POST", user.csrfToken),
      );
      location.assign(result.url);
    } catch (error) {
      notify.error((error as Error).message);
      if (error instanceof ApiError && error.status === 401) {
        await onSessionExpired();
        return;
      }
      setBusy(false);
    }
  };
  return (
    <section className="bindings" aria-labelledby="bindings-title">
      <div className="bindings-heading">
        <h2 id="bindings-title">
          <ShieldCheck size={18} />
          账号绑定
        </h2>
        <span>账号 ID：{user.id}</span>
      </div>
      {providers === null && (
        <button className="secondary" onClick={onRetry}>
          重试登录方式
        </button>
      )}
      <div className="binding-list">
        {choices.map((provider) => {
          const identity = identities.find((i) => i.providerId === provider.id);
          return (
            <div className="binding-row" key={provider.id}>
              <ProviderIcon provider={provider} />
              <div className="binding-info">
                <strong>{provider.name}</strong>
                <span>{identity?.nickname || "未绑定"}</span>
              </div>
              {identity ? (
                <span className="bound-label">
                  <Check size={15} />
                  已绑定
                </span>
              ) : (
                <button
                  className="secondary"
                  onClick={() => {
                    setPending(provider);
                  }}
                  aria-label={`绑定 ${provider.name}`}
                >
                  绑定
                </button>
              )}
            </div>
          );
        })}
      </div>
      <Presence visible={!!pending}>
        {pending && (
          <MotionDialog
            ref={ref}
            className="modal"
            aria-labelledby="bind-title"
            onCancel={(e) => {
              if (busy) e.preventDefault();
              else setPending(null);
            }}
          >
            <div className="modal-head">
              <h2 id="bind-title">绑定 {pending.name}</h2>
              <button
                className="icon-button"
                aria-label="关闭"
                disabled={busy}
                onClick={() => setPending(null)}
              >
                <X size={20} />
              </button>
            </div>
            <p>绑定后可用于登录，不可解绑。</p>
            <div className="modal-actions">
              <button
                className="secondary"
                disabled={busy}
                onClick={() => setPending(null)}
              >
                取消
              </button>
              <button className="primary" disabled={busy} onClick={bind}>
                {busy ? "跳转中" : "继续绑定"}
              </button>
            </div>
          </MotionDialog>
        )}
      </Presence>
    </section>
  );
}
