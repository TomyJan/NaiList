export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}

export async function api<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const response = await fetch(path, {
    credentials: "same-origin",
    ...options,
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: "请求失败" }));
    throw new ApiError(error.error || "请求失败", response.status);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}
export function writeOptions(
  method: string,
  csrf: string,
  body?: unknown,
): RequestInit {
  return {
    method,
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  };
}
