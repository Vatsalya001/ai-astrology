/**
 * UI copy, by locale.
 *
 * Every user-visible string lives here. The reason is not future
 * translation work — it is that a hardcoded string is invisible to
 * review: nobody notices "Enter your phone number" shipping on a screen
 * that accepts an email until a user does.
 *
 * `en` is the source of truth. `Dictionary` is derived from it, so
 * adding a key to English and forgetting Hindi is a type error rather
 * than a blank space on a page.
 *
 * ── Deliberately NOT in here ──
 *
 * `/terms` and `/privacy` are legal prose. Translating a liability
 * disclaimer myself would produce a document nobody has reviewed in a
 * language nobody has checked, which is worse than English text a reader
 * can at least run through a translator knowing it is unofficial. They
 * get a reviewed translation when they get a reviewed original.
 *
 * `/status` is an internal operations page carrying `noindex`. Its
 * audience reads English by construction.
 */

export const en = {
  common: {
    continue: 'Continue',
    back: 'Back',
    cancel: 'Cancel',
    saving: 'Saving…',
    somethingWentWrong: 'Something went wrong. Please try again.',
    tryAgain: 'Try again',
  },

  auth: {
    title: 'Sign in or create an account',
    // One field, one button. Never make someone guess whether they
    // already have an account — the branch happens server-side.
    subtitle: 'We’ll send you a code. No password to remember.',
    emailLabel: 'Email address',
    emailPlaceholder: 'you@example.com',
    phoneLabel: 'Phone number',
    phonePlaceholder: '+91 98765 43210',
    useEmail: 'Use email instead',
    usePhone: 'Use phone instead',
    invalidEmail: 'Enter a valid email address.',
    invalidPhone: 'Enter a valid phone number, including the country code.',
    sending: 'Sending…',
    termsPrefix: 'By continuing you agree to our',
    terms: 'Terms',
    and: 'and',
    privacy: 'Privacy Policy',
  },

  verify: {
    title: 'Enter your code',
    sentTo: 'We sent a 6-digit code to',
    changeIdentifier: 'Use a different address',
    resend: 'Resend code',
    resendIn: 'Resend in {seconds}s',
    // After three sends, offering the other channel beats repeating a
    // button that evidently is not working for this person.
    tryEmailInstead: 'Try email instead',
    verifying: 'Verifying…',
    incorrect: 'That code is not valid. Check it, or request a new one.',
    tooManyAttempts: 'Too many incorrect attempts. Request a new code.',
    rateLimited: 'Too many requests. Try again shortly.',
  },

  onboarding: {
    title: 'What should we call you?',
    subtitle: 'Just a name for now. Your birth details come next.',
    nameLabel: 'Your name',
    namePlaceholder: 'Priya',
    languageLabel: 'Preferred language',
    finish: 'Finish',
    nameRequired: 'Please enter a name.',
  },

  nav: {
    settings: 'Settings',
    signOut: 'Sign out',
    home: 'Home',
    back: 'Back to settings',
  },

  home: {
    sectionLabel: 'Your account',
    welcomeNamed: 'Welcome, {name}',
    welcome: 'Welcome',
    body: "You're signed in. Your birth chart arrives in Phase 2 — until then this is the shell that proves the account works.",
    chartTitle: 'Your birth chart',
    chartBody: 'Date, time and place. We resolve the exact historical timezone offset — a 30-minute error can change your entire chart.',
    chartCta: 'Add your birth details',
    loading: 'Loading your account…',
  },

  landing: {
    phase: 'Phase 1 · Accounts',
    headlineA: "The AI doesn't just know astrology.",
    headlineB: 'It knows your astrology.',
    lede: 'A personal astrologer that understands your birth chart, your life context and everything you\u2019ve discussed before \u2014 and connects you to a human expert when you need one.',
    ctaPrimary: 'Get your free Kundli',
    ctaSecondary: 'View system status',
    ctaNote: 'Free to start. No password to remember.',
    signIn: 'Sign in',
    systemStatus: 'System status',
    howLabel: 'How it works',
    howTitle: 'From birth details to real guidance',
    commitLabel: 'What we commit to',
    commitTitle: 'Built to be trusted',
    progressLabel: 'Progress',
    progressTitle: 'Where we are',
    progressBody: 'Built one phase at a time. Each phase has a gate that must pass before the next one starts.',
    disclaimer: 'For guidance and reflection. Astrological readings are not a substitute for professional medical, legal or financial advice.',
  },

  settings: {
    title: 'Settings',
    profile: 'Profile',
    preferences: 'Preferences',
    sessions: 'Devices',
    deleteAccount: 'Delete account',
    saved: 'Saved',
    save: 'Save changes',

    profileTitle: 'Your profile',
    nameLabel: 'Name',
    genderLabel: 'Gender',
    emailLabel: 'Email',
    phoneLabel: 'Phone',
    verified: 'Verified',
    unverified: 'Not verified',
    contactLocked: 'Contact details change through verification, not here.',
    notSet: 'Not set',

    prefsTitle: 'Preferences',
    languageLabel: 'Language',
    systemLabel: 'Astrology system',
    chartStyleLabel: 'Chart style',
    themeLabel: 'Theme',

    sessionsTitle: 'Signed-in devices',
    sessionsBody: 'Revoking a device signs it out immediately.',
    sessionsEmpty: 'No other devices are signed in.',
    signedIn: 'Signed in',
    expires: 'Expires',
    revoke: 'Revoke',
    revoking: 'Revoking…',
    signOutEverywhere: 'Sign out everywhere',
    thisDevice: 'This device',

    deleteTitle: 'Delete your account',
    deleteBody: 'This removes your account and everything in it. Not hidden, not flagged — deleted.',
    deleteGrace: 'You have seven days to change your mind. After that it cannot be undone.',
    deleteExportFirst: 'Download your data first',
    deleteSendCode: 'Send me a code',
    deleteCodeLabel: 'Code sent to your verified contact',
    deleteConfirmLabel: 'Type DELETE to confirm',
    deleteConfirmWord: 'DELETE',
    deleteButton: 'Delete my account',
    deleteScheduled: 'Your account will be deleted on {date}.',
    deleteCancel: 'Cancel deletion',
    exportTitle: 'Export your data',
    exportBody: 'A complete JSON file of everything we hold about you.',
    exportButton: 'Download',
  },

  /**
   * Option VALUES, not just their labels.
   *
   * These are rendered to the user, so an untranslated enum is a
   * half-translated screen — "ज्योतिष पद्धति: Vedic / Western" reads as
   * broken rather than bilingual. Keyed by the exact value the API
   * accepts, so a mismatch is a missing key rather than a silent
   * fallback to something wrong.
   */
  values: {
    vedic: 'Vedic',
    western: 'Western',
    north: 'North',
    south: 'South',
    east: 'East',
    dark: 'Dark',
    light: 'Light',
    system: 'System',
    male: 'Male',
    female: 'Female',
    other: 'Other',
    prefer_not_to_say: 'Prefer not to say',
  },
}

