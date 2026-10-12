import { useEffect, useRef, useState } from "react";
import { Check, LogIn, ShieldCheck, X } from "lucide-react";
import { api, writeOptions } from "./api";
import type { LoginProvider, User } from "./types";

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
}: {
  providers: LoginProvider[] | null | undefined;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = ref.current!;
    dialog.showModal();
    return () => dialog.close();
  }, []);
  return (
    <dialog
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
        <p>登录方式加载失败，请刷新</p>
      ) : providers.length === 0 ? (
        <p>暂无可用登录方式</p>
      ) : (
        <div className="login-options">
          {providers.map((provider) => (
            <a
              className="provider-login"
              key={provider.id}
              href={provider.loginUrl}
            >
              <ProviderIcon provider={provider} />
              <span>{provider.name} 登录</span>
            </a>
          ))}
        </div>
      )}
    </dialog>
  );
}

export function AccountBindings({
  user,
  providers,
}: {
  user: User;
  providers: LoginProvider[] | null | undefined;
}) {
  const [pending, setPending] = useState<LoginProvider | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
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
  useEffect(() => {
    if (!pending) return;
    const dialog = ref.current!;
    dialog.showModal();
    return () => dialog.close();
  }, [pending]);
  const bind = async () => {
    if (!pending) return;
    setBusy(true);
    setError("");
    try {
      const result = await api<{ url: string }>(
        `/api/auth/bind/${pending.id}`,
        writeOptions("POST", user.csrfToken),
      );
      location.assign(result.url);
    } catch (error) {
      setError((error as Error).message);
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
        <span>绑定后不可解绑</span>
      </div>
      {providers === null && <p className="error">登录方式加载失败，请刷新</p>}
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
                    setError("");
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
      {pending && (
        <dialog
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
          {error && (
            <p role="alert" className="error">
              {error}
            </p>
          )}
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
        </dialog>
      )}
    </section>
  );
}
