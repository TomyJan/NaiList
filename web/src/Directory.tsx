import { useEffect, useRef, useState } from "react";
import {
  ArrowUpRight,
  Check,
  Copy,
  Globe,
  Pencil,
  Plus,
  Search,
  ShieldCheck,
  Shuffle,
  Trash2,
  X,
} from "lucide-react";
import { api, ApiError, writeOptions } from "./api";
import { GroupForm, DeleteDialog } from "./GroupForm";
import { SiteForm } from "./SiteForm";
import { ListTabs, type ListKind } from "./ListTabs";
import { clearGroupDraft } from "./group-drafts";
import { clearSiteDraft } from "./site-drafts";
import { filterGroups } from "./search";
import { displayDomain, filterSites } from "./site-input";
import { shuffleGroups } from "./group-order";
import { notify } from "./notifications";
import { Presence, RefreshIcon } from "./motion";
import type { Group, GroupInput, Site, SiteInput, User } from "./types";

type Editor =
  { kind: "groups"; item: Group | null } | { kind: "sites"; item: Site | null };

export function Directory({
  manage,
  user,
  authLoaded,
  kind,
  onKindChange,
  onLogin,
  onSessionExpired,
}: {
  manage: boolean;
  user: User | null;
  authLoaded: boolean;
  kind: ListKind;
  onKindChange: (kind: ListKind) => void;
  onLogin: () => void;
  onSessionExpired: () => Promise<void>;
}) {
  const [groups, setGroups] = useState<Group[]>([]);
  const [sites, setSites] = useState<Site[]>([]);
  const [queries, setQueries] = useState({ groups: "", sites: "" });
  const [revisions, setRevisions] = useState({ groups: 0, sites: 0 });
  const loaded = useRef<Partial<Record<ListKind, number>>>({});
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [editor, setEditor] = useState<Editor | null>(null);
  const [deleting, setDeleting] = useState<Group | Site | null>(null);
  const [copied, setCopied] = useState<number | null>(null);
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
    if (!authLoaded) return;
    if (manage && !user) {
      setLoading(false);
      return;
    }
    if (loaded.current[kind] === revisions[kind]) {
      setLoading(false);
      setFailed(false);
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    setFailed(false);
    const path = `/api/${manage ? "my/" : ""}${kind}`;
    const request =
      kind === "groups"
        ? api<Group[]>(path, { signal: controller.signal }).then((data) => {
            if (!controller.signal.aborted)
              setGroups(manage ? data : shuffleGroups(data));
          })
        : api<Site[]>(path, { signal: controller.signal }).then((data) => {
            if (!controller.signal.aborted)
              setSites(manage ? data : shuffleGroups(data));
          });
    request
      .then(() => {
        if (!controller.signal.aborted) loaded.current[kind] = revisions[kind];
      })
      .catch((error: Error) => {
        if (controller.signal.aborted) return;
        setFailed(true);
        notify.error(error.message);
        if (manage && error instanceof ApiError && error.status === 401)
          void sessionHandler.current();
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [kind, revisions, manage, user?.id, authLoaded]);
  useEffect(() => {
    if (copied === null) return;
    const timer = setTimeout(() => setCopied(null), 2600);
    return () => clearTimeout(timer);
  }, [copied]);
  const query = queries[kind];
  const setQuery = (value: string) =>
    setQueries((current) => ({ ...current, [kind]: value }));
  const refresh = () =>
    setRevisions((current) => ({ ...current, [kind]: current[kind] + 1 }));
  const visibleGroups = filterGroups(groups, query);
  const visibleSites = filterSites(sites, query);
  const count = kind === "groups" ? visibleGroups.length : visibleSites.length;
  const noun = kind === "groups" ? "奶群" : "奶站";
  const write = async (path: string, method: string, body?: unknown) => {
    if (!user) throw new Error("请重新登录");
    try {
      await api(path, writeOptions(method, user.csrfToken, body));
    } catch (error) {
      if (error instanceof ApiError && error.status === 401)
        await sessionHandler.current();
      throw error;
    }
  };
  const save = async (input: GroupInput | SiteInput) => {
    if (!editor || !user) return;
    const { kind: resource, item } = editor;
    await write(
      `/api/${resource}${item ? `/${item.id}` : ""}`,
      item ? "PUT" : "POST",
      input,
    );
    if (resource === "groups") clearGroupDraft(user.id, item?.id);
    else clearSiteDraft(user.id, item?.id);
    if (!mounted.current) return;
    setEditor(null);
    refresh();
    notify.success(
      resource === "groups"
        ? "已保存。群信息可能需要一分钟才能更新到首页, 请耐心等待"
        : "已保存。奶站信息可能需要一分钟才能更新到首页, 请耐心等待",
      8000,
    );
  };
  const remove = async () => {
    if (!deleting || !user) return;
    const resource = "qqNumber" in deleting ? "groups" : "sites";
    await write(`/api/${resource}/${deleting.id}`, "DELETE");
    if (resource === "groups") clearGroupDraft(user.id, deleting.id);
    else clearSiteDraft(user.id, deleting.id);
    if (!mounted.current) return;
    setDeleting(null);
    refresh();
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
  const actions = (item: Group | Site) =>
    manage && (
      <div className="manage-actions">
        <button
          className="icon-button"
          aria-label={`编辑 ${"name" in item ? item.name : item.organization}`}
          onClick={() =>
            setEditor(
              "qqNumber" in item
                ? { kind: "groups", item }
                : { kind: "sites", item },
            )
          }
        >
          <Pencil size={17} />
        </button>
        <button
          className="icon-button delete-button"
          aria-label={`删除 ${"name" in item ? item.name : item.organization}`}
          onClick={() => setDeleting(item)}
        >
          <Trash2 size={17} />
        </button>
      </div>
    );
  return (
    <>
      <section className="page-heading">
        <div>
          <h1>
            {manage ? "管理奶表" : "奶龙群与站, 防止迷路"}
            <span className="title-dot">.</span>
          </h1>
          {!manage && (
            <div className="heading-intro">
              <p>发现奶群与奶站; 登录后可上传与管理奶群与奶站</p>
              <ul className="heading-notes">
                <li className="shuffle-note">
                  <Shuffle size={14} aria-hidden="true" />
                  每次加载列表随机排序, 以保证曝光
                </li>
                <li>
                  <ShieldCheck size={14} aria-hidden="true" />
                  所有信息由用户自行上传, 请注意甄别
                </li>
                <li>帮助支持信息请查看页脚</li>
              </ul>
            </div>
          )}
        </div>
      </section>
      <ListTabs kind={kind} onChange={onKindChange} />
      <section
        key={kind}
        className="panel-enter"
        id="directory-panel"
        role="tabpanel"
        aria-labelledby={`tab-${kind}`}
      >
        {manage && user && (
          <div className="directory-heading">
            <h2>我的{noun}</h2>
            <button
              className="primary add-button"
              onClick={() =>
                setEditor(
                  kind === "groups"
                    ? { kind, item: null }
                    : { kind, item: null },
                )
              }
            >
              <Plus size={17} />
              添加{noun}
            </button>
          </div>
        )}
        {manage && authLoaded && !user ? (
          <div className="empty">
            <p>登录后管理奶表</p>
            <button className="primary" onClick={onLogin}>
              登录
            </button>
          </div>
        ) : (
          <>
            <div className="search-toolbar">
              <div className="search-box">
                <Search size={20} />
                <input
                  aria-label={`搜索${noun}`}
                  placeholder={
                    kind === "groups"
                      ? "搜索学校、群号或关键词"
                      : "搜索站名、学校 / 单位或域名"
                  }
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
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
                onClick={refresh}
              >
                <RefreshIcon
                  size={20}
                  active={loading}
                  cycle={revisions[kind]}
                />
              </button>
            </div>
            {!manage && kind === "groups" && (
              <div className="recommended-searches" aria-label="推荐搜索">
                <span>推荐搜索</span>
                <button
                  type="button"
                  className="secondary"
                  onClick={() => setQuery("世界奶蛙组织")}
                >
                  世界奶蛙组织
                </button>
              </div>
            )}
            <div className="list-caption">
              {loading ? "加载中" : `${count} 个${noun}`}
            </div>
            {failed ? (
              <div className="empty">
                <button className="secondary" onClick={refresh}>
                  重试
                </button>
              </div>
            ) : loading ? (
              <div className="skeleton-list" aria-label="加载中">
                {[1, 2, 3].map((id) => (
                  <div className="skeleton-row" key={id} />
                ))}
              </div>
            ) : count === 0 ? (
              <div className="empty">
                <p>
                  {query
                    ? `没有匹配的${noun}`
                    : manage
                      ? `还没有添加${noun}`
                      : `暂无${noun}`}
                </p>
                {query && (
                  <button className="secondary" onClick={() => setQuery("")}>
                    清空搜索
                  </button>
                )}
              </div>
            ) : (
              <div className="group-list">
                {kind === "groups"
                  ? visibleGroups.map((group) => (
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
                          {actions(group)}
                        </div>
                      </article>
                    ))
                  : visibleSites.map((site) => (
                      <article className="group-row site-row" key={site.id}>
                        <div className="group-identity">
                          <div className="group-icon" aria-hidden="true">
                            <Globe size={19} />
                          </div>
                          <div className="site-identity">
                            <h2>{site.name}</h2>
                            <p>{site.organization}</p>
                          </div>
                        </div>
                        <div className="group-actions">
                          <span className="site-domain">
                            {displayDomain(site.domain)}
                          </span>
                          <a
                            className="join-button"
                            aria-label={`访问 ${site.name}`}
                            href={`https://${site.domain}`}
                            target="_blank"
                            rel="noopener noreferrer"
                          >
                            访问
                            <ArrowUpRight size={16} />
                          </a>
                          {actions(site)}
                        </div>
                      </article>
                    ))}
              </div>
            )}
          </>
        )}
      </section>
      <Presence visible={!!editor && !!user}>
        {editor &&
          user &&
          (editor.kind === "groups" ? (
            <GroupForm
              accountID={user.id}
              group={editor.item}
              onClose={() => setEditor(null)}
              onSave={save}
            />
          ) : (
            <SiteForm
              accountID={user.id}
              site={editor.item}
              onClose={() => setEditor(null)}
              onSave={save}
            />
          ))}
      </Presence>
      <Presence visible={!!deleting}>
        {deleting && (
          <DeleteDialog
            group={deleting}
            onClose={() => setDeleting(null)}
            onDelete={remove}
          />
        )}
      </Presence>
    </>
  );
}