/**
 * The shape every locale must satisfy.
 *
 * `en` is deliberately NOT `as const`: that would make each value a
 * literal type, and `hi` could then only be assigned the English string.
 * Without it the keys are still checked exhaustively — a key present in
 * English and missing from Hindi is a compile error — which is the
 * property that matters.
 */
export type Dictionary = typeof en

export const hi: Dictionary = {
  common: {
    continue: 'आगे बढ़ें',
    back: 'वापस',
    cancel: 'रद्द करें',
    saving: 'सहेजा जा रहा है…',
    somethingWentWrong: 'कुछ गलत हो गया। कृपया पुनः प्रयास करें।',
    tryAgain: 'पुनः प्रयास करें',
  },

  auth: {
    title: 'साइन इन करें या खाता बनाएँ',
    subtitle: 'हम आपको एक कोड भेजेंगे। कोई पासवर्ड याद रखने की ज़रूरत नहीं।',
    emailLabel: 'ईमेल पता',
    emailPlaceholder: 'you@example.com',
    phoneLabel: 'फ़ोन नंबर',
    phonePlaceholder: '+91 98765 43210',
    useEmail: 'इसके बजाय ईमेल का उपयोग करें',
    usePhone: 'इसके बजाय फ़ोन का उपयोग करें',
    invalidEmail: 'एक मान्य ईमेल पता दर्ज करें।',
    invalidPhone: 'देश कोड सहित एक मान्य फ़ोन नंबर दर्ज करें।',
    sending: 'भेजा जा रहा है…',
    termsPrefix: 'आगे बढ़कर आप हमारी',
    terms: 'शर्तों',
    and: 'और',
    privacy: 'गोपनीयता नीति',
  },

  verify: {
    title: 'अपना कोड दर्ज करें',
    sentTo: 'हमने 6 अंकों का कोड भेजा है',
    changeIdentifier: 'कोई दूसरा पता उपयोग करें',
    resend: 'कोड फिर भेजें',
    resendIn: '{seconds} सेकंड में फिर भेजें',
    tryEmailInstead: 'इसके बजाय ईमेल आज़माएँ',
    verifying: 'सत्यापित किया जा रहा है…',
    incorrect: 'यह कोड मान्य नहीं है। इसे जाँचें, या नया कोड माँगें।',
    tooManyAttempts: 'बहुत अधिक गलत प्रयास। नया कोड माँगें।',
    rateLimited: 'बहुत अधिक अनुरोध। कृपया थोड़ी देर बाद प्रयास करें।',
  },

  onboarding: {
    title: 'हम आपको क्या कहें?',
    subtitle: 'अभी बस एक नाम। जन्म विवरण अगले चरण में।',
    nameLabel: 'आपका नाम',
    namePlaceholder: 'प्रिया',
    languageLabel: 'पसंदीदा भाषा',
    finish: 'पूर्ण करें',
    nameRequired: 'कृपया एक नाम दर्ज करें।',
  },

  nav: {
    settings: 'सेटिंग्स',
    signOut: 'साइन आउट',
    home: 'होम',
    back: 'सेटिंग्स पर वापस',
  },

  home: {
    sectionLabel: 'आपका खाता',
    welcomeNamed: 'स्वागत है, {name}',
    welcome: 'स्वागत है',
    body: 'आप साइन इन हैं। आपकी जन्म कुंडली चरण 2 में आएगी — तब तक यह खाता काम करने का प्रमाण है।',
    chartTitle: 'आपकी जन्म कुंडली',
    chartBody: 'तारीख, समय और स्थान। हम सही ऐतिहासिक समय-क्षेत्र निकालते हैं — 30 मिनट की त्रुटि पूरी कुंडली बदल सकती है।',
    chartCta: 'अपना जन्म विवरण जोड़ें',
    loading: 'आपका खाता लोड हो रहा है…',
  },

  landing: {
    phase: 'चरण 1 · खाते',
    headlineA: 'यह AI केवल ज्योतिष नहीं जानता।',
    headlineB: 'यह आपका ज्योतिष जानता है।',
    lede: 'एक निजी ज्योतिषी जो आपकी जन्म कुंडली, आपके जीवन संदर्भ और आपकी पिछली सभी बातचीत को समझता है — और ज़रूरत पड़ने पर आपको विशेषज्ञ से जोड़ता है।',
    ctaPrimary: 'अपनी निःशुल्क कुंडली पाएँ',
    ctaSecondary: 'सिस्टम स्थिति देखें',
    ctaNote: 'शुरू करना निःशुल्क। कोई पासवर्ड याद रखने की ज़रूरत नहीं।',
    signIn: 'साइन इन',
    systemStatus: 'सिस्टम स्थिति',
    howLabel: 'यह कैसे काम करता है',
    howTitle: 'जन्म विवरण से वास्तविक मार्गदर्शन तक',
    commitLabel: 'हमारी प्रतिबद्धता',
    commitTitle: 'भरोसे के लिए बनाया गया',
    progressLabel: 'प्रगति',
    progressTitle: 'हम कहाँ हैं',
    progressBody: 'एक समय में एक चरण। हर चरण का एक गेट है जो अगले चरण से पहले पास होना चाहिए।',
    disclaimer: 'मार्गदर्शन और चिंतन के लिए। ज्योतिषीय पठन पेशेवर चिकित्सा, कानूनी या वित्तीय सलाह का विकल्प नहीं हैं।',
  },

  settings: {
    title: 'सेटिंग्स',
    profile: 'प्रोफ़ाइल',
    preferences: 'प्राथमिकताएँ',
    sessions: 'डिवाइस',
    deleteAccount: 'खाता हटाएँ',
    saved: 'सहेजा गया',
    save: 'परिवर्तन सहेजें',

    profileTitle: 'आपकी प्रोफ़ाइल',
    nameLabel: 'नाम',
    genderLabel: 'लिंग',
    emailLabel: 'ईमेल',
    phoneLabel: 'फ़ोन',
    verified: 'सत्यापित',
    unverified: 'असत्यापित',
    contactLocked: 'संपर्क विवरण सत्यापन द्वारा बदले जाते हैं, यहाँ नहीं।',
    notSet: 'सेट नहीं',

    prefsTitle: 'प्राथमिकताएँ',
    languageLabel: 'भाषा',
    systemLabel: 'ज्योतिष पद्धति',
    chartStyleLabel: 'कुंडली शैली',
    themeLabel: 'थीम',

    sessionsTitle: 'साइन-इन डिवाइस',
    sessionsBody: 'डिवाइस रद्द करने पर वह तुरंत साइन आउट हो जाता है।',
    sessionsEmpty: 'कोई अन्य डिवाइस साइन इन नहीं है।',
    signedIn: 'साइन इन',
    expires: 'समाप्ति',
    revoke: 'रद्द करें',
    revoking: 'रद्द किया जा रहा है…',
    signOutEverywhere: 'सभी जगह साइन आउट करें',
    thisDevice: 'यह डिवाइस',

    deleteTitle: 'अपना खाता हटाएँ',
    deleteBody: 'यह आपका खाता और उसमें सब कुछ हटा देता है। छिपाया नहीं, चिह्नित नहीं — हटाया गया।',
    deleteGrace: 'आपके पास मन बदलने के लिए सात दिन हैं। उसके बाद इसे पूर्ववत नहीं किया जा सकता।',
    deleteExportFirst: 'पहले अपना डेटा डाउनलोड करें',
    deleteSendCode: 'मुझे कोड भेजें',
    deleteCodeLabel: 'आपके सत्यापित संपर्क पर भेजा गया कोड',
    deleteConfirmLabel: 'पुष्टि के लिए DELETE लिखें',
    deleteConfirmWord: 'DELETE',
    deleteButton: 'मेरा खाता हटाएँ',
    deleteScheduled: 'आपका खाता {date} को हटा दिया जाएगा।',
    deleteCancel: 'हटाना रद्द करें',
    exportTitle: 'अपना डेटा निर्यात करें',
    exportBody: 'आपके बारे में रखी गई हर चीज़ की पूरी JSON फ़ाइल।',
    exportButton: 'डाउनलोड',
  },

  values: {
    vedic: 'वैदिक',
    western: 'पाश्चात्य',
    north: 'उत्तर',
    south: 'दक्षिण',
    east: 'पूर्व',
    dark: 'गहरा',
    light: 'हल्का',
    system: 'सिस्टम',
    male: 'पुरुष',
    female: 'महिला',
    other: 'अन्य',
    prefer_not_to_say: 'बताना नहीं चाहते',
  },
}

export const dictionaries = { en, hi } as const

export type Locale = keyof typeof dictionaries

/** Locales offered in the UI, with their own endonyms. */
export const LOCALE_NAMES: Record<Locale, string> = {
  en: 'English',
  // The endonym, not "Hindi" — a language picker written in a language
  // you cannot read is not a picker.
  hi: 'हिन्दी',
}

export function getDictionary(locale: string): Dictionary {
  return dictionaries[locale as Locale] ?? en
}

/** Substitutes {placeholders}. Deliberately tiny — no library needed. */
export function interpolate(template: string, values: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (match, key: string) =>
    key in values ? String(values[key]) : match,
  )
}
