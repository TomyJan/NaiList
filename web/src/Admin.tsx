import { useEffect, useMemo, useRef, useState } from "react";
import { Pencil, Search, X } from "lucide-react";
import { api, ApiError, writeOptions } from "./api";
import { AdminOverview } from "./AdminOverview";
import { GroupForm } from "./GroupForm";
import { clearAccountDrafts, clearGroupDraft } from "./group-drafts";
import { notify } from "./notifications";
import type {
  AdminGroup,
  AdminUser,
  GroupInput,
  LoginProvider,
  User,
} from "./types";
import "./admin.css";

type PendingAction = {
  user: AdminUser;
  providerId?: string;
  providerName?: string;
};

export function Admin({
  page,
  user,
  authLoaded,
  providers,
  navigate,
  onLogin,
  onSessionExpired,
}: {
  page: "overview" | "users" | "groups";
  user: User | null;
  authLoaded: boolean;
  providers: LoginProvider[] | null | undefined;
  navigate: (path: string) => void;
  onLogin: () => void;
  onSessionExpired: () => Promise<void>;
}) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [groups, setGroups] = useState<AdminGroup[]>([]);
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [revision, setRevision] = useState(0);
  const [editor, setEditor] = useState<AdminGroup | null>(null);
  const [pending, setPending] = useState<PendingAction | null>(null);
  const mounted = useRef(false);
  const sessionHandler = useRef(onSessionExpired);
  sessionHandler.current = onSessionExpired;
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  useEffect(() => {
    if (!user?.isAdmin || page === "overview") return;
    const controller = new AbortController();
    setLoading(true);
    setFailed(false);
    Promise.all([
      api<AdminUser[]>("/api/admin/users", { signal: controller.signal }),
      page === "groups"
        ? api<AdminGroup[]>("/api/admin/groups", { signal: controller.signal })
        : Promise.resolve([]),
    ])
      .then(([accounts, records]) => {
        if (controller.signal.aborted) return;
        setUsers(accounts);
        setGroups(records);
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
  }, [user?.id, user?.isAdmin, page, revision]);
  const providerName = (id: string) =>
    providers?.find((provider) => provider.id === id)?.name ||
    (id === "qq" ? "QQ" : id);
  const usersByID = useMemo(
    () => new Map(users.map((account) => [account.id, account])),
    [users],
  );
  const matches = (value: string) =>
    value.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase());
  const visibleUsers = users.filter((account) =>
    matches(
      `${account.id} ${account.nickname} ${account.identities.map((identity) => `${identity.providerId} ${providerName(identity.providerId)} ${identity.nickname}`).join(" ")}`,
    ),
  );
  const ownerLabel = (id: number) =>
    `${usersByID.get(id)?.nickname || "未知用户"}（ID：${id}）`;
  const visibleGroups = groups.filter((group) =>
    matches(
      `${group.organization} ${group.keywords.join(" ")} ${group.qqNumber} ${group.joinUrl} ${ownerLabel(group.ownerId)}`,
    ),
  );
  const write = async (path: string, method: string, body?: unknown) => {
    if (!user) throw new Error("请重新登录");
    try {
      return await api(path, writeOptions(method, user.csrfToken, body));
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        await sessionHandler.current();
      }
      throw error;
    }
  };
  const save = async (input: GroupInput, ownerId?: number) => {
    if (!editor || !user) return;
    await write(`/api/admin/groups/${editor.id}`, "PUT", { ...input, ownerId });
    clearGroupDraft(user.id, editor.id, "admin");
    if (!mounted.current) return;
    setEditor(null);
    setRevision((value) => value + 1);
    notify.success(
      "已保存奶群信息与归属。群信息可能需要一分钟才能更新到首页, 请耐心等待",
      8000,
    );
  };
  const confirm = async () => {
    if (!pending || !user) return;
    const { user: target, providerId } = pending;
    await write(
      `/api/admin/users/${target.id}${providerId ? `/identities/${encodeURIComponent(providerId)}` : ""}`,
      "DELETE",
    );
    if (providerId && target.id === user.id) {
      clearAccountDrafts(user.id);
      notify.success("已解除绑定，请重新登录");
      await sessionHandler.current();
      return;
    }
    if (!mounted.current) return;
    setPending(null);
    setRevision((value) => value + 1);
    notify.success(providerId ? "已解除绑定，该账号需重新登录" : "已删除账号");
  };
  return (
    <>
      <nav className="admin-nav" aria-label="站点管理导航">
        {(["overview", "users", "groups"] as const).map((tab) => (
          <a
            key={tab}
            href={tab === "overview" ? "/admin" : `/admin/${tab}`}
            className={page === tab ? "active" : ""}
            aria-current={page === tab ? "page" : undefined}
            onClick={(event) => {
              event.preventDefault();
              navigate(tab === "overview" ? "/admin" : `/admin/${tab}`);
            }}
          >
            {tab === "overview"
              ? "概览"
              : tab === "users"
                ? "用户管理"
                : "奶群管理"}
          </a>
        ))}
      </nav>
      <section className="page-heading">
        <h1>
          {page === "overview"
            ? "站点概览"
            : page === "users"
              ? "用户管理"
              : "奶群管理"}
          <span className="title-dot">.</span>
        </h1>
      </section>
      {!authLoaded ? (
        <div className="empty">加载登录状态中</div>
      ) : !user ? (
        <div className="empty">
          <p>登录后访问站点管理</p>
          <button className="primary" onClick={onLogin}>
            登录
          </button>
        </div>
      ) : !user.isAdmin ? (
        <div className="empty">无权限访问站点管理</div>
      ) : page === "overview" ? (
        <AdminOverview onSessionExpired={onSessionExpired} />
      ) : (
        <>
          <div className="search-box">
            <Search size={20} />
            <input
              aria-label={page === "users" ? "搜索用户" : "搜索奶群"}
              placeholder={
                page === "users"
                  ? "搜索昵称、账号 ID 或登录方式"
                  : "搜索学校、关键词、群号或所属账号"
              }
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
            {query && (
              <button
                className="icon-button"
                aria-label="清空搜索"
                onClick={() => setQuery("")}
              >
                <X size={18} />
              </button>
            )}
          </div>
          <div className="list-caption">
            {loading
              ? "加载中"
              : failed
                ? "加载失败"
                : page === "users"
                  ? `${visibleUsers.length} 个用户`
                  : `${visibleGroups.length} 个奶群`}
          </div>
          {failed ? (
            <div className="empty">
              <p>管理列表加载失败</p>
              <button
                className="secondary"
                onClick={() => setRevision((value) => value + 1)}
              >
                重试
              </button>
            </div>
          ) : loading ? (
            <div className="skeleton-list" aria-label="加载中">
              {[1, 2, 3].map((index) => (
                <div key={index} className="skeleton-row" />
              ))}
            </div>
          ) : (page === "users" ? visibleUsers : visibleGroups).length === 0 ? (
            <div className="empty">
              <p>
                {query
                  ? `没有匹配的${page === "users" ? "用户" : "奶群"}`
                  : `暂无${page === "users" ? "用户" : "有效奶群"}`}
              </p>
              {query && (
                <button className="secondary" onClick={() => setQuery("")}>
                  清空搜索
                </button>
              )}
            </div>
          ) : page === "users" ? (
            <div className="group-list">
              {visibleUsers.map((account) => {
                const deleteReason =
                  account.id === user.id
                    ? "不能删除当前管理员账号"
                    : account.groupCount > 0
                      ? "请先在奶群管理转移有效奶群归属"
                      : "";
                return (
                  <article className="group-row admin-user" key={account.id}>
                    <div className="admin-user-summary">
                      <h2>{account.nickname}</h2>
                      <p>
                        账号 ID：{account.id} · {account.groupCount} 个有效奶群
                      </p>
                      <button
                        className="secondary"
                        disabled={!!deleteReason}
                        title={deleteReason || undefined}
                        aria-label={`删除账号 ${account.nickname}`}
                        onClick={() => setPending({ user: account })}
                      >
                        删除账号
                      </button>
                      {deleteReason && (
                        <p className="admin-reason">{deleteReason}</p>
                      )}
                    </div>
                    <div className="admin-identities">
                      {account.identities.map((identity) => (
                        <div
                          className="admin-identity"
                          key={identity.providerId}
                        >
                          <div className="binding-info">
                            <strong>
                              {providerName(identity.providerId)}
                              {providerName(identity.providerId) !==
                                identity.providerId &&
                                `（${identity.providerId}）`}
                            </strong>
                            <span>{identity.nickname || "未设置身份昵称"}</span>
                          </div>
                          <div className="admin-identity-action">
                            <button
                              className="secondary"
                              aria-label={`解除 ${account.nickname} 的 ${providerName(identity.providerId)}`}
                              disabled={account.identities.length <= 1}
                              onClick={() =>
                                setPending({
                                  user: account,
                                  providerId: identity.providerId,
                                  providerName: providerName(
                                    identity.providerId,
                                  ),
                                })
                              }
                            >
                              解除绑定
                            </button>
                            {account.identities.length <= 1 && (
                              <span className="admin-reason">
                                至少保留一种登录方式
                              </span>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  </article>
                );
              })}
            </div>
          ) : (
            <div className="group-list">
              {visibleGroups.map((group) => (
                <article className="group-row admin-group" key={group.id}>
                  <div className="admin-group-info">
                    <h2>{group.organization}</h2>
                    <p>
                      关键词：
                      {group.keywords.length
                        ? group.keywords.join("、")
                        : "未填写"}
                    </p>
                    <p>群号：{group.qqNumber}</p>
                    <a
                      className="admin-full-link"
                      href={group.joinUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      {group.joinUrl}
                    </a>
                    <p>所属账号：{ownerLabel(group.ownerId)}</p>
                  </div>
                  <button
                    className="secondary"
                    aria-label={`编辑 ${group.organization}`}
                    onClick={() => setEditor(group)}
                  >
                    <Pencil size={16} />
                    编辑
                  </button>
                </article>
              ))}
            </div>
          )}
        </>
      )}
      {editor && user?.isAdmin && (
        <GroupForm
          key={editor.id}
          accountID={user.id}
          group={editor}
          ownership={{ users, ownerId: editor.ownerId }}
          onClose={() => setEditor(null)}
          onSave={save}
        />
      )}
      {pending && user?.isAdmin && (
        <AdminConfirmation
          action={pending}
          onClose={() => setPending(null)}
          onConfirm={confirm}
        />
      )}
    </>
  );
}

function AdminConfirmation({
  action,
  onClose,
  onConfirm,
}: {
  action: PendingAction;
  onClose: () => void;
  onConfirm: () => Promise<void>;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const mounted = useRef(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const dialog = ref.current!;
    mounted.current = true;
    dialog.showModal();
    return () => {
      mounted.current = false;
      dialog.close();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className="modal delete-modal"
      aria-labelledby="admin-confirm-title"
      onCancel={(event) => {
        if (busy) event.preventDefault();
        else onClose();
      }}
    >
      <h2 id="admin-confirm-title">
        {action.providerId
          ? `解除 ${action.providerName} 绑定？`
          : "删除账号？"}
      </h2>
      <p className="delete-name">
        {action.user.nickname}（ID：{action.user.id}）
      </p>
      <p className="delete-name">
        {action.providerId
          ? "该账号的所有登录会话将失效，需使用保留的登录方式重新登录。"
          : "账号及其登录身份将被永久删除，此操作不可撤销。"}
      </p>
      <div className="modal-actions">
        <button className="secondary" disabled={busy} onClick={onClose}>
          取消
        </button>
        <button
          className="danger"
          disabled={busy}
          onClick={async () => {
            if (busy) return;
            setBusy(true);
            try {
              await onConfirm();
            } catch (error) {
              if (mounted.current) notify.error((error as Error).message);
            } finally {
              if (mounted.current) setBusy(false);
            }
          }}
        >
          {busy ? "处理中…" : action.providerId ? "确认解除" : "确认删除"}
        </button>
      </div>
    </dialog>
  );
}
