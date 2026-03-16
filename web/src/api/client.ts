import type { ApiError } from './types';

export class ApiRequestError extends Error {
  public statusCode: number;
  public errorCode: string;
  public errorName: string;
  public errorInstanceId: string;
  public parameters?: Record<string, string>;

  constructor(error: ApiError) {
    super(`${error.errorCode}: ${error.errorName}`);
    this.name = 'ApiRequestError';
    this.statusCode = error.statusCode;
    this.errorCode = error.errorCode;
    this.errorName = error.errorName;
    this.errorInstanceId = error.errorInstanceId;
    this.parameters = error.parameters;
  }
}

export async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const options: RequestInit = {
    method,
    headers: {
      'Content-Type': 'application/json',
    },
  };

  if (body !== undefined) {
    options.body = JSON.stringify(body);
  }

  const response = await fetch(path, options);

  if (!response.ok) {
    let errorData: Partial<ApiError>;
    try {
      errorData = await response.json();
    } catch {
      errorData = {};
    }
    throw new ApiRequestError({
      errorCode: errorData.errorCode ?? 'UNKNOWN',
      errorName: errorData.errorName ?? response.statusText,
      errorInstanceId: errorData.errorInstanceId ?? '',
      parameters: errorData.parameters,
      statusCode: response.status,
    });
  }

  const text = await response.text();
  if (!text) {
    return undefined as T;
  }

  return JSON.parse(text) as T;
}
