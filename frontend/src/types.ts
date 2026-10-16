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

// Demo verifier, see backend/rules.

export type AgeGroup = 'under21' | 'adult' | 'pension';

export interface Household {
  age_group: AgeGroup;
  partner: boolean;
  partner_pension_age: boolean;
  housemates: number;
}

export interface VerifierStartResponse {
  sessionPtr: unknown;
  frontendRequest?: unknown;
}

export interface IncomeItem {
  // "<credential>.<attribute>", e.g. "income.salary"
  attribute: string;
  // euro cents per month
  amount: number;
  article?: string;
  reason?: 'awir' | 'assessed' | 'negative';
}

export interface RuleStep {
  key: string;
  article?: string;
  // euro cents, or a count for cost_sharers
  amount: number;
}

export type Outcome = 'indication' | 'no_indication' | 'not_evaluated';

export interface RuleResult {
  rule: 'bijstand' | 'aio' | 'minima';
  outcome: Outcome;
  steps?: RuleStep[];
  notes?: string[];
}

export interface Evaluation {
  source: { law: string; bwb_id: string; version: string; url: string };
  counted: IncomeItem[];
  excluded: IncomeItem[];
  income: number;
  results: RuleResult[];
}

export interface VerifierResult {
  household: Household;
  disclosed: string[] | null;
  evaluation: Evaluation;
}
