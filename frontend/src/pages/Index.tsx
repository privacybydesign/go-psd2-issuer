import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { startSession, ApiError } from '../api';
import { Alert, CredentialTitle, Page } from '../components';
import { CREDENTIAL_KEYS } from '../types';

export default function IndexPage() {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | undefined>();

  const start = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErrorMessage(undefined);
    try {
      const { url } = await startSession();
      // the AppScreen cannot be embedded, so leave the page
      window.location.assign(url);
    } catch (err) {
      setBusy(false);
      if (err instanceof ApiError && err.code === 'error_ratelimit' && err.retryAfter) {
        const time = new Date(Date.now() + err.retryAfter * 1000).toLocaleTimeString('nl-NL', { timeZone: 'Europe/Amsterdam' });
        setErrorMessage(t('error_ratelimit', { time }));
      } else {
        setErrorMessage(t(err instanceof ApiError ? err.code : 'error_default'));
      }
    }
  };

  return (
    <form onSubmit={start} style={{ height: '100%' }}>
      <Page
        title={t('index_header')}
        footer={<><div></div><button id="submit-button" type="submit" disabled={busy}>{t('index_start')}</button></>}
      >
        {errorMessage && <Alert kind="danger">{errorMessage}</Alert>}
        <p>{t('index_explanation')}</p>
        <ul className="credential-list">
          {CREDENTIAL_KEYS.map((key) => (
            <li key={key} className="credential-card">
              <CredentialTitle credentialKey={key} />
            </li>
          ))}
        </ul>
        <p className="privacy-note">{t('index_privacy')}</p>
      </Page>
    </form>
  );
}
