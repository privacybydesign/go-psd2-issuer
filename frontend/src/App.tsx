import { useEffect } from 'react';
import { BrowserRouter, Routes, Route, Navigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

import IndexPage from './pages/Index';
import CallbackPage from './pages/Callback';
import ProgressPage from './pages/Progress';
import ReviewPage from './pages/Review';
import ErrorPage from './pages/Error';
import DonePage from './pages/Done';
import { rememberLanguage } from './language';

import './i18n';

// Wrapper that sets the language based on the URL
function LanguageRouter() {
  const { lang } = useParams();
  const { i18n } = useTranslation();

  useEffect(() => {
    if (lang && i18n.language !== lang) {
      i18n.changeLanguage(lang);
    }
    if (lang) {
      rememberLanguage(lang);
    }
  }, [lang, i18n]);

  return (
    <Routes>
      <Route path="/" element={<IndexPage />} />
      <Route path="/progress" element={<ProgressPage />} />
      <Route path="/review" element={<ReviewPage />} />
      <Route path="/error" element={<ErrorPage />} />
      <Route path="/done" element={<DonePage />} />
    </Routes>
  );
}

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Navigate to="/nl" replace />} />
        {/* the enablenow AppScreen redirects back here */}
        <Route path="/callback" element={<CallbackPage />} />
        <Route path=":lang/*" element={<LanguageRouter />} />
      </Routes>
    </BrowserRouter>
  );
}

export default App;
