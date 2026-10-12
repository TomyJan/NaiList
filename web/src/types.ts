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
