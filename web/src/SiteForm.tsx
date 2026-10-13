import { useEffect, useRef, useState } from "react";
import { MotionDialog } from "./motion";
import { Undo2, X } from "lucide-react";
import { normalizeDomain, validateSiteInput } from "./site-input";
import { readSiteDraft, writeSiteDraft, type SiteDraft } from "./site-drafts";
import { OwnerPicker } from "./OwnerPicker";
import { notify } from "./notifications";
import type { AdminUser, Site, SiteInput } from "./types";

export function SiteForm({
  accountID,
  site,
  ownership,
  onClose,
  onSave,
}: {
  accountID: number;
  site: Site | null;
  ownership?: { users: AdminUser[]; ownerId: number };
  onClose: () => void;
  onSave: (input: SiteInput, ownerId?: number) => Promise<void>;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const mounted = useRef(false);
  const scope = ownership ? "admin" : "personal";
  const [original] = useState<SiteDraft>(() => ({
    name: site?.name ?? "",
    domain: site?.domain ?? "",
    organization: site?.organization ?? "",
    ...(ownership ? { ownerId: ownership.ownerId } : {}),
  }));
  const [input, setInput] = useState<SiteDraft>(() => ({
    ...original,
    ...readSiteDraft(accountID, site?.id, scope),
  }));
  const [busy, setBusy] = useState(false);
  const [ownerRevision, setOwnerRevision] = useState(0);
  useEffect(() => {
    writeSiteDraft(accountID, site?.id, input, scope);
  }, [accountID, site?.id, input, scope]);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const field = (key: keyof SiteInput, value: string) =>
    setInput((current) => ({ ...current, [key]: value }));
  return (
    <MotionDialog
      ref={ref}
      className="modal"
      aria-labelledby="site-form-title"
      onCancel={(event) => {
        if (busy) event.preventDefault();
        else onClose();
      }}
      onClick={(event) => {
        if (event.target !== ref.current || busy) return;
        const bounds = ref.current!.getBoundingClientRect();
        if (
          event.clientX < bounds.left ||
          event.clientX > bounds.right ||
          event.clientY < bounds.top ||
          event.clientY > bounds.bottom
        )
          onClose();
      }}
    >
      <form
        noValidate
        onSubmit={async (event) => {
          event.preventDefault();
          if (busy) return;
          const normalized = {
            ...input,
            domain: normalizeDomain(input.domain),
          };
          const error = validateSiteInput(normalized);
          if (error) {
            notify.error(error);
            return;
          }
          if (
            ownership &&
            !ownership.users.some((owner) => owner.id === input.ownerId)
          ) {
            notify.error("请选择有效的所属账号");
            return;
          }
          setBusy(true);
          try {
            const { ownerId, ...fields } = normalized;
            await onSave(
              {
                name: fields.name.trim(),
                organization: fields.organization.trim(),
                domain: normalizeDomain(fields.domain),
              },
              ownerId,
            );
          } catch (error) {
            if (mounted.current) notify.error((error as Error).message);
          } finally {
            if (mounted.current) setBusy(false);
          }
        }}
      >
        <div className="modal-head">
          <h2 id="site-form-title">{site ? "编辑奶站" : "添加奶站"}</h2>
          <button
            type="button"
            className="icon-button"
            aria-label="关闭"
            disabled={busy}
            onClick={onClose}
          >
            <X size={20} />
          </button>
        </div>
        <label>
          <span className="site-field-label">
            站点名称{" "}
            <span className="field-counter" aria-hidden="true">
              {Array.from(input.name).length} / 32
            </span>
          </span>
          <input
            aria-label="站点名称"
            autoFocus
            required
            disabled={busy}
            value={input.name}
            onChange={(event) =>
              field(
                "name",
                Array.from(event.target.value).slice(0, 32).join(""),
              )
            }
            placeholder="填写站点名称"
          />
        </label>
        <div className="site-link-field">
          <label htmlFor="site-domain">站点域名</label>
          <div className="site-domain-input">
            <span aria-hidden="true">https://</span>
            <input
              id="site-domain"
              required
              disabled={busy}
              value={input.domain}
              aria-describedby="site-domain-help"
              autoCapitalize="none"
              autoComplete="off"
              spellCheck={false}
              onChange={(event) => field("domain", event.target.value)}
              onBlur={(event) =>
                field("domain", normalizeDomain(event.target.value))
              }
              onPaste={(event) => {
                event.preventDefault();
                field(
                  "domain",
                  normalizeDomain(event.clipboardData.getData("text")),
                );
              }}
              placeholder="nailong.example.com"
            />
          </div>
          <p className="field-help" id="site-domain-help">
            可粘贴完整链接，自动去除协议、www. 和路径；仅支持 HTTPS
          </p>
        </div>
        <label>
          <span className="site-field-label">
            学校 / 单位{" "}
            <span className="field-counter" aria-hidden="true">
              {Array.from(input.organization).length} / 32
            </span>
          </span>
          <input
            aria-label="学校 / 单位"
            required
            disabled={busy}
            value={input.organization}
            onChange={(event) =>
              field(
                "organization",
                Array.from(event.target.value).slice(0, 32).join(""),
              )
            }
            placeholder="填写所属学校或单位"
          />
        </label>
        {ownership && (
          <OwnerPicker
            key={ownerRevision}
            users={ownership.users}
            ownerId={input.ownerId}
            disabled={busy}
            onChange={(ownerId) =>
              setInput((current) => ({ ...current, ownerId }))
            }
          />
        )}
        <div className="modal-actions">
          <button
            type="button"
            className="secondary undo-button"
            disabled={busy}
            onClick={() => {
              setInput({ ...original });
              setOwnerRevision((value) => value + 1);
            }}
          >
            <Undo2 size={16} />
            撤销
          </button>
          <button
            type="button"
            className="secondary"
            disabled={busy}
            onClick={onClose}
          >
            取消
          </button>
          <button className="primary" disabled={busy}>
            {busy ? "保存中…" : "保存"}
          </button>
        </div>
      </form>
    </MotionDialog>
  );
}
