import type { APIErrorBody } from "./types";

export class APIError extends Error {
  constructor(
    readonly status: number,
    readonly body: APIErrorBody,
  ) {
    super(body.message);
  }
}

export interface APIClient {
  read<T>(path: string, token: string): Promise<T>;
}

export const apiClient: APIClient = {
  async read<T>(path: string, token: string): Promise<T> {
    const response = await fetch(path, {
      headers: { Authorization: `Bearer ${token}` },
      credentials: "same-origin",
    });
    const body = (await response.json()) as T | APIErrorBody;
    if (!response.ok) {
      throw new APIError(response.status, body as APIErrorBody);
    }
    return body as T;
  },
};
