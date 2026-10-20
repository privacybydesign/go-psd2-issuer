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

// Declared by the user; the age comes from the date of birth credential.
export interface Household {
  partner: boolean;
  partner_pension_age: boolean;
  housemates: number;
}

export interface VerifierLaw {
  id: string;
  // the attributes the law asks for, e.g. "income.salary"; the date of birth
  // comes on top
  attributes: string[];
  needs_tax_assessment: boolean;
  // the postcode is asked for on top of the attributes
  needs_postcode: boolean;
  // keys of the criteria the check does and does not establish
  checked: string[];
  not_checked: string[];
}

export interface VerifierStartResponse {
  sessionPtr: unknown;
  frontendRequest?: unknown;
}

export interface Source {
  id: string;
  name: string;
  version: string;
  url: string;
}

export interface IncomeItem {
  // "<credential>.<attribute>", e.g. "income.salary", or "housing"
  attribute: string;
  // euro cents per month
  amount: number;
  law?: string;
  article?: string;
  reason?: 'negative';
}

export interface RuleStep {
  key: string;
  law?: string;
  article?: string;
  // euro cents, a count for cost_sharers or a percentage for
  // cost_of_living_percentage
  amount: number;
}

export type CriterionStatus = 'met' | 'not_met' | 'partial' | 'counted' | 'not_checked';

export interface Criterion {
  key: string;
  status: CriterionStatus;
  law?: string;
  article?: string;
  // what a checked criterion compares, in euro cents per month
  amount?: number;
  limit?: number;
  // what a checked criterion that is no amount was based on, e.g. the
  // postcode
  value?: string;
}

export type Outcome = 'indication' | 'partial' | 'no_indication' | 'not_evaluated';

export interface RuleResult {
  rule: string;
  outcome: Outcome;
  criteria: Criterion[];
  steps?: RuleStep[];
  notes?: string[];
}

export interface Evaluation {
  law: string;
  sources: Source[];
  counted: IncomeItem[];
  excluded: IncomeItem[];
  expenses?: IncomeItem[];
  income: number;
  result: RuleResult;
}

export interface VerifierResult {
  household: Household;
  age_group: AgeGroup;
  tax_assessment?: number;
  evaluation: Evaluation;
}
