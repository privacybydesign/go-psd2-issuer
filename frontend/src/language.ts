// The AppScreen redirects back to /callback without the language prefix, so
// the chosen language is remembered for the duration of the tab session.
const LANGUAGE_STORAGE_KEY = 'language';

export const rememberLanguage = (language: string) => {
  try {
    sessionStorage.setItem(LANGUAGE_STORAGE_KEY, language);
  } catch {
    // Ignore storage errors (e.g. private mode).
  }
};

export const rememberedLanguage = (): string => {
  try {
    const language = sessionStorage.getItem(LANGUAGE_STORAGE_KEY);
    if (language === 'en' || language === 'nl') {
      return language;
    }
  } catch {
    // Ignore storage errors (e.g. private mode).
  }
  return 'nl';
};
