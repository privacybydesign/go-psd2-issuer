import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { deleteSession } from '../api';
import { Page } from '../components';

export default function DonePage() {
  const { t, i18n } = useTranslation();

  // the computed values are no longer needed once they are in the Yivi app
  useEffect(() => {
    deleteSession().catch(() => undefined);
  }, []);

  return (
    <Page
      title={t('done_header')}
      footer={<><Link to={`/${i18n.language}`} id="back-button">{t('again')}</Link><div></div></>}
    >
      <div className="imageContainer">
        <img src="/images/done.png" alt="" />
        <p>{t('thank_you')}</p>
      </div>
    </Page>
  );
}
