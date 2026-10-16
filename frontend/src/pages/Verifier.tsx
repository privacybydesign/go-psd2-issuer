import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, startVerification, verificationResult } from '../api';
import { Alert, Page, translate, useDefinitions } from '../components';
import { AgeGroup, Definitions, Evaluation, Household, IncomeItem, RuleResult, VerifierResult } from '../types';

const AGE_GROUPS: AgeGroup[] = ['adult', 'pension', 'under21'];

// articleUrl links e.g. "22a.1.c" to Artikel22a of the law version used.
export const articleUrl = (evaluation: Evaluation, article: string) =>
  `${evaluation.source.url}#Artikel${article.split('.')[0]}`;

function useEuro() {
  const { i18n } = useTranslation();
  const euro = new Intl.NumberFormat(i18n.language === 'en' ? 'en-GB' : 'nl-NL', { style: 'currency', currency: 'EUR' });
  return (cents: number) => euro.format(cents / 100);
}

function Article({ evaluation, article }: { evaluation: Evaluation; article?: string }) {
  const { t } = useTranslation();
  if (!article) return null;
  return (
    <a href={articleUrl(evaluation, article)} target="_blank" rel="noreferrer" className="article">
      {t('verifier_article', { article })}
    </a>
  );
}

function attributeName(definitions: Definitions | undefined, attribute: string, language: string) {
  const [key, id] = attribute.split('.');
  const credential = definitions?.credentials.find((c) => c.key === key);
  const name = translate(credential?.attributes.find((a) => a.id === id)?.name, language);
  return name ? `${translate(credential?.name, language)}: ${name}` : attribute;
}

function IncomeTable({ evaluation, items, definitions }: { evaluation: Evaluation; items: IncomeItem[]; definitions?: Definitions }) {
  const { t, i18n } = useTranslation();
  const euro = useEuro();
  return (
    <table className="attributes">
      <tbody>
        {items.map((item) => (
          <tr key={item.attribute}>
            <th>
              {attributeName(definitions, item.attribute, i18n.language)}
              <div className="reason">
                {item.reason && t(`verifier_reason_${item.reason}`)} <Article evaluation={evaluation} article={item.article} />
              </div>
            </th>
            <td>{euro(item.amount)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function RuleCard({ evaluation, result }: { evaluation: Evaluation; result: RuleResult }) {
  const { t } = useTranslation();
  const euro = useEuro();
  return (
    <li className={`credential-card rule-card outcome-${result.outcome}`}>
      <h2>{t(`verifier_rule_${result.rule}`)}</h2>
      <p className="outcome">{t(`verifier_outcome_${result.rule}_${result.outcome}`)}</p>
      {result.steps && (
        <table className="attributes">
          <tbody>
            {result.steps.map((step) => (
              <tr key={step.key}>
                <th>
                  {t(`verifier_step_${step.key}`)}
                  <div className="reason"><Article evaluation={evaluation} article={step.article} /></div>
                </th>
                <td>{step.key === 'cost_sharers' ? step.amount : euro(step.amount)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {result.notes && (
        <ul className="notes">
          {result.notes.map((note) => <li key={note}>{t(`verifier_${note}`)}</li>)}
        </ul>
      )}
    </li>
  );
}

export function VerifierResultView({ result, definitions }: { result: VerifierResult; definitions?: Definitions }) {
  const { t } = useTranslation();
  const euro = useEuro();
  const { evaluation } = result;
  return (
    <>
      <p className="disclaimer">{t('verifier_disclaimer')}</p>
      <ul className="credential-list">
        {evaluation.results.map((r) => <RuleCard key={r.rule} evaluation={evaluation} result={r} />)}
      </ul>
      <h2>{t('verifier_income_header', { amount: euro(evaluation.income) })}</h2>
      {evaluation.counted.length > 0
        ? <IncomeTable evaluation={evaluation} items={evaluation.counted} definitions={definitions} />
        : <p>{t('verifier_no_income')}</p>}
      {evaluation.excluded.length > 0 && (
        <>
          <h2>{t('verifier_excluded_header')}</h2>
          <IncomeTable evaluation={evaluation} items={evaluation.excluded} definitions={definitions} />
        </>
      )}
      <p className="privacy-note">
        {t('verifier_source', { law: evaluation.source.law, version: evaluation.source.version })}{' '}
        <a href={evaluation.source.url} target="_blank" rel="noreferrer">wetten.overheid.nl</a>
      </p>
    </>
  );
}

export default function VerifierPage() {
  const { t, i18n } = useTranslation();
  const { definitions } = useDefinitions();
  const [household, setHousehold] = useState<Household>({ age_group: 'adult', partner: false, partner_pension_age: false, housemates: 0 });
  const [busy, setBusy] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | undefined>();
  const [result, setResult] = useState<VerifierResult | undefined>();

  const update = (change: Partial<Household>) => setHousehold((h) => {
    const next = { ...h, ...change };
    return { ...next, partner_pension_age: next.partner && next.partner_pension_age };
  });

  const verify = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErrorMessage(undefined);
    try {
      // started by the backend, which keeps the requestor token and fetches
      // the result itself
      const session = await startVerification(household);
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
      <Page
        title={t('verifier_result_header')}
        footer={<><button type="button" id="back-button" onClick={() => setResult(undefined)}>{t('again')}</button><div></div></>}
      >
        <VerifierResultView result={result} definitions={definitions} />
      </Page>
    );
  }

  return (
    <form onSubmit={verify} style={{ height: '100%' }}>
      <Page
        title={t('verifier_header')}
        footer={<><div></div><button id="submit-button" type="submit" disabled={busy}>{t('verifier_start')}</button></>}
      >
        {errorMessage && <Alert kind="danger">{errorMessage}</Alert>}
        <p>{t('verifier_explanation')}</p>
        <fieldset className="household">
          <legend>{t('verifier_age')}</legend>
          {AGE_GROUPS.map((age) => (
            <label key={age} className="choice">
              <input type="radio" name="age_group" checked={household.age_group === age} onChange={() => update({ age_group: age })} />
              {t(`verifier_age_${age}`)}
            </label>
          ))}
        </fieldset>
        <fieldset className="household">
          <legend>{t('verifier_household')}</legend>
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
        <p className="privacy-note">{t('verifier_privacy')}</p>
      </Page>
    </form>
  );
}
