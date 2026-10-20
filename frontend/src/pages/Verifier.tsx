import React, { ReactNode, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, getVerifierLaws, startVerification, verificationResult } from '../api';
import { Alert, translate, useDefinitions } from '../components';
import { Criterion, CriterionStatus, Definitions, Evaluation, Household, IncomeItem, RuleResult, RuleStep, VerifierLaw, VerifierResult } from '../types';

// articleUrl links e.g. "22a.1.c" of a source to e.g. Artikel22a of the
// version used.
export const articleUrl = (evaluation: Evaluation, law: string | undefined, article: string) => {
  const source = evaluation.sources.find((s) => s.id === law);
  if (!source) return undefined;
  // a local regulation has no anchors per article
  return source.url.includes('wetten.overheid.nl') ? `${source.url}#Artikel${article.split('.')[0]}` : source.url;
};

function useEuro() {
  const { i18n } = useTranslation();
  const euro = new Intl.NumberFormat(i18n.language === 'en' ? 'en-GB' : 'nl-NL', { style: 'currency', currency: 'EUR' });
  return (cents: number) => euro.format(cents / 100);
}

// GemeentePage lays out the verifier as a page of the municipality that
// runs it: a fictitious one, since this is a demo.
function GemeentePage({ title, children, footer }: { title: string; children: ReactNode; footer?: ReactNode }) {
  const { t } = useTranslation();
  return (
    <div id="container" className="gemeente">
      <header className="gemeente-header">
        <div className="gemeente-bar">
          <div className="gemeente-brand">
            <svg className="gemeente-logo" viewBox="0 0 40 48" aria-hidden="true">
              <path d="M4 4h32v20c0 12-8 19-16 22C12 43 4 36 4 24z" fill="#FFFFFF" />
              <path d="M8 8h24v16c0 9-6 14.5-12 17C14 38.5 8 33 8 24z" fill="currentColor" />
              <path d="M13 31V19h3v-3h2.5v3h3v-3H24v3h3v12h-5v-5a2 2 0 0 0-4 0v5z" fill="#FFFFFF" />
            </svg>
            <span className="gemeente-name">{t('gemeente_name')}</span>
          </div>
          <span className="gemeente-demo">{t('gemeente_demo')}</span>
        </div>
        <nav className="gemeente-crumbs" aria-label="breadcrumb">
          <span>{t('gemeente_home')}</span>
          <span aria-hidden="true">›</span>
          <span>{t('gemeente_section')}</span>
          <span aria-hidden="true">›</span>
          <span aria-current="page">{title}</span>
        </nav>
      </header>
      <main>
        <div className="content gemeente-content">
          <h1>{title}</h1>
          {children}
          <p className="gemeente-colofon">{t('gemeente_footer')}</p>
        </div>
      </main>
      <footer>
        <div className="actions">{footer}</div>
      </footer>
    </div>
  );
}

function Article({ evaluation, law, article }: { evaluation: Evaluation; law?: string; article?: string }) {
  const { t } = useTranslation();
  if (!article) return null;
  const url = articleUrl(evaluation, law, article);
  const name = evaluation.sources.find((s) => s.id === law)?.name;
  const text = t('verifier_article', { article }) + (name ? ` ${name}` : '');
  return url ? <a href={url} target="_blank" rel="noreferrer" className="article">{text}</a> : <span className="article">{text}</span>;
}

export function attributeName(definitions: Definitions | undefined, attribute: string, language: string, t: (key: string) => string) {
  const [key, id] = attribute.split('.');
  if (!id) return t(`verifier_attribute_${key}`);
  const credential = definitions?.credentials.find((c) => c.key === key);
  const name = translate(credential?.attributes.find((a) => a.id === id)?.name, language);
  return name ? `${translate(credential?.name, language)}: ${name}` : attribute;
}

