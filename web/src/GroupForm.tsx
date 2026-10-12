import { useEffect, useRef, useState } from "react";
import { Upload, Undo2, X } from "lucide-react";
import {
  extractJoinLink,
  initialGroupInput,
  validateGroupInput,
} from "./group-input";
import { readGroupDraft, writeGroupDraft } from "./group-drafts";
import type { Group, GroupInput } from "./types";

export function GroupForm({
  accountID,
  group,
  onClose,
  onSave,
}: {
  accountID: number;
  group: Group | null;
  onClose: () => void;
  onSave: (input: GroupInput) => Promise<void>;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const qrGeneration = useRef(0);
  const [original] = useState(() => initialGroupInput(group));
  const [input, setInput] = useState<GroupInput>(
    () => readGroupDraft(accountID, group?.id) ?? original,
  );
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [reading, setReading] = useState(false);
  useEffect(() => {
    writeGroupDraft(accountID, group?.id, input);
  }, [accountID, group?.id, input]);
  useEffect(() => {
    const dialog = ref.current!;
    dialog.showModal();
    return () => {
      qrGeneration.current++;
      dialog.close();
    };
  }, []);
  const field = (key: keyof GroupInput, value: string) =>
    setInput((current) => ({ ...current, [key]: value }));
  const readQRCode = async (file: File) => {
    const generation = ++qrGeneration.current;
    setReading(true);
    setError("");
    try {
      const { decodeGroupQRCode } = await import("./qr");
      const link = await decodeGroupQRCode(file);
      if (qrGeneration.current === generation) field("joinUrl", link);
    } catch (error) {
      if (qrGeneration.current === generation)
        setError((error as Error).message);
    } finally {
      if (qrGeneration.current === generation) setReading(false);
    }
  };
  return (
    <dialog
      ref={ref}
      className="modal"
      onCancel={(e) => {
        if (busy) e.preventDefault();
        else onClose();
      }}
      onClick={(e) => {
        if (e.target !== ref.current || busy) return;
        const bounds = ref.current!.getBoundingClientRect();
        if (
          e.clientX < bounds.left ||
          e.clientX > bounds.right ||
          e.clientY < bounds.top ||
          e.clientY > bounds.bottom
        )
          onClose();
      }}
      aria-labelledby="form-title"
    >
      <form
        noValidate
        onSubmit={async (e) => {
          e.preventDefault();
          if (busy || reading) return;
          const validationError = validateGroupInput(input);
          if (validationError) {
            setError(validationError);
            return;
          }
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
            disabled={busy}
            value={input.organization}
            onChange={(e) => field("organization", e.target.value)}
            placeholder="兰州理工大学"
          />
        </label>
        <label>
          关键词
          <span id="keywords-hint" className="optional">
            选填, 逗号分隔, 可填入学校缩写等, 方便搜索
          </span>
          <input
            aria-label="关键词"
            aria-describedby="keywords-hint"
            disabled={busy}
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
            disabled={busy}
            pattern="[0-9]{5,11}"
            maxLength={11}
            title="5～11 位数字"
            value={input.qqNumber}
            onChange={(e) => field("qqNumber", e.target.value)}
            placeholder="123456789"
          />
        </label>
        <div className="group-link-field">
          <label htmlFor="group-link">群链接</label>
          <div className="link-input">
            <input
              id="group-link"
              required
              type="url"
              disabled={busy}
              maxLength={2048}
              value={input.joinUrl}
              onChange={(e) => field("joinUrl", e.target.value)}
              onPaste={(e) => {
                e.preventDefault();
                const text = e.clipboardData.getData("text");
                field("joinUrl", extractJoinLink(text) ?? text.trim());
              }}
              placeholder="https://qm.qq.com/q/…"
            />
            <button
              type="button"
              className="qr-upload"
              disabled={busy || reading}
              onClick={() => fileRef.current?.click()}
            >
              <Upload size={16} />
              {reading ? "解析中…" : "从二维码解析"}
            </button>
          </div>
          <input
            ref={fileRef}
            type="file"
            accept="image/*"
            aria-label="上传群二维码"
            hidden
            onChange={(e) => {
              const file = e.currentTarget.files?.[0];
              e.currentTarget.value = "";
              if (file) void readQRCode(file);
            }}
          />
        </div>
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        <div className="modal-actions">
          <button
            type="button"
            className="secondary undo-button"
            disabled={busy}
            onClick={() => {
              qrGeneration.current++;
              setReading(false);
              setInput({ ...original });
              setError("");
            }}
          >
            <Undo2 size={16} />
            撤销
          </button>
          <button
            type="button"
            className="secondary"
            onClick={onClose}
            disabled={busy}
          >
            取消
          </button>
          <button className="primary" disabled={busy || reading}>
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
