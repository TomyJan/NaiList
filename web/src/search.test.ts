import { describe, it, expect } from "vitest";
import { filterGroups } from "./search";
const groups = [
  {
    id: 1,
    organization: "兰州理工大学",
    keywords: ["烂梨", "兰理", "兰理工", "烂🍐"],
    qqNumber: "123456789",
    joinUrl: "https://qm.qq.com/q/test",
    updatedAt: "",
  },
  {
    id: 2,
    organization: "Acme 单位",
    keywords: [],
    qqNumber: "987654321",
    joinUrl: "https://qm.qq.com/q/other",
    updatedAt: "",
  },
];
describe("本地搜索", () => {
  it.each(["烂梨", "兰理", "兰理工", "烂🍐", " 兰州 ", "45678"])(
    "匹配 %s",
    (q) => expect(filterGroups(groups, q).map((g) => g.id)).toEqual([1]),
  );
  it("大小写不敏感", () => expect(filterGroups(groups, "ACME")[0].id).toBe(2));
  it("空搜索返回全部且不修改数据", () => {
    expect(filterGroups(groups, " ")).toEqual(groups);
    expect(groups).toHaveLength(2);
  });
  it("未匹配返回空", () => expect(filterGroups(groups, "不存在")).toEqual([]));
});
