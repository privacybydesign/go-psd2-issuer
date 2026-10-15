import { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Credential, CredentialKey } from './types';

export function Page({ title, children, footer }: { title: string; children: ReactNode; footer?: ReactNode }) {
  return (
    <div id="container">
      <header>
        <h1>{title}</h1>
      </header>
      <main>
        <div className="content">{children}</div>
      </main>
      <footer>
        <div className="actions">{footer}</div>
      </footer>
    </div>
  );
}

export function Alert({ kind, children }: { kind: 'success' | 'danger'; children: ReactNode }) {
  return (
    <div id="status-bar" className={`alert alert-${kind}`} role="alert">
      <div className="status-container">
        <div id="status">{children}</div>
      </div>
    </div>
  );
}

// Attributes that hold a count or date rather than a euro amount.
const NON_AMOUNT = new Set(['months', 'salarymonths', 'periodstart', 'periodend']);
const ENUM_VALUES = new Set(['yes', 'no', 'rent', 'mortgage', 'other', 'none']);

export function useFormatAttribute() {
  const { t, i18n } = useTranslation();
  const locale = i18n.language === 'en' ? 'en-GB' : 'nl-NL';
  const euro = new Intl.NumberFormat(locale, { style: 'currency', currency: 'EUR', maximumFractionDigits: 0 });

  return (name: string, value: string): string => {
    if (name === 'periodstart' || name === 'periodend') {
      const date = new Date(value + 'T00:00:00');
      return isNaN(date.getTime()) ? value : date.toLocaleDateString(locale, { day: 'numeric', month: 'long', year: 'numeric' });
    }
    if (ENUM_VALUES.has(value) && !['iban', 'accountholder', 'bank'].includes(name)) {
      return t(`value_${value}`);
    }
    if (!NON_AMOUNT.has(name) && /^-?\d+$/.test(value)) {
      return euro.format(parseInt(value));
    }
    return value;
  };
}

// Display order of the attributes per credential; the backend sends maps.
const ATTRIBUTE_ORDER: Record<CredentialKey, string[]> = {
  income: ['salary', 'pension', 'otherincome', 'total', 'salarymonths'],
  housing: ['housingtype', 'housingcost', 'energy', 'water', 'insurance', 'telecom', 'municipaltaxes', 'total'],
  debts: ['loanrepayments', 'studentloan', 'debtcollection', 'flashloan', 'bnpl', 'gambling'],
  benefits: ['receivesbenefits', 'healthcare', 'rent', 'child', 'unemployment', 'social', 'disability', 'student', 'other', 'total'],
  bankaccount: ['iban', 'accountholder', 'bank'],
};
const PERIOD_ATTRIBUTES = ['months', 'periodstart', 'periodend'];

export const orderedAttributes = (credential: Credential): [string, string][] => {
  const order = [...(ATTRIBUTE_ORDER[credential.key] ?? []), ...PERIOD_ATTRIBUTES];
  const rank = (name: string) => (order.indexOf(name) === -1 ? order.length : order.indexOf(name));
  return Object.entries(credential.attributes).sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b));
};

export function AttributeTable({ credential }: { credential: Credential }) {
  const { t } = useTranslation();
  const format = useFormatAttribute();
  return (
    <table className="attributes">
      <tbody>
        {orderedAttributes(credential).map(([name, value]) => (
          <tr key={name}>
            <th>{t(`attr_${name}`)}</th>
            <td>{format(name, value)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function CredentialTitle({ credentialKey }: { credentialKey: CredentialKey }) {
  const { t } = useTranslation();
  return (
    <>
      <h2>{t(`credential_${credentialKey}`)}</h2>
      <p>{t(`credential_${credentialKey}_description`)}</p>
    </>
  );
}
