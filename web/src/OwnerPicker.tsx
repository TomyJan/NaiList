import { useState } from "react";
import type { AdminUser } from "./types";

export function OwnerPicker({
  users,
  ownerId,
  disabled,
  onChange,
}: {
  users: AdminUser[];
  ownerId?: number;
  disabled: boolean;
  onChange: (ownerId: number) => void;
}) {
  const [query, setQuery] = useState("");
  const selected = users.find((owner) => owner.id === ownerId);
  const visible = users.filter((owner) =>
    `${owner.nickname} ${owner.id}`
      .toLocaleLowerCase()
      .includes(query.trim().toLocaleLowerCase()),
  );
  return (
    <fieldset className="owner-picker" disabled={disabled}>
      <legend>所属账号</legend>
      <p className="owner-selected">
        当前选择：
        {selected
          ? `${selected.nickname}（ID：${selected.id}）`
          : `账号不可用（ID：${ownerId}），请重新选择`}
      </p>
      <input
        aria-label="搜索所属账号"
        placeholder="搜索昵称或账号 ID"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
      />
      <div className="owner-options" aria-label="所属账号选项">
        {visible.map((owner) => (
          <button
            type="button"
            key={owner.id}
            className={
              owner.id === ownerId ? "owner-option selected" : "owner-option"
            }
            aria-label={`选择 ${owner.nickname}（ID：${owner.id}）`}
            aria-pressed={owner.id === ownerId}
            onClick={() => onChange(owner.id)}
          >
            {owner.nickname}（ID：{owner.id}）
          </button>
        ))}
        {!visible.length && <p>没有匹配的账号</p>}
      </div>
    </fieldset>
  );
}
