import { useEffect, useRef, useState } from "react";
import { ChevronDown, LogOut, Settings, ShieldCheck } from "lucide-react";
import { api, writeOptions } from "./api";
import { clearAccountDrafts } from "./group-drafts";
import { Directory } from "./Directory";
import type { ListKind } from "./ListTabs";
import { AccountBindings, LoginDialog } from "./Account";
import { Admin } from "./Admin";
import { Notifications, notify } from "./notifications";
import { TRAFFIC_ENABLED } from "./runtime";
import { FooterMeta } from "./FooterMeta";
import { TrafficStats } from "./TrafficStats";
import { PageTransition, Presence } from "./motion";
import { AudioButton } from "./AudioButton";
import type { User, LoginProvider } from "./types";
import { CookieNotice, LegalPage } from "./Legal";

export default function App() {
  const [path, setPath] = useState(location.pathname);
  const manage = path === "/manage";
  const legalPage =
    path === "/terms" ? "terms" : path === "/privacy" ? "privacy" : null;
  const adminPage = [
    "/admin",
    "/admin/",
    "/admin/users",
    "/admin/groups",
    "/admin/sites",
  ].includes(path);
  const [user, setUser] = useState<User | null>(null);
  const [authLoaded, setAuthLoaded] = useState(false);
  const [providers, setProviders] = useState<
    LoginProvider[] | null | undefined
  >(undefined);
  const [loginDialog, setLoginDialog] = useState(false);
  const [kinds, setKinds] = useState<{ home: ListKind; manage: ListKind }>({
    home: "groups",
    manage: "groups",
  });
  const kind = kinds[manage ? "manage" : "home"];
  const [menu, setMenu] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const [logoutBusy, setLogoutBusy] = useState(false);
  const navigate = (target: string) => {
    history.pushState(null, "", target);
    setPath(target);
    setMenu(false);
  };
  useEffect(() => {
    const listener = () => {
      setPath(location.pathname);
    };
    window.addEventListener("popstate", listener);
    return () => window.removeEventListener("popstate", listener);
  }, []);
  useEffect(() => {
    const title = legalPage
      ? legalPage === "terms"
        ? "用户协议"
        : "隐私政策"
      : manage
        ? "管理奶表"
        : adminPage
          ? path === "/admin/users"
            ? "用户管理"
            : path === "/admin/groups"
              ? "奶群管理"
              : path === "/admin/sites"
                ? "奶站管理"
                : "站点概览"
          : kind === "groups"
            ? "奶群列表"
            : "奶站列表";
    document.title = `${title} - NaiList`;
  }, [path, kind, manage, adminPage, legalPage]);
  // 供 CSS 区分页面：主页显示奶蛙背景，管理页不显示。
  useEffect(() => {
    document.documentElement.dataset.page = path === "/" ? "home" : "manage";
  }, [path]);
  useEffect(() => {
    const controller = new AbortController();
    api<User | null>("/api/me", { signal: controller.signal })
      .then(setUser)
      .catch((e) => {
        if (e.name !== "AbortError") notify.error("登录状态加载失败");
      })
      .finally(() => {
        if (!controller.signal.aborted) setAuthLoaded(true);
      });
    api<LoginProvider[]>("/api/auth/providers", { signal: controller.signal })
      .then((data) => setProviders(Array.isArray(data) ? data : []))
      .catch((error) => {
        if (error.name !== "AbortError") {
          setProviders(null);
          notify.error("登录方式加载失败，请重试");
        }
      });
    const params = new URLSearchParams(location.search);
    if (params.has("bound")) {
      notify.success("已绑定");
      history.replaceState(null, "", location.pathname);
    }
    const authError = params.get("auth_error");
    if (authError) {
      notify.error(
        (
          {
            unconfigured: "登录方式未配置",
            identity_conflict: "此登录身份已绑定其他账号",
            already_bound: "此账号已绑定该登录方式",
            session_changed: "登录状态已变化，请重新绑定",
            rate_limited: "登录操作过于频繁，请稍后重试",
          } as Record<string, string>
        )[authError] || "登录或绑定失败，请重试",
      );
      history.replaceState(null, "", location.pathname);
    }
    return () => controller.abort();
  }, []);
  const retryProviders = async () => {
    setProviders(undefined);
    try {
      const data = await api<LoginProvider[]>("/api/auth/providers");
      setProviders(Array.isArray(data) ? data : []);
    } catch {
      setProviders(null);
      notify.error("登录方式加载失败，请重试");
    }
  };
  useEffect(() => {
    if (!menu) return;
    const close = (event: PointerEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) setMenu(false);
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setMenu(false);
    };
    document.addEventListener("pointerdown", close);
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("pointerdown", close);
      document.removeEventListener("keydown", escape);
    };
  }, [menu]);
  const sessionExpired = async () => {
    if (user) clearAccountDrafts(user.id);
    setUser(null);
    setMenu(false);
    try {
      setUser(await api<User | null>("/api/me"));
    } catch {
      notify.error("登录状态刷新失败，请重新登录");
    }
  };
  return (
    <>
      <header className="nav">
        <div className="nav-inner">
          <a
            className="brand"
            href="/"
            onClick={(e) => {
              e.preventDefault();
              navigate("/");
            }}
          >
            <img
              className="brand-mark"
              src="/nailong.png"
              alt=""
              width="38"
              height="38"
            />
            <span>NaiList</span>
          </a>
          <div className="nav-right">
            <AudioButton />
            {path !== "/" && (
              <a
                className="nav-link"
                href="/"
                onClick={(e) => {
                  e.preventDefault();
                  navigate("/");
                }}
              >
                首页
              </a>
            )}
            {user ? (
              <div ref={menuRef} className="profile">
                <button
                  className="avatar-button"
                  aria-label="账户菜单"
                  aria-expanded={menu}
                  aria-haspopup="menu"
                  onClick={() => setMenu((current) => !current)}
                >
                  <span className="avatar">
                    {user.avatarUrl ? (
                      <img
                        src={user.avatarUrl}
                        alt=""
                        referrerPolicy="no-referrer"
                        onError={(e) => {
                          e.currentTarget.hidden = true;
                        }}
                      />
                    ) : (
                      user.nickname.slice(0, 1)
                    )}
                  </span>
                  <ChevronDown size={14} />
                </button>
                <Presence visible={menu}>
                  {menu && (
                    <div className="account-menu" role="menu">
                      <span className="nickname">{user.nickname}</span>
                      <button
                        role="menuitem"
                        onClick={() => navigate("/manage")}
                      >
                        <Settings size={16} />
                        管理奶表
                      </button>
                      {user.isAdmin && (
                        <button
                          role="menuitem"
                          onClick={() => navigate("/admin")}
                        >
                          <ShieldCheck size={16} />
                          站点管理
                        </button>
                      )}
                      <button
                        role="menuitem"
                        disabled={logoutBusy}
                        onClick={async () => {
                          setLogoutBusy(true);
                          try {
                            await api(
                              "/api/auth/logout",
                              writeOptions("POST", user.csrfToken),
                            );
                            clearAccountDrafts(user.id);
                            setUser(null);
                            navigate("/");
                            notify.success("已退出");
                          } catch (e) {
                            notify.error((e as Error).message);
                          } finally {
                            setLogoutBusy(false);
                          }
                        }}
                      >
                        <LogOut size={16} />
                        退出登录
                      </button>
                    </div>
                  )}
                </Presence>
              </div>
            ) : authLoaded ? (
              <button className="login" onClick={() => setLoginDialog(true)}>
                登录
              </button>
            ) : (
              <span className="auth-skeleton" />
            )}
          </div>
        </div>
      </header>
      <PageTransition value={path}>
        {legalPage ? (
          <LegalPage page={legalPage} />
        ) : adminPage ? (
          <Admin
            key={`${path}:${user?.id ?? "guest"}`}
            page={
              path === "/admin/users"
                ? "users"
                : path === "/admin/groups"
                  ? "groups"
                  : path === "/admin/sites"
                    ? "sites"
                    : "overview"
            }
            user={user}
            authLoaded={authLoaded}
            providers={providers}
            navigate={navigate}
            onLogin={() => setLoginDialog(true)}
            onSessionExpired={sessionExpired}
          />
        ) : (
          <>
            {manage && user && (
              <AccountBindings
                user={user}
                providers={providers}
                onRetry={retryProviders}
                onSessionExpired={sessionExpired}
              />
            )}
            <Directory
              key={`${manage ? "manage" : "home"}:${user?.id ?? "guest"}`}
              manage={manage}
              user={user}
              authLoaded={authLoaded}
              kind={kind}
              onKindChange={(value) =>
                setKinds((current) => ({
                  ...current,
                  [manage ? "manage" : "home"]: value,
                }))
              }
              onLogin={() => setLoginDialog(true)}
              onSessionExpired={sessionExpired}
            />
          </>
        )}
      </PageTransition>
      <footer className="footer">
        <FooterMeta />
        <p>反馈支持联系 请搜索加入 兰理工 群</p>
        {TRAFFIC_ENABLED && <TrafficStats />}
      </footer>
      <Presence visible={loginDialog}>
        {loginDialog && (
          <LoginDialog
            providers={providers}
            onClose={() => setLoginDialog(false)}
            onRetry={retryProviders}
          />
        )}
      </Presence>
      <Notifications />
      <CookieNotice />
    </>
  );
}
