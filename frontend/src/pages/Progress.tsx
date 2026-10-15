import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ApiError, deleteSession, getStatus } from '../api';
import { Page } from '../components';
import { SessionStatus } from '../types';

const POLL_INTERVAL_MS = 2000;

const STEPS: { key: string; status: SessionStatus }[] = [
  { key: 'step_consent', status: 'awaiting_consent' },
  { key: 'step_transactions', status: 'awaiting_transactions' },
  { key: 'step_categorizing', status: 'categorizing' },
  { key: 'step_ready', status: 'ready' },
];

export default function ProgressPage() {
  const navigate = useNavigate();
  const { t, i18n } = useTranslation();
  const [status, setStatus] = useState<SessionStatus>('awaiting_transactions');

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;

    const poll = async () => {
      try {
        const response = await getStatus();
        if (cancelled) return;
        setStatus(response.status);
        if (response.status === 'ready') {
          navigate(`/${i18n.language}/review`, { replace: true, state: response });
          return;
        }
        if (response.status === 'failed') {
          navigate(`/${i18n.language}/error?code=${(response.error ?? 'error_default').replaceAll(':', '_').replaceAll('-', '_')}`, { replace: true });
          return;
        }
        if (response.status === 'awaiting_consent') {
          navigate(`/${i18n.language}/error?code=error_no_consent`, { replace: true });
          return;
        }
        timer = setTimeout(poll, POLL_INTERVAL_MS);
      } catch (err) {
        if (cancelled) return;
        navigate(`/${i18n.language}/error?code=${err instanceof ApiError ? err.code : 'error_default'}`, { replace: true });
      }
    };
    poll();

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [navigate, i18n.language]);

  const current = STEPS.findIndex((s) => s.status === status);

  const cancel = async () => {
    await deleteSession().catch(() => undefined);
    navigate(`/${i18n.language}`);
  };

  return (
    <Page
      title={t('progress_header')}
      footer={<><button type="button" id="back-button" onClick={cancel} style={{ background: '#FFFFFF', color: '#484747', border: '1px solid #484747' }}>{t('cancel')}</button><div></div></>}
    >
      <p>{t('progress_explanation')}</p>
      <ol className="steps">
        {STEPS.map((step, i) => (
          <li key={step.key} className={i < current ? 'done' : i === current ? 'active' : ''}>
            {t(step.key)}
          </li>
        ))}
      </ol>
    </Page>
  );
}
