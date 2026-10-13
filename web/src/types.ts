export interface Group {
  id: number;
  organization: string;
  keywords: string[];
  qqNumber: string;
  joinUrl: string;
  updatedAt: string;
}
export interface User {
  id: number;
  nickname: string;
  avatarUrl: string;
  csrfToken: string;
  identities: LoginIdentity[];
  isAdmin?: boolean;
}
export interface AdminUser {
  id: number;
  nickname: string;
  avatarUrl: string;
  groupCount: number;
  siteCount: number;
  identities: LoginIdentity[];
}
export interface AdminGroup extends Group {
  ownerId: number;
}
export interface Site {
  id: number;
  name: string;
  domain: string;
  organization: string;
  updatedAt: string;
}
export interface AdminSite extends Site {
  ownerId: number;
}
export interface SiteInput {
  name: string;
  domain: string;
  organization: string;
}
export interface LoginIdentity {
  providerId: string;
  nickname: string;
  avatarUrl: string;
}
export interface LoginProvider {
  id: string;
  name: string;
  icon: string;
  loginUrl: string;
}
export interface GroupInput {
  organization: string;
  keywords: string;
  qqNumber: string;
  joinUrl: string;
}

export interface AdminStats {
  users: number;
  groups: number;
  sites: number;
  contributors: number;
  organizations: number;
  daily: { date: string; users: number; groups: number; sites: number }[];
}
