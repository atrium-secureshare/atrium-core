// UI language follows the browser on first visit; an explicit switcher choice is
// persisted in localStorage and wins on return. English is the fallback.
import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import de from './locales/de.json'
import en from './locales/en.json'
import fr from './locales/fr.json'
import it from './locales/it.json'

// Single source of truth for the switcher and detector.
export const SUPPORTED_LANGUAGES = ['de', 'fr', 'it', 'en'] as const
export type Language = (typeof SUPPORTED_LANGUAGES)[number]

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      de: { translation: de },
      fr: { translation: fr },
      it: { translation: it },
      en: { translation: en },
    },
    supportedLngs: [...SUPPORTED_LANGUAGES],
    fallbackLng: 'en',
    // Collapse regional tags (de-CH, en-US) to the base language we ship.
    load: 'languageOnly',
    interpolation: { escapeValue: false },
    // Resources are bundled (synchronous init), so disable Suspense — no boundary in the tree.
    react: { useSuspense: false },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      lookupLocalStorage: 'atrium-locale',
    },
  })

// Keep <html lang> in sync with the active language (a11y, hyphenation/quotes).
function syncDocumentLang(lng: string) {
  document.documentElement.lang = lng
}
syncDocumentLang(i18n.resolvedLanguage ?? 'en')
i18n.on('languageChanged', syncDocumentLang)

export default i18n
