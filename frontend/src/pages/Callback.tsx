import { useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ApiError, getStatus, submitConsent } from '../api';
import { Page } from '../components';
import { rememberedLanguage } from '../language';

// CallbackPage receives the redirect from the enablenow AppScreen and hands the
// consent id over to the backend. The user then connects another bank or
// continues on the banks page.
export default function CallbackPage() {
  const navigate = useNavigate();
  const { t, i18n } = useTranslation();
  const submitted = useRef(false);
  const lang = rememberedLanguage();

  useEffect(() => {
    if (i18n.language !== lang) {
      i18n.changeLanguage(lang);
    }
    // StrictMode runs effects twice in development; submit only once
    if (submitted.current) {
      return;
    }
    submitted.current = true;

    const fail = (err: unknown) => {
      const code = err instanceof ApiError ? err.code : 'error_default';
      navigate(`/${lang}/error?code=${code}`, { replace: true });
    };

    const params = new URLSearchParams(window.location.search);
    const consentId = params.get('consent_id');
    const state = params.get('state');
    if (!consentId || !state) {
      // connecting another bank was cancelled: keep the banks connected so far
      getStatus()
        .then((response) => {
          if (response.status === 'awaiting_consent' && response.banks?.length) {
            navigate(`/${lang}/banks`, { replace: true, state: { notConnected: true } });
          } else {
            navigate(`/${lang}/error?code=error_no_consent`, { replace: true });
          }
        })
        .catch(() => navigate(`/${lang}/error?code=error_no_consent`, { replace: true }));
      return;
    }

    submitConsent(consentId, state)
      .then(() => navigate(`/${lang}/banks`, { replace: true }))
      .catch(fail);
  }, [navigate, i18n, lang]);

  return <Page title={t('callback_header')}><p>{t('progress_explanation')}</p></Page>;
}
