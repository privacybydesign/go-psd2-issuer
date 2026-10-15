import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

const en = {
    index_header: "Add financial data",
    index_explanation: "Prove facts about your finances with Yivi, without handing over your bank statements. Connect your bank account once; we analyse your transactions of the last 12 months and you choose which of the following cards to add to your Yivi app.",
    index_privacy: "Your bank connection is made through enablenow, a licensed account information service. After the analysis the connection and your transactions are deleted; only the cards you add are kept, in your own Yivi app.",
    index_start: "Connect bank",
    back: "Back",
    cancel: "Cancel",


    callback_header: "Bank connected",
    progress_header: "Analysing transactions",
    progress_explanation: "This usually takes less than a minute. Please keep this page open.",
    step_consent: "Bank account connected",
    step_transactions: "Retrieving transactions",
    step_categorizing: "Categorizing transactions",
    step_ready: "Calculating results",

    review_header: "Choose your cards",
    review_explanation: "Check the results and choose which cards to add to your Yivi app. Amounts are the net monthly average per transaction category, rounded to whole tens of euros: positive is received, negative is paid. Cards with only zero amounts are not selected.",
    definitions_error: "The card descriptions could not be loaded.",
    review_none_selected: "Select at least one card.",
    review_add: "Add to Yivi",
    add_success: "Cards added.",
    add_cancel: "Cancelled.",
    add_error: "Unfortunately, it was not possible to add the cards to the Yivi app.",

    error_header: "Error message",
    error_default: "An unknown error has occurred. Please try again later.",
    error_internal: "Internal error. Please contact Yivi if this happens more often.",
    error_bad_request: "Your request could not be processed. Please try again.",
    error_ratelimit: "You have tried too often. Please try again after {{time}}.",
    error_session_not_found: "Your session has expired. Please start again.",
    error_invalid_state: "The bank connection could not be linked to this session. Please start again.",
    error_not_ready: "The analysis is not finished yet.",
    error_bank_connection: "Connecting to your bank failed or was cancelled.",
    error_categorization: "Analysing your transactions failed. Please try again later.",
    error_no_transactions: "No transactions were found in the last 12 months.",
    error_timeout: "Retrieving your transactions took too long. Please try again later.",
    error_enablenow: "The bank data service is unavailable. Please try again later.",
    error_no_consent: "The bank connection was not completed.",

    done_header: "Cards added",
    thank_you: "Thank you for using Yivi, you can close this page now.",
    again: "Start again",
};

type Translations = typeof en;

const nl: Translations = {
    index_header: "Financiële gegevens toevoegen",
    index_explanation: "Bewijs feiten over je financiën met Yivi, zonder je bankafschriften af te geven. Koppel eenmalig je bankrekening; wij analyseren je transacties van de afgelopen 12 maanden en jij kiest welke van de volgende kaartjes je aan je Yivi-app toevoegt.",
    index_privacy: "De koppeling met je bank loopt via enablenow, een vergunde rekeninginformatiedienst. Na de analyse worden de koppeling en je transacties verwijderd; alleen de kaartjes die je toevoegt blijven bewaard, in je eigen Yivi-app.",
    index_start: "Bank koppelen",
    back: "Terug",
    cancel: "Annuleren",


    callback_header: "Bank gekoppeld",
    progress_header: "Transacties analyseren",
    progress_explanation: "Dit duurt meestal minder dan een minuut. Houd deze pagina open.",
    step_consent: "Bankrekening gekoppeld",
    step_transactions: "Transacties ophalen",
    step_categorizing: "Transacties categoriseren",
    step_ready: "Resultaten berekenen",

    review_header: "Kies je kaartjes",
    review_explanation: "Controleer de resultaten en kies welke kaartjes je aan je Yivi-app toevoegt. Bedragen zijn het netto maandgemiddelde per transactiecategorie, afgerond op hele tientallen euro's: positief is ontvangen, negatief is betaald. Kaartjes met alleen nulbedragen zijn niet geselecteerd.",
    definitions_error: "De beschrijvingen van de kaartjes konden niet worden geladen.",
    review_none_selected: "Selecteer minstens één kaartje.",
    review_add: "Toevoegen aan Yivi",
    add_success: "Kaartjes toegevoegd.",
    add_cancel: "Geannuleerd.",
    add_error: "Het is helaas niet gelukt om de kaartjes aan de Yivi-app toe te voegen.",

    error_header: "Foutmelding",
    error_default: "Er is een onbekende fout opgetreden. Probeer het later opnieuw.",
    error_internal: "Interne fout. Neem contact op met Yivi als dit vaker voorkomt.",
    error_bad_request: "Je verzoek kon niet worden verwerkt. Probeer het opnieuw.",
    error_ratelimit: "Je hebt het te vaak geprobeerd. Probeer het opnieuw na {{time}}.",
    error_session_not_found: "Je sessie is verlopen. Begin opnieuw.",
    error_invalid_state: "De bankkoppeling kon niet aan deze sessie worden gekoppeld. Begin opnieuw.",
    error_not_ready: "De analyse is nog niet klaar.",
    error_bank_connection: "Het koppelen van je bank is mislukt of geannuleerd.",
    error_categorization: "Het analyseren van je transacties is mislukt. Probeer het later opnieuw.",
    error_no_transactions: "Er zijn geen transacties gevonden in de afgelopen 12 maanden.",
    error_timeout: "Het ophalen van je transacties duurde te lang. Probeer het later opnieuw.",
    error_enablenow: "De bankgegevensdienst is niet beschikbaar. Probeer het later opnieuw.",
    error_no_consent: "De bankkoppeling is niet afgerond.",

    done_header: "Kaartjes toegevoegd",
    thank_you: "Bedankt voor het gebruiken van Yivi, je kunt deze pagina nu sluiten.",
    again: "Opnieuw beginnen",
};

i18n
    .use(LanguageDetector)
    .use(initReactI18next).init({
        detection: {
            order: ['path', 'navigator'],
            lookupFromPathIndex: 0
        },
        resources: {
            en: { translation: en },
            nl: { translation: nl },
        },
        fallbackLng: 'nl',
        supportedLngs: ['nl', 'en'],
        interpolation: {
            escapeValue: false,
        },
    });

export default i18n;
