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
