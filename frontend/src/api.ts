import { Definitions, IssueResponse, StatusResponse } from './types';

// ApiError carries the backend error code (e.g. "error:session-not-found")
// converted to a translation key (e.g. "error_session_not_found").
export class ApiError extends Error {
  constructor(public code: string, public retryAfter?: number) {
    super(code);
  }
}

export const errorKey = (code: string) =>
  code.trim().replaceAll('-', '_').replaceAll(':', '_').toLowerCase();

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    const text = await response.text();
    const retryAfter = response.headers.get('Retry-After');
    throw new ApiError(
      text.startsWith('error:') ? errorKey(text) : 'error_internal',
      retryAfter ? parseInt(retryAfter) : undefined,
    );
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return response.json() as Promise<T>;
}

export const startSession = () =>
  request<{ url: string }>('POST', '/api/session/start', {});

export const submitConsent = (consentId: string, state: string) =>
  request<StatusResponse>('POST', '/api/session/consent', { consent_id: consentId, state });

export const getStatus = () =>
  request<StatusResponse>('GET', '/api/session/status');

export const issue = (credentials: string[]) =>
  request<IssueResponse>('POST', '/api/session/issue', { credentials });

export const deleteSession = () =>
  request<void>('DELETE', '/api/session');

// The definitions never change while the page is open, so fetch them once.
let definitions: Promise<Definitions> | undefined;
export const getDefinitions = () => {
  definitions ??= request<Definitions>('GET', '/api/definitions').catch((err) => {
    definitions = undefined;
    throw err;
  });
  return definitions;
};
