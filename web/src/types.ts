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
}
export interface GroupInput {
  organization: string;
  keywords: string;
  qqNumber: string;
  joinUrl: string;
}
