import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, getVerifierLaws, startVerification, verificationResult } from '../api';
import { Alert, Page, translate, useDefinitions } from '../components';
import { Definitions, Evaluation, Household, IncomeItem, RuleStep, VerifierLaw, VerifierResult } from '../types';

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

export function VerifierResultView({ result, definitions }: { result: VerifierResult; definitions?: Definitions }) {
  const { t } = useTranslation();
  const euro = useEuro();
  const { evaluation } = result;
  const rule = evaluation.result;
  return (
    <>
      <p className="disclaimer">{t('verifier_disclaimer')}</p>
      <ul className="credential-list">
        <li className={`credential-card rule-card outcome-${rule.outcome}`}>
          <h2>{t(`verifier_rule_${rule.rule}`)}</h2>
          <p className="outcome">{t(`verifier_outcome_${rule.rule}_${rule.outcome}`)}</p>
          <p className="reason">{t('verifier_age_group', { age: t(`verifier_age_${result.age_group}`) })}</p>
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
          {rule.notes && (
            <ul className="notes">
              {rule.notes.map((note) => <li key={note}>{t(`verifier_${note}`)}</li>)}
            </ul>
          )}
        </li>
      </ul>
      <h2>{t('verifier_income_header', { amount: euro(evaluation.income) })}</h2>
      {evaluation.counted.length > 0
        ? <ItemTable evaluation={evaluation} items={evaluation.counted} definitions={definitions} />
        : <p>{t('verifier_no_income')}</p>}
      {evaluation.expenses && evaluation.expenses.length > 0 && (
        <>
          <h2>{t('verifier_expenses_header')}</h2>
          <ItemTable evaluation={evaluation} items={evaluation.expenses} definitions={definitions} />
        </>
      )}
      {evaluation.excluded.length > 0 && (
        <>
          <h2>{t('verifier_excluded_header')}</h2>
          <ItemTable evaluation={evaluation} items={evaluation.excluded} definitions={definitions} />
        </>
      )}
      <h2>{t('verifier_sources_header')}</h2>
      <ul className="sources">
        {evaluation.sources.map((source) => (
          <li key={source.id}>
            <a href={source.url} target="_blank" rel="noreferrer">{source.name}</a>{' '}
            {t('verifier_source_version', { version: source.version })}
          </li>
        ))}
      </ul>
      <p className="privacy-note">{t('verifier_source')}</p>
    </>
  );
}

function LawChoice({ law, selected, onSelect, definitions }: { law: VerifierLaw; selected: boolean; onSelect: () => void; definitions?: Definitions }) {
  const { t, i18n } = useTranslation();
  return (
    <li className={`credential-card law-card ${selected ? '' : 'unselected'}`}>
      <label className="select">
        <input type="radio" name="law" checked={selected} onChange={onSelect} />
        <div>
          <h2>{t(`verifier_law_${law.id}`)}</h2>
          <p>{t(`verifier_law_${law.id}_description`)}</p>
        </div>
      </label>
      {selected && (
        <details className="requested">
          <summary>{t('verifier_requested', { count: law.attributes.length + 1 })}</summary>
          <ul>
            {law.attributes.map((attribute) => <li key={attribute}>{attributeName(definitions, attribute, i18n.language, t)}</li>)}
            <li>{t('verifier_date_of_birth')}</li>
          </ul>
        </details>
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
        footer={<><div></div><button id="submit-button" type="submit" disabled={busy || !law}>{t('verifier_start')}</button></>}
      >
        {errorMessage && <Alert kind="danger">{errorMessage}</Alert>}
        <p>{t('verifier_explanation')}</p>
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
      </Page>
    </form>
  );
}
