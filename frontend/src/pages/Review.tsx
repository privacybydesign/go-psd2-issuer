import { useEffect, useMemo, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ApiError, getStatus, issue } from '../api';
import { Alert, AttributeTable, CredentialTitle, Page } from '../components';
import { Credential, CredentialKey, CREDENTIAL_KEYS, StatusResponse } from '../types';

export default function ReviewPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { t, i18n } = useTranslation();
  const [credentials, setCredentials] = useState<Credential[] | undefined>(
    (location.state as StatusResponse | null)?.credentials,
  );
  const [selected, setSelected] = useState<Set<CredentialKey>>(new Set(CREDENTIAL_KEYS));
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | undefined>();
  const [errorMessage, setErrorMessage] = useState<string | undefined>();

  // after a page reload the credentials are fetched again
  useEffect(() => {
    if (credentials) return;
    getStatus()
      .then((response) => {
        if (response.status === 'ready' && response.credentials) {
          setCredentials(response.credentials);
        } else {
          navigate(`/${i18n.language}/progress`, { replace: true });
        }
      })
      .catch((err) => navigate(`/${i18n.language}/error?code=${err instanceof ApiError ? err.code : 'error_default'}`, { replace: true }));
  }, [credentials, navigate, i18n.language]);

  // bank accounts share a key, so group them into one selectable card
  const groups = useMemo(() => {
    const byKey = new Map<CredentialKey, Credential[]>();
    for (const credential of credentials ?? []) {
      byKey.set(credential.key, [...(byKey.get(credential.key) ?? []), credential]);
    }
    return CREDENTIAL_KEYS.filter((key) => byKey.has(key)).map((key) => ({ key, credentials: byKey.get(key)! }));
  }, [credentials]);

  const toggle = (key: CredentialKey) => {
    const next = new Set(selected);
    if (next.has(key)) {
      next.delete(key);
    } else {
      next.add(key);
    }
    setSelected(next);
  };

  const chosen = groups.map((g) => g.key).filter((key) => selected.has(key));

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (chosen.length === 0) {
      setErrorMessage(t('review_none_selected'));
      return;
    }
    setBusy(true);
    setErrorMessage(undefined);
    setMessage(undefined);
    try {
      const res = await issue(chosen);
      const yivi = await import('@privacybydesign/yivi-frontend');
      const issuance = yivi.newPopup({
        language: i18n.language as 'nl' | 'en',
        session: {
          url: res.irma_server_url,
          start: {
            method: 'POST',
            headers: { 'Content-Type': 'text/plain' },
            body: res.jwt,
          },
          result: false,
        },
      });
      await issuance.start();
      setMessage(t('add_success'));
      navigate(`/${i18n.language}/done`);
    } catch (err) {
      if (err === 'Aborted') {
        setErrorMessage(t('add_cancel'));
      } else if (err instanceof ApiError) {
        setErrorMessage(t(err.code));
      } else {
        setErrorMessage(t('add_error'));
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={add} style={{ height: '100%' }}>
      <Page
        title={t('review_header')}
        footer={<><div></div><button id="submit-button" type="submit" disabled={busy || !credentials || chosen.length === 0}>{t('review_add')}</button></>}
      >
        {message && <Alert kind="success">{message}</Alert>}
        {errorMessage && <Alert kind="danger">{errorMessage}</Alert>}
        <p>{t('review_explanation')}</p>
        <ul className="credential-list">
          {groups.map(({ key, credentials }) => (
            <li key={key} className={`credential-card ${selected.has(key) ? '' : 'unselected'}`}>
              <label className="select">
                <input type="checkbox" checked={selected.has(key)} onChange={() => toggle(key)} />
                <div><CredentialTitle credentialKey={key} /></div>
              </label>
              {credentials.map((credential, i) => (
                <AttributeTable key={i} credential={credential} />
              ))}
            </li>
          ))}
        </ul>
      </Page>
    </form>
  );
}
