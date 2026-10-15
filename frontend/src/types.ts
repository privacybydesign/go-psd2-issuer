// Credential keys come from the definitions: one per E1 top-level group
// (income, housing, subsidy, ...) plus bankaccount.
export type CredentialKey = string;

export const BANK_ACCOUNT = 'bankaccount';

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

export interface Translated {
  en: string;
  nl: string;
}

export interface AttributeDefinition {
  id: string;
  // E1 category code; absent for attributes not taken from transactions
  e1?: string;
  name: Translated;
  description: Translated;
}

export interface CredentialDefinition {
  key: CredentialKey;
  e1_group?: string;
  name: Translated;
  description: Translated;
  attributes: AttributeDefinition[];
}

export interface Definitions {
  period_attributes: AttributeDefinition[];
  credentials: CredentialDefinition[];
}
