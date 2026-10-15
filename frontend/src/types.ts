export type CredentialKey = 'income' | 'housing' | 'debts' | 'benefits' | 'bankaccount';

export const CREDENTIAL_KEYS: CredentialKey[] = ['income', 'housing', 'debts', 'benefits', 'bankaccount'];

export interface Credential {
  key: CredentialKey;
  attributes: Record<string, string>;
}

export type SessionStatus =
  | 'awaiting_consent'
  | 'awaiting_transactions'
  | 'categorizing'
  | 'ready'
  | 'failed';

export interface StatusResponse {
  status: SessionStatus;
  error?: string;
  credentials?: Credential[];
}

export interface IssueResponse {
  jwt: string;
  irma_server_url: string;
}