function ItemTable({ evaluation, items, definitions }: { evaluation: Evaluation; items: IncomeItem[]; definitions?: Definitions }) {
  const { t, i18n } = useTranslation();
  const euro = useEuro();
  return (
    <table className="attributes">
      <tbody>
        {items.map((item) => (
          <tr key={item.attribute}>
            <th>
              {attributeName(definitions, item.attribute, i18n.language, t)}
              <div className="reason">
                {item.reason && t(`verifier_reason_${item.reason}`)} <Article evaluation={evaluation} law={item.law} article={item.article} />
              </div>
            </th>
            <td>{euro(item.amount)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function stepValue(step: RuleStep, euro: (cents: number) => string) {
  if (step.key === 'cost_sharers') return String(step.amount);
  if (step.key === 'cost_of_living_percentage') return `${step.amount}%`;
  return euro(step.amount);
}

const STATUS_ICON: Record<CriterionStatus, string> = { met: '✓', not_met: '✕', partial: '±', counted: '✓', not_checked: '?' };

function householdText(household: Household, t: (key: string, options?: Record<string, unknown>) => string) {
  const partner = !household.partner
    ? t('verifier_household_single')
    : t(household.partner_pension_age ? 'verifier_household_partner_pension' : 'verifier_household_partner');
  return household.housemates > 0 ? `${partner}, ${t('verifier_household_housemates', { count: household.housemates })}` : partner;
}

// CriterionItem shows a condition of the scheme with its status: for a
// checked one what was compared, otherwise what it is about.
function CriterionItem({ criterion, evaluation, household }: { criterion: Criterion; evaluation?: Evaluation; household?: Household }) {
  const { t, i18n } = useTranslation();
  const euro = useEuro();
  const { key, status } = criterion;
  let detail = t(`verifier_criterion_${key}_help`);
  if (key === 'household' && household) {
    detail = t('verifier_criterion_household_declared', { household: householdText(household, t) });
  } else if (status !== 'not_checked' && criterion.value) {
    detail = t(`verifier_criterion_${key}_value`, { value: criterion.value });
  } else if (status !== 'not_checked' && i18n.exists(`verifier_criterion_${key}_amounts`)) {
    detail = t(`verifier_criterion_${key}_amounts`, { amount: euro(criterion.amount ?? 0), limit: euro(criterion.limit ?? 0) });
  }
  return (
    <li className={`criterion status-${status}`}>
      <span className="status-icon" aria-hidden="true">{STATUS_ICON[status]}</span>
      <div className="criterion-body">
        <div className="criterion-title">{t(`verifier_criterion_${key}`)}</div>
        <div className="criterion-detail">
          {detail}
          {evaluation && <> <Article evaluation={evaluation} law={criterion.law} article={criterion.article} /></>}
        </div>
      </div>
      <span className="status-label">{t(`verifier_status_${status}`)}</span>
    </li>
  );
}

const TONE: Record<RuleResult['outcome'], string> = {
  indication: 'positive',
  partial: 'partial',
  no_indication: 'negative',
  not_evaluated: 'neutral',
};

// keyFigure is the amount that matters most for the outcome, if any.
function keyFigure(rule: RuleResult, euro: (cents: number) => string, t: (key: string, options?: Record<string, unknown>) => string) {
  const amount = (key: string) => rule.steps?.find((s) => s.key === key)?.amount;
  const supplement = amount('supplement');
  if (rule.outcome === 'indication' && supplement !== undefined) return t('verifier_key_supplement', { amount: euro(supplement) });
  if (rule.outcome === 'partial') {
    const remitted = amount('remitted');
    if (remitted !== undefined) return t('verifier_key_remitted', { amount: euro(remitted) });
    const payable = amount('payable_per_year');
    if (payable !== undefined) return t('verifier_key_payable', { amount: euro(payable) });
  }
  return undefined;
}

// Verdict states the outcome, and what it is based on: the conditions that
// were checked (costs that were counted are no conditions) and how many
// were not.
function Verdict({ rule, conditions, unchecked }: { rule: RuleResult; conditions: number; unchecked: number }) {
  const { t } = useTranslation();
  const euro = useEuro();
  const authority = t(`verifier_authority_${rule.rule}`);
  const evaluated = rule.outcome !== 'not_evaluated';
  const figure = keyFigure(rule, euro, t);
  const basis = !evaluated
    ? t('verifier_basis_not_evaluated')
    : rule.outcome === 'no_indication'
      ? t('verifier_basis_not_met')
      : t('verifier_basis_met', { count: conditions, unchecked, authority });
  return (
    <section className={`verdict tone-${TONE[rule.outcome]}`} aria-live="polite">
      <p className="verdict-scheme">{t(`verifier_rule_${rule.rule}`)}</p>
      <h2 className="verdict-title">
        {evaluated ? t(`verifier_verdict_${rule.rule}_${rule.outcome}`) : t('verifier_verdict_not_evaluated')}
      </h2>
      {evaluated && <p className="verdict-outcome">{t(`verifier_outcome_${rule.rule}_${rule.outcome}`)}</p>}
      {figure && <p className="verdict-figure">{figure}</p>}
      <p className="verdict-basis">{basis}</p>
    </section>
  );
}

export function VerifierResultView({ result, definitions }: { result: VerifierResult; definitions?: Definitions }) {
  const { t } = useTranslation();
  const euro = useEuro();
  const { evaluation } = result;
  const rule = evaluation.result;
  const checked = rule.criteria.filter((c) => c.status !== 'not_checked');
  const unchecked = rule.criteria.filter((c) => c.status === 'not_checked');
  const authority = t(`verifier_authority_${rule.rule}`);
  return (
    <>
      <Verdict rule={rule} conditions={checked.filter((c) => c.status !== 'counted').length} unchecked={unchecked.length} />

      {checked.length > 0 && (
        <section className="criteria-section">
          <h2>{t('verifier_checked_header')}</h2>
          <p className="section-intro">{t('verifier_checked_intro')}</p>
          <ul className="criteria">
            {checked.map((c) => <CriterionItem key={c.key} criterion={c} evaluation={evaluation} household={result.household} />)}
          </ul>
          <p className="privacy-note">{t('verifier_age_group', { age: t(`verifier_age_${result.age_group}`) })}</p>
        </section>
      )}

      {unchecked.length > 0 && (
        <section className="criteria-section">
          <h2>{t('verifier_not_checked_header')}</h2>
          <p className="section-intro">{t('verifier_not_checked_intro', { authority })}</p>
          <ul className="criteria">
            {unchecked.map((c) => <CriterionItem key={c.key} criterion={c} evaluation={evaluation} household={result.household} />)}
          </ul>
        </section>
      )}

      {rule.notes && rule.notes.length > 0 && (
        <section className="criteria-section">
          <h2>{t('verifier_notes_header')}</h2>
          <ul className="notes">
            {rule.notes.map((note) => <li key={note}>{t(`verifier_${note}`)}</li>)}
          </ul>
        </section>
      )}

      <section className="info-box">
        <h2>{t('verifier_discretion_header')}</h2>
        <p>{t(`verifier_discretion_${rule.rule}`)}</p>
        <h2>{t('verifier_next_header')}</h2>
        <p>{t(`verifier_next_${rule.rule}`)}</p>
      </section>

      <details className="calculation">
        <summary>{t('verifier_calculation')}</summary>
        {rule.steps && (
          <table className="attributes">
            <tbody>
              {rule.steps.map((step) => (
                <tr key={step.key}>
                  <th>
                    {t(`verifier_step_${step.key}`)}
                    <div className="reason"><Article evaluation={evaluation} law={step.law} article={step.article} /></div>
                  </th>
                  <td>{stepValue(step, euro)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <h3>{t('verifier_income_header', { amount: euro(evaluation.income) })}</h3>
        {evaluation.counted.length > 0
          ? <ItemTable evaluation={evaluation} items={evaluation.counted} definitions={definitions} />
          : <p>{t('verifier_no_income')}</p>}
        {evaluation.expenses && evaluation.expenses.length > 0 && (
          <>
            <h3>{t('verifier_expenses_header')}</h3>
            <ItemTable evaluation={evaluation} items={evaluation.expenses} definitions={definitions} />
          </>
        )}
        {evaluation.excluded.length > 0 && (
          <>
            <h3>{t('verifier_excluded_header')}</h3>
            <ItemTable evaluation={evaluation} items={evaluation.excluded} definitions={definitions} />
          </>
        )}
        <h3>{t('verifier_sources_header')}</h3>
        <ul className="sources">
          {evaluation.sources.map((source) => (
            <li key={source.id}>
              <a href={source.url} target="_blank" rel="noreferrer">{source.name}</a>{' '}
              {t('verifier_source_version', { version: source.version })}
            </li>
          ))}
        </ul>
      </details>

      <p className="disclaimer">{t('verifier_disclaimer')}</p>
    </>
  );
}

function LawChoice({ law, selected, onSelect, definitions }: { law: VerifierLaw; selected: boolean; onSelect: () => void; definitions?: Definitions }) {
  const { t, i18n } = useTranslation();
  return (
    <li className={`credential-card law-card ${selected ? 'selected' : 'unselected'}`}>
      <label className="select">
        <input type="radio" name="law" checked={selected} onChange={onSelect} />
        <div>
          <h2>{t(`verifier_law_${law.id}`)}</h2>
          <p>{t(`verifier_law_${law.id}_description`)}</p>
        </div>
      </label>
      {selected && (
        <>
          <div className="law-preview">
            <div>
              <h3>{t('verifier_preview_checked')}</h3>
              <ul className="preview-list checked">
                {law.checked.map((key) => <li key={key}>{t(`verifier_criterion_${key}`)}</li>)}
              </ul>
            </div>
            <div>
              <h3>{t('verifier_preview_not_checked')}</h3>
              <ul className="preview-list not-checked">
                {law.not_checked.map((key) => <li key={key}>{t(`verifier_criterion_${key}`)}</li>)}
              </ul>
            </div>
          </div>
          <details className="requested">
            <summary>{t('verifier_requested', { count: law.attributes.length + 1 + (law.needs_postcode ? 1 : 0) })}</summary>
            <ul>
              {law.attributes.map((attribute) => <li key={attribute}>{attributeName(definitions, attribute, i18n.language, t)}</li>)}
              <li>{t('verifier_date_of_birth')}</li>
              {law.needs_postcode && <li>{t('verifier_postcode')}</li>}
            </ul>
          </details>
        </>
      )}
    </li>
  );
}

export default function VerifierPage() {
  const { t, i18n } = useTranslation();
  const { definitions } = useDefinitions();
  const [laws, setLaws] = useState<VerifierLaw[] | undefined>();
  const [lawId, setLawId] = useState<string>('bijstand');
  const [household, setHousehold] = useState<Household>({ partner: false, partner_pension_age: false, housemates: 0 });
  const [taxAssessment, setTaxAssessment] = useState<number | undefined>();
  const [busy, setBusy] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | undefined>();
  const [result, setResult] = useState<VerifierResult | undefined>();

  useEffect(() => {
    getVerifierLaws()
      .then(setLaws)
      .catch((err) => setErrorMessage(t(err instanceof ApiError ? err.code : 'verifier_error')));
  }, [t]);

  const law = laws?.find((l) => l.id === lawId);

  const update = (change: Partial<Household>) => setHousehold((h) => {
    const next = { ...h, ...change };
    return { ...next, partner_pension_age: next.partner && next.partner_pension_age };
  });

  const verify = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!law) return;
    setBusy(true);
    setErrorMessage(undefined);
    try {
      // started by the backend, which keeps the requestor token and fetches
      // the result itself
      const session = await startVerification(law.id, household, law.needs_tax_assessment ? taxAssessment : undefined);
      const yivi = await import('@privacybydesign/yivi-frontend');
      const popup = yivi.newPopup({
        language: i18n.language as 'nl' | 'en',
        session: {
          start: false,
          mapping: {
            sessionPtr: () => session.sessionPtr as never,
            frontendRequest: () => session.frontendRequest as never,
          },
          result: false,
        },
      });
      await popup.start();
      setResult(await verificationResult());
      window.scrollTo(0, 0);
    } catch (err) {
      if (err === 'Aborted') {
        setErrorMessage(t('verifier_cancel'));
      } else if (err instanceof ApiError && err.code === 'error_ratelimit' && err.retryAfter) {
        const time = new Date(Date.now() + err.retryAfter * 1000).toLocaleTimeString('nl-NL', { timeZone: 'Europe/Amsterdam' });
        setErrorMessage(t('error_ratelimit', { time }));
      } else {
        setErrorMessage(t(err instanceof ApiError ? err.code : 'verifier_error'));
      }
    } finally {
      setBusy(false);
    }
  };

  if (result) {
    return (
      <GemeentePage
        title={t('verifier_result_header')}
        footer={<><button type="button" id="back-button" onClick={() => setResult(undefined)}>{t('again')}</button><div></div></>}
      >
        <VerifierResultView result={result} definitions={definitions} />
      </GemeentePage>
    );
  }

  return (
    <form onSubmit={verify} style={{ height: '100%' }}>
      <GemeentePage
        title={t('verifier_header')}
        footer={<><div></div><button id="submit-button" type="submit" disabled={busy || !law}>{t('verifier_start')}</button></>}
      >
        {errorMessage && <Alert kind="danger">{errorMessage}</Alert>}
        <p className="lead">{t('verifier_explanation')}</p>
        <fieldset className="household">
          <legend>{t('verifier_law')}</legend>
          <ul className="credential-list">
            {laws?.map((l) => (
              <LawChoice key={l.id} law={l} selected={l.id === lawId} onSelect={() => setLawId(l.id)} definitions={definitions} />
            ))}
          </ul>
        </fieldset>
        <fieldset className="household">
          <legend>{t('verifier_household')}</legend>
          <p className="privacy-note">{t('verifier_household_explanation')}</p>
          <label className="choice">
            <input type="checkbox" checked={household.partner} onChange={(e) => update({ partner: e.target.checked })} />
            {t('verifier_partner')}
          </label>
          {household.partner && (
            <label className="choice">
              <input type="checkbox" checked={household.partner_pension_age} onChange={(e) => update({ partner_pension_age: e.target.checked })} />
              {t('verifier_partner_pension_age')}
            </label>
          )}
          <label htmlFor="housemates">{t('verifier_housemates')}</label>
          <input
            id="housemates"
            type="number"
            min={0}
            max={20}
            value={household.housemates}
            onChange={(e) => update({ housemates: Math.max(0, Math.min(20, parseInt(e.target.value) || 0)) })}
          />
          <p className="privacy-note">{t('verifier_housemates_help')}</p>
        </fieldset>
        {law?.needs_tax_assessment && (
          <fieldset className="household">
            <legend>{t('verifier_tax_assessment')}</legend>
            <label htmlFor="tax-assessment">{t('verifier_tax_assessment_label')}</label>
            <input
              id="tax-assessment"
              type="number"
              min={0}
              max={100000}
              value={taxAssessment ?? ''}
              onChange={(e) => {
                const value = parseInt(e.target.value);
                setTaxAssessment(isNaN(value) ? undefined : Math.max(0, Math.min(100000, value)));
              }}
            />
            <p className="privacy-note">{t('verifier_tax_assessment_help')}</p>
          </fieldset>
        )}
        <p className="privacy-note">{t('verifier_privacy')}</p>
      </GemeentePage>
    </form>
  );
}
