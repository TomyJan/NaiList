import { useEffect, useRef, useState } from "react";
import {
  ArrowUpRight,
  Check,
  ChevronDown,
  Copy,
  LogOut,
  Pencil,
  Plus,
  RefreshCw,
  Shuffle,
  Search,
  Settings,
  ShieldCheck,
  Trash2,
  X,
} from "lucide-react";
import { api, ApiError, writeOptions } from "./api";
import { filterGroups } from "./search";
import { shuffleGroups } from "./group-order";
import { GroupForm, DeleteDialog } from "./GroupForm";
import { clearAccountDrafts, clearGroupDraft } from "./group-drafts";
import { AccountBindings, LoginDialog } from "./Account";
import { Admin } from "./Admin";
import { Notifications, notify } from "./notifications";
import { SITE_VERSION } from "./runtime";
import type { Group, GroupInput, User, LoginProvider } from "./types";

export default function App() {
  const [path, setPath] = useState(location.pathname);
  const manage = path === "/manage";
  const adminPage = path === "/admin/users" || path === "/admin/groups";
  const [user, setUser] = useState<User | null>(null);
  const [authLoaded, setAuthLoaded] = useState(false);
  const [providers, setProviders] = useState<
    LoginProvider[] | null | undefined
  >(undefined);
  const [loginDialog, setLoginDialog] = useState(false);
  const [groups, setGroups] = useState<Group[]>([]);
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [revision, setRevision] = useState(0);
  const [menu, setMenu] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const [editor, setEditor] = useState<{ group: Group | null } | null>(null);
  const [deleting, setDeleting] = useState<Group | null>(null);
  const [copied, setCopied] = useState<number | null>(null);
  const [logoutBusy, setLogoutBusy] = useState(false);
  const navigate = (target: string) => {
    history.pushState(null, "", target);
    setPath(target);
    setQuery("");
    setMenu(false);
    setEditor(null);
    setDeleting(null);
  };
  useEffect(() => {
    const listener = () => {
      setPath(location.pathname);
      setQuery("");
      setEditor(null);
      setDeleting(null);
    };
    window.addEventListener("popstate", listener);
    return () => window.removeEventListener("popstate", listener);
  }, []);
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
          } as Record<string, string>
        )[authError] || "登录或绑定失败，请重试",
      );
      history.replaceState(null, "", location.pathname);
    }
    return () => controller.abort();
  }, []);
  useEffect(() => {
    if (!authLoaded || adminPage) return;
    if (manage && !user) {
      setLoading(false);
      setGroups([]);
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    setLoadFailed(false);
    api<Group[]>(manage ? "/api/my/groups" : "/api/groups", {
      signal: controller.signal,
    })
      .then((data) => setGroups(manage ? data : shuffleGroups(data)))
      .catch((e) => {
        if (e.name !== "AbortError") {
          setLoadFailed(true);
          notify.error(e.message);
          if (manage && e instanceof ApiError && e.status === 401)
            void sessionExpired();
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [manage, adminPage, user, revision, authLoaded]);
  useEffect(() => {
    if (copied === null) return;
    const timer = setTimeout(() => setCopied(null), 2600);
    return () => clearTimeout(timer);
  }, [copied]);
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
  const visible = filterGroups(groups, query);
  const sessionExpired = async () => {
    if (user) clearAccountDrafts(user.id);
    setUser(null);
    setEditor(null);
    setDeleting(null);
    setMenu(false);
    try {
      setUser(await api<User | null>("/api/me"));
    } catch {
      notify.error("登录状态刷新失败，请重新登录");
    }
  };
  const writePersonal = async (
    target: string,
    method: string,
    body?: unknown,
  ) => {
    if (!user) throw new Error("请重新登录");
    try {
      await api(target, writeOptions(method, user.csrfToken, body));
    } catch (error) {
      if (error instanceof ApiError && error.status === 401)
        await sessionExpired();
      throw error;
    }
  };
  const save = async (input: GroupInput) => {
    if (!user) return;
    const group = editor?.group;
    await writePersonal(
      group ? `/api/groups/${group.id}` : "/api/groups",
      group ? "PUT" : "POST",
      input,
    );
    clearGroupDraft(user.id, group?.id);
    setEditor(null);
    setRevision((v) => v + 1);
    notify.success(
      "已保存。群信息可能需要一分钟才能更新到首页, 请耐心等待",
      8000,
    );
  };
  const remove = async () => {
    if (!user || !deleting) return;
    await writePersonal(`/api/groups/${deleting.id}`, "DELETE");
    clearGroupDraft(user.id, deleting.id);
    setDeleting(null);
    setRevision((v) => v + 1);
    notify.success("已删除");
  };
  const copy = async (group: Group) => {
    try {
      await navigator.clipboard.writeText(group.qqNumber);
      setCopied(group.id);
      notify.success("已复制");
    } catch {
      notify.error("复制失败，请手动复制");
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
                  onClick={() => setMenu(!menu)}
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
                {menu && (
                  <div className="account-menu" role="menu">
                    <span className="nickname">{user.nickname}</span>
                    <button role="menuitem" onClick={() => navigate("/manage")}>
                      <Settings size={16} />
                      管理奶群
                    </button>
                    {user.isAdmin && (
                      <button
                        role="menuitem"
                        onClick={() => navigate("/admin/users")}
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
      <main className="main">
        {adminPage ? (
          <Admin
            key={`${path}:${user?.id ?? "guest"}`}
            page={path === "/admin/users" ? "users" : "groups"}
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
            <section className="page-heading">
              <div>
                <h1>
                  {manage ? "管理奶群" : "奶龙群列表, 防止迷路"}
                  <span className="title-dot">.</span>
                </h1>
                {!manage && (
                  <div className="heading-intro">
                    <p>登录后可上传与管理自己的奶群</p>
                    <ul className="heading-notes">
                      <li className="shuffle-note">
                        <Shuffle size={14} aria-hidden="true" />
                        每次加载列表随机排序, 以保证曝光
                      </li>
                      <li>所有信息由用户自行上传, 请注意甄别</li>
                      <li>帮助支持信息请查看页脚</li>
                    </ul>
                  </div>
                )}
              </div>
              {manage && user && (
                <button
                  className="primary add-button"
                  onClick={() => setEditor({ group: null })}
                >
                  <Plus size={17} />
                  添加奶群
                </button>
              )}
            </section>
            {manage && authLoaded && !user ? (
              <div className="empty">
                <p>登录后管理奶群</p>
                <button
                  className="primary"
                  onClick={() => setLoginDialog(true)}
                >
                  登录
                </button>
              </div>
            ) : (
              <>
                <div className="search-toolbar">
                  <div className="search-box">
                    <Search size={20} />
                    <input
                      aria-label="搜索奶群"
                      placeholder="搜索学校、群号或关键词"
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                    />
                    {query && (
                      <button
                        aria-label="清空搜索"
                        className="icon-button"
                        onClick={() => setQuery("")}
                      >
                        <X size={18} />
                      </button>
                    )}
                  </div>
                  <button
                    className="secondary refresh-button"
                    aria-label="刷新列表"
                    title="刷新列表"
                    disabled={loading}
                    onClick={() => setRevision((v) => v + 1)}
                  >
                    <RefreshCw
                      size={20}
                      className={loading ? "refresh-spinning" : undefined}
                      aria-hidden="true"
                    />
                  </button>
                </div>
                <div className="list-caption">
                  <span>{loading ? "加载中" : `${visible.length} 个奶群`}</span>
                </div>
                {loadFailed ? (
                  <div className="empty">
                    <button
                      className="secondary"
                      onClick={() => setRevision((v) => v + 1)}
                    >
                      重试
                    </button>
                  </div>
                ) : loading ? (
                  <div className="skeleton-list" aria-label="加载中">
                    {[1, 2, 3].map((i) => (
                      <div className="skeleton-row" key={i} />
                    ))}
                  </div>
                ) : visible.length === 0 ? (
                  <div className="empty">
                    <p>
                      {query
                        ? "没有匹配的奶群"
                        : manage
                          ? "还没有添加奶群"
                          : "暂无奶群"}
                    </p>
                    {query && (
                      <button
                        className="secondary"
                        onClick={() => setQuery("")}
                      >
                        清空搜索
                      </button>
                    )}
                  </div>
                ) : (
                  <div className="group-list">
                    {visible.map((group) => (
                      <article className="group-row" key={group.id}>
                        <div className="group-identity">
                          <div className="group-icon" aria-hidden="true">
                            {group.organization.slice(0, 1)}
                          </div>
                          <h2>{group.organization}</h2>
                        </div>
                        <div className="group-actions">
                          <button
                            className="number-button"
                            aria-label={`复制群号 ${group.qqNumber}`}
                            onClick={() => copy(group)}
                          >
                            <span>{group.qqNumber}</span>
                            {copied === group.id ? (
                              <Check size={15} />
                            ) : (
                              <Copy size={15} />
                            )}
                          </button>
                          <a
                            className="join-button"
                            href={group.joinUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                          >
                            加群
                            <ArrowUpRight size={16} />
                          </a>
                          {manage && (
                            <div className="manage-actions">
                              <button
                                className="icon-button"
                                aria-label={`编辑 ${group.organization}`}
                                onClick={() => setEditor({ group })}
                              >
                                <Pencil size={17} />
                              </button>
                              <button
                                className="icon-button delete-button"
                                aria-label={`删除 ${group.organization}`}
                                onClick={() => setDeleting(group)}
                              >
                                <Trash2 size={17} />
                              </button>
                            </div>
                          )}
                        </div>
                      </article>
                    ))}
                  </div>
                )}
              </>
            )}
          </>
        )}
      </main>
      <footer className="footer">
        <div className="footer-meta">
          <span>
            Powered by{" "}
            <a href="https://vov.moe" target="_blank" rel="noopener noreferrer">
              TomyJan
            </a>
          </span>
          <span aria-hidden="true">|</span>
          <span className="footer-version">V{SITE_VERSION}</span>
          <a
            href="https://github.com/TomyJan/NaiList"
            target="_blank"
            rel="noopener noreferrer"
          >
            GitHub
          </a>
        </div>
        <p>反馈支持请搜索加入 兰理工 群</p>
      </footer>
      {loginDialog && (
        <LoginDialog
          providers={providers}
          onClose={() => setLoginDialog(false)}
          onRetry={retryProviders}
        />
      )}
      <Notifications />
      {editor && user && (
        <GroupForm
          accountID={user.id}
          group={editor.group}
          onClose={() => setEditor(null)}
          onSave={save}
        />
      )}{" "}
      {deleting && (
        <DeleteDialog
          group={deleting}
          onClose={() => setDeleting(null)}
          onDelete={remove}
        />
      )}
    </>
  );
}
