import { useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { addBank, ApiError, deleteSession, finishConnecting, getStatus } from '../api';
import { Alert, BankList, Page } from '../components';
import { BankStatus, MAX_BANKS } from '../types';

// BanksPage shows the connected banks; the user connects another bank or
// continues to the analysis of all of them together.
export default function BanksPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { t, i18n } = useTranslation();
  const [banks, setBanks] = useState<BankStatus[] | undefined>();
  const [busy, setBusy] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | undefined>();
  // set by the callback page when the last bank was not connected
  const notConnected = (location.state as { notConnected?: boolean } | null)?.notConnected;

  useEffect(() => {
    const fail = (code: string) => navigate(`/${i18n.language}/error?code=${code}`, { replace: true });
    getStatus()
      .then((response) => {
        if (response.status !== 'awaiting_consent') {
          navigate(`/${i18n.language}/progress`, { replace: true });
        } else if (!response.banks?.length) {
          fail('error_no_consent');
        } else {
          setBanks(response.banks);
        }
      })
      .catch((err) => fail(err instanceof ApiError ? err.code : 'error_default'));
  }, [navigate, i18n.language]);

  const run = async (action: () => Promise<void>) => {
    setBusy(true);
    setErrorMessage(undefined);
    try {
      await action();
    } catch (err) {
      setBusy(false);
      setErrorMessage(t(err instanceof ApiError ? err.code : 'error_default'));
    }
  };

  const connectAnother = () =>
    run(async () => {
      const { url } = await addBank();
      // the AppScreen cannot be embedded, so leave the page
      window.location.assign(url);
    });

  const continueToAnalysis = () =>
    run(async () => {
      await finishConnecting();
      navigate(`/${i18n.language}/progress`, { replace: true });
    });

  const cancel = async () => {
    await deleteSession().catch(() => undefined);
    navigate(`/${i18n.language}`);
  };

  const canAdd = banks !== undefined && banks.length < MAX_BANKS;

  return (
    <Page
      title={t('banks_header')}
      footer={
        <>
          <button type="button" id="back-button" onClick={cancel} disabled={busy} style={{ background: '#FFFFFF', color: '#484747', border: '1px solid #484747' }}>
            {t('cancel')}
          </button>
          <button type="button" id="submit-button" onClick={continueToAnalysis} disabled={busy || !banks}>
            {t('banks_continue')}
          </button>
        </>
      }
    >
      {notConnected && <Alert kind="danger">{t('banks_not_connected')}</Alert>}
      {errorMessage && <Alert kind="danger">{errorMessage}</Alert>}
      <p>{t('banks_explanation')}</p>
      {banks && <BankList banks={banks} />}
      {canAdd ? (
        <button type="button" id="add-bank-button" className="secondary" onClick={connectAnother} disabled={busy}>
          {t('banks_add')}
        </button>
      ) : (
        banks && <p className="privacy-note">{t('banks_max', { max: MAX_BANKS })}</p>
      )}
    </Page>
  );
}
