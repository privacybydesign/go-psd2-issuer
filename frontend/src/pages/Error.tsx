import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Page } from '../components';

export default function ErrorPage() {
  const { t, i18n } = useTranslation();
  const code = new URLSearchParams(window.location.search).get('code');
  // only show messages we have a translation for
  const message = code && /^error_[a-z_]+$/.test(code) && i18n.exists(code) ? t(code) : t('error_default');

  return (
    <Page
      title={t('error_header')}
      footer={<><Link to={`/${i18n.language}`} id="back-button">{t('again')}</Link><div></div></>}
    >
      <div className="imageContainer">
        <img src="/images/fail.png" alt="" />
        <p>{message}</p>
      </div>
    </Page>
  );
}
