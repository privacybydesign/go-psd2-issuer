import { useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ApiError, submitConsent } from '../api';
import { Page } from '../components';
import { rememberedLanguage } from '../language';

// CallbackPage receives the redirect from the enablenow AppScreen and hands the
// consent id over to the backend.
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

    const params = new URLSearchParams(window.location.search);
    const consentId = params.get('consent_id');
    const state = params.get('state');
    if (!consentId || !state) {
      navigate(`/${lang}/error?code=error_no_consent`, { replace: true });
      return;
    }

    submitConsent(consentId, state)
      .then(() => navigate(`/${lang}/progress`, { replace: true }))
      .catch((err) => {
        const code = err instanceof ApiError ? err.code : 'error_default';
        navigate(`/${lang}/error?code=${code}`, { replace: true });
      });
  }, [navigate, i18n, lang]);

  return <Page title={t('callback_header')}><p>{t('progress_explanation')}</p></Page>;
}
