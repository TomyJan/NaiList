import { useEffect, useRef, useState } from "react";
import { X } from "lucide-react";
import type { Group, GroupInput } from "./types";

export function GroupForm({
  group,
  onClose,
  onSave,
}: {
  group: Group | null;
  onClose: () => void;
  onSave: (input: GroupInput) => Promise<void>;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const [input, setInput] = useState<GroupInput>({
    organization: group?.organization || "",
    keywords: group?.keywords.join(",") || "",
    qqNumber: group?.qqNumber || "",
    joinUrl: group?.joinUrl || "",
  });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const dialog = ref.current!;
    dialog.showModal();
    return () => dialog.close();
  }, []);
  const field = (key: keyof GroupInput, value: string) =>
    setInput({ ...input, [key]: value });
  return (
    <dialog
      ref={ref}
      className="modal"
      onCancel={(e) => {
        if (busy) e.preventDefault();
        else onClose();
      }}
      onClick={(e) => {
        if (e.target === ref.current && !busy) onClose();
      }}
      aria-labelledby="form-title"
    >
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError("");
          try {
            await onSave(input);
          } catch (error) {
            setError((error as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <div className="modal-head">
          <h2 id="form-title">{group ? "编辑奶群" : "添加奶群"}</h2>
          <button
            type="button"
            className="icon-button"
            aria-label="关闭"
            onClick={onClose}
            disabled={busy}
          >
            <X size={20} />
          </button>
        </div>
        <label>
          学校 / 单位
          <input
            autoFocus
            required
            maxLength={100}
            value={input.organization}
            onChange={(e) => field("organization", e.target.value)}
            placeholder="兰州理工大学"
          />
        </label>
        <label>
          关键词
          <span className="optional">
            选填, 逗号分隔, 可填入学校缩写等, 方便搜索
          </span>
          <input
            maxLength={500}
            value={input.keywords}
            onChange={(e) => field("keywords", e.target.value)}
            placeholder="烂梨,兰理,兰理工,烂🍐"
          />
        </label>
        <label>
          群号
          <input
            required
            inputMode="numeric"
            pattern="[1-9][0-9]{4,11}"
            maxLength={12}
            title="5～12 位数字"
            value={input.qqNumber}
            onChange={(e) => field("qqNumber", e.target.value)}
            placeholder="123456789"
          />
        </label>
        <label>
          群链接
          <input
            required
            type="url"
            maxLength={2048}
            value={input.joinUrl}
            onChange={(e) => field("joinUrl", e.target.value)}
            placeholder="https://qm.qq.com/…"
          />
        </label>
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        <div className="modal-actions">
          <button
            type="button"
            className="secondary"
            onClick={onClose}
            disabled={busy}
          >
            取消
          </button>
          <button className="primary" disabled={busy}>
            {busy ? "保存中…" : "保存"}
          </button>
        </div>
      </form>
    </dialog>
  );
}

export function DeleteDialog({
  group,
  onClose,
  onDelete,
}: {
  group: Group;
  onClose: () => void;
  onDelete: () => Promise<void>;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const dialog = ref.current!;
    dialog.showModal();
    return () => dialog.close();
  }, []);
  return (
    <dialog
      ref={ref}
      className="modal delete-modal"
      onCancel={(e) => {
        if (busy) e.preventDefault();
        else onClose();
      }}
      aria-labelledby="delete-title"
    >
      <h2 id="delete-title">删除奶群？</h2>
      <p className="delete-name">
        {group.organization} · {group.qqNumber}
      </p>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      <div className="modal-actions">
        <button className="secondary" onClick={onClose} disabled={busy}>
          取消
        </button>
        <button
          className="danger"
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            try {
              await onDelete();
            } catch (e) {
              setError((e as Error).message);
            } finally {
              setBusy(false);
            }
          }}
        >
          {busy ? "删除中…" : "删除"}
        </button>
      </div>
    </dialog>
  );
}
