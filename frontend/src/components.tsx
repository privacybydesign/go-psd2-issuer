import { ReactNode, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getDefinitions } from './api';
import { AttributeDefinition, Credential, CredentialDefinition, Definitions, Translated } from './types';

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
const PERIOD = new Set(['months', 'periodstart', 'periodend']);

export function useFormatAttribute() {
  const { i18n } = useTranslation();
  const locale = i18n.language === 'en' ? 'en-GB' : 'nl-NL';
  const euro = new Intl.NumberFormat(locale, { style: 'currency', currency: 'EUR', maximumFractionDigits: 0 });

  return (attribute: AttributeDefinition | undefined, name: string, value: string): string => {
    if (name === 'periodstart' || name === 'periodend') {
      const date = new Date(value + 'T00:00:00');
      return isNaN(date.getTime()) ? value : date.toLocaleDateString(locale, { day: 'numeric', month: 'long', year: 'numeric' });
    }
    // only attributes taken from E1 categories hold amounts
    if (attribute?.e1 && !PERIOD.has(name) && /^-?\d+$/.test(value)) {
      return euro.format(parseInt(value));
    }
    return value;
  };
}

export const translate = (text: Translated | undefined, language: string) =>
  text ? (language === 'en' ? text.en : text.nl) : undefined;

// The definitions, fetched once from the backend; undefined while loading.
export function useDefinitions(): { definitions?: Definitions; error?: unknown } {
  const [state, setState] = useState<{ definitions?: Definitions; error?: unknown }>({});
  useEffect(() => {
    let active = true;
    getDefinitions()
      .then((definitions) => active && setState({ definitions }))
      .catch((error) => active && setState({ error }));
    return () => {
      active = false;
    };
  }, []);
  return state;
}

// Rows in the order of the definition, then the period, then anything unknown.
export const orderedAttributes = (
  credential: Credential,
  definition: CredentialDefinition | undefined,
  period: AttributeDefinition[],
): [string, string, AttributeDefinition | undefined][] => {
  const known = [...(definition?.attributes ?? []), ...period];
  const rank = (name: string) => {
    const i = known.findIndex((a) => a.id === name);
    return i === -1 ? known.length : i;
  };
  return Object.entries(credential.attributes)
    .sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
    .map(([name, value]) => [name, value, known.find((a) => a.id === name)]);
};

// isEmpty reports whether all amounts of a credential are zero.
export const isEmpty = (credential: Credential, definition: CredentialDefinition | undefined) =>
  !!definition?.e1_group && definition.attributes.every((a) => (credential.attributes[a.id] ?? '0') === '0');

export function AttributeTable({ credential, definitions }: { credential: Credential; definitions: Definitions }) {
  const { i18n } = useTranslation();
  const format = useFormatAttribute();
  const definition = definitions.credentials.find((c) => c.key === credential.key);
  return (
    <table className="attributes">
      <tbody>
        {orderedAttributes(credential, definition, definitions.period_attributes).map(([name, value, attribute]) => (
          <tr key={name} title={attribute?.e1}>
            <th>{translate(attribute?.name, i18n.language) ?? name}</th>
            <td>{format(attribute, name, value)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function CredentialTitle({ definition }: { definition: CredentialDefinition }) {
  const { i18n } = useTranslation();
  return (
    <>
      <h2>{translate(definition.name, i18n.language)}</h2>
      <p>{translate(definition.description, i18n.language)}</p>
    </>
  );
}
