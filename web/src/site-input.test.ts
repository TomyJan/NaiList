import { describe, expect, it } from "vitest";
import {
  displayDomain,
  filterSites,
  normalizeDomain,
  validateSiteInput,
} from "./site-input";

describe("奶站域名", () => {
  it.each([
    ["https://www.NAI.example.com/path?q=1#top", "nai.example.com"],
    [" www.example.edu.cn/学校 ", "example.edu.cn"],
    ["http://EXAMPLE.com", "example.com"],
    ["example.com?utm=1", "example.com"],
    ["example.com#here", "example.com"],
    ["sub.example.com", "sub.example.com"],
    ["https://www.甘肃奶龙大学.com/首页", "xn--psszg58cy52cu6lj57b.com"],
    ["https://www.例子.中国/首页", "xn--fsqu00a.xn--fiqs8s"],
    ["example.中国", "example.xn--fiqs8s"],
  ])("从 %s 保留域名", (value, expected) => {
    expect(normalizeDomain(value)).toBe(expected);
  });
  it.each([
    "localhost",
    "127.0.0.1",
    "[::1]",
    "user@example.com",
    "example.com:443",
    "bad_domain.com",
    "-bad.example.com",
    "bad-.example.com",
    "example..com",
    "example.c",
    "example.123",
    "https://example.com",
    "example.com/path",
    `${"a".repeat(64)}.com`,
    "example.com\\evil",
    "example.com.",
    "例子.com:443",
    "user@例子.com",
    "例子%2ecom",
    "例子.com\\evil",
    "例 子.com",
    "example.xn--",
    "example.xn--a",
    "example.xn--abc-",
    `example.xn--${"a".repeat(60)}`,
  ])("拒绝非法域名 %s", (domain) => {
    expect(
      validateSiteInput({ name: "奶站", organization: "大学", domain }),
    ).toBeTruthy();
  });
  it("长度按 Unicode 字符计算", () => {
    const input = {
      name: "🍐".repeat(32),
      organization: "校".repeat(32),
      domain: "site.example.com",
    };
    expect(validateSiteInput(input)).toBeNull();
    expect(validateSiteInput({ ...input, name: `${input.name}梨` })).toContain(
      "32",
    );
    expect(
      validateSiteInput({ ...input, organization: `${input.organization}校` }),
    ).toContain("32");
  });
  it("中文域名与 Punycode 使用相同的规范域名", () => {
    const domain = normalizeDomain("甘肃奶龙大学.com");
    expect(domain).toBe("xn--psszg58cy52cu6lj57b.com");
    expect(normalizeDomain(domain)).toBe(domain);
    expect(
      validateSiteInput({
        name: "甘肃奶龙大学",
        organization: "兰州理工大学",
        domain,
      }),
    ).toBeNull();
  });
  it("所有字段必填", () => {
    const input = { name: "奶站", organization: "大学", domain: "example.com" };
    for (const field of ["name", "organization", "domain"] as const)
      expect(validateSiteInput({ ...input, [field]: " " })).toBeTruthy();
  });
  it.each([
    ["例子.中国", "xn--fsqu00a.xn--fiqs8s"],
    ["example.中国", "example.xn--fiqs8s"],
    ["xn--fsqu00a.xn--fiqs8s", "xn--fsqu00a.xn--fiqs8s"],
  ])("中文后缀 %s 可规范化并通过校验", (value, expected) => {
    const domain = normalizeDomain(value);
    expect(domain).toBe(expected);
    expect(
      validateSiteInput({ name: "奶站", organization: "大学", domain }),
    ).toBeNull();
  });
});
describe("奶站本地搜索", () => {
  const sites = [
    {
      id: 1,
      name: "兰理奶站",
      organization: "兰州理工大学",
      domain: "lut.example.com",
      updatedAt: "",
    },
  ];
  it.each(["兰理", "理工大学", " LUT.EXAMPLE "])("搜索 %s", (query) =>
    expect(filterSites(sites, query)).toEqual(sites),
  );
  it("空搜索保留顺序，未匹配返回空", () => {
    expect(filterSites(sites, " ")).toEqual(sites);
    expect(filterSites(sites, "其他")).toEqual([]);
  });
  it.each(["甘肃奶龙大学", "xn--psszg58cy52cu6lj57b.com"])(
    "搜索中文域名 %s",
    (query) => {
      const sites = [
        {
          id: 2,
          name: "独立站点",
          organization: "学校",
          domain: "xn--psszg58cy52cu6lj57b.com",
          updatedAt: "",
        },
      ];
      expect(filterSites(sites, query)).toEqual(sites);
    },
  );
});

describe("域名显示", () => {
  it.each([
    ["xn--psszg58cy52cu6lj57b.com", "甘肃奶龙大学.com"],
    ["xn--fsqu00a.xn--fiqs8s", "例子.中国"],
    ["example.xn--fiqs8s", "example.中国"],
    ["lut.example.com", "lut.example.com"],
    ["甘肃奶龙大学.com", "甘肃奶龙大学.com"],
    ["xn--.com", "xn--.com"],
    ["", ""],
  ])("显示 %s", (domain, expected) => {
    expect(displayDomain(domain)).toBe(expected);
  });
});
