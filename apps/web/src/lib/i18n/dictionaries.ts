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

  birth: {
    // Three steps, one question per screen. This is the highest
    // drop-off point in the whole product; every field costs conversion.
    stepOf: 'Step {current} of {total}',

    dateTitle: 'When were you born?',
    dateSubtitle: 'Your date of birth, as it appears on your documents.',
    dayLabel: 'Day',
    monthLabel: 'Month',
    yearLabel: 'Year',
    dateInvalid: 'That date doesn’t exist. Please check the day and month.',
    dateFuture: 'That date is in the future.',
    dateTooOld: 'Please enter a year after 1900.',

    timeTitle: 'What time were you born?',
    timeSubtitle: 'As close as you know. Even fifteen minutes matters.',
    hourLabel: 'Hour',
    minuteLabel: 'Minute',
    timeInvalid: 'Please enter a time between 00:00 and 23:59.',
    unknownLabel: 'I don’t know my exact birth time',
    // Says plainly what is lost rather than hiding it. The checkbox
    // converts a dead end into a completed signup; pretending nothing
    // changes converts it into a wrong chart.
    unknownExplained:
      'We’ll show your planetary positions. Your rising sign and dasha periods need an exact time — you can add it any time later.',

    placeTitle: 'Where were you born?',
    placeSubtitle: 'The town or city. We’ll work out the rest.',
    placeLabel: 'Birth place',
    placePlaceholder: 'Start typing a town or city',
    placeKeepTyping: 'Keep typing to search.',
    placeNoResults: 'No places found. Try a nearby larger town.',
    placeSearching: 'Searching…',
    placeSelected: 'Selected',
    placeRequired: 'Please choose your birth place from the list.',
    placeSearchFailed: 'We couldn’t search places just now.',

    submit: 'See my Kundli',

    computingTitle: 'Reading the sky',
    computingSubtitle: 'Working out where every planet was at the moment you were born.',
    computingFailed: 'We couldn’t compute your chart.',
    computingRetry: 'Try again',
  },

  profiles: {
    title: 'Birth profiles',
    subtitle: 'Your details, and anyone else’s you’ve added.',
    empty: 'Add your birth details to get started.',
    emptyCta: 'Add birth details',
    add: 'Add a profile',
    edit: 'Edit',
    remove: 'Remove',
    removeConfirm: 'Remove this profile?',
    // Soft delete, and it says so: the charts and readings that
    // reference it stay explicable.
    removeExplained:
      'Past readings stay readable. You can add these details again at any time.',
    versionLabel: 'Version {version}',
    unknownTime: 'Birth time not set',
    unknownTimeCta: 'Add the time',
    // Persistent, not nagging. Shown once per profile card.
    unknownTimeBanner:
      'Your rising sign and dasha periods need an exact birth time.',
    loading: 'Loading your profiles…',
    failed: 'We couldn’t load your profiles.',

    editTitle: 'Edit birth details',
    // The warning is the point of the screen. Correcting a birth time
    // creates a new version, and a reading given last month stays
    // attached to the old one.
    editWarning:
      'Saving creates a new version. Readings you’ve already had stay based on the details they were computed from.',
    editSaved: 'Saved as version {version}.',
    history: 'Version history',
    historyEmpty: 'No earlier versions.',
  },

  nav: {
    settings: 'Settings',
    signOut: 'Sign out',
    home: 'Home',
    back: 'Back to settings',
  },

  home: {
    loading: 'Loading your account…',

    // Four combinations: with and without a clock, with and without a
    // name. The server knows neither, so both absences are real.
    greetMorning: 'Good morning',
    greetAfternoon: 'Good afternoon',
    greetEvening: 'Good evening',
    greetNeutral: 'Hello',
    greetWithName: '{greeting}, {name}',

    todayLabel: 'Today',
    todayMoonIn: 'Moon in {sign}',
    todayNoSky: 'Today’s sky is still being prepared. Positions are computed every six hours.',
    todayNoNote: 'No note written for this sign yet.',

    askTitle: 'Ask your AI astrologer',
    askPlaceholder: 'What’s on your mind?',
    askDisabled:
      'Not available yet. Everything on this screen so far is computed from your chart, with no AI involved — that part comes later.',
    askCareer: 'Career',
    askLove: 'Love',
    askMoney: 'Money',
    askMarriage: 'Marriage',

    periodLabel: 'Your current period',
    periodNoBirthTime: 'Dasha periods need a birth time. Add one and this fills in.',
    periodElapsed: '{percent}% elapsed',
    periodElapsedLabel: '{planet} mahadasha elapsed',
    periodAntardasha: 'Antardasha: {planet}',
    periodAntardashaTo: 'Antardasha: {planet} (to {end})',
    periodSeeAll: 'See all periods',

    viewKundli: 'View my full Kundli',
    talkToAstrologer: 'Talk to an astrologer',
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

  chart: {
    // A screen reader otherwise announces "Nakshatra, button" and gives
    // the user nothing to decide with. {term} is the word itself.
    defineTerm: 'What “{term}” means',

    // Shared across every Kundli screen.
    loading: 'Loading chart…',
    tryAgain: 'Try again',
    addBirthDetails: 'Add birth details',
    unreadable:
      'This chart could not be read. Nothing is lost — your birth details are saved. Recomputing usually fixes it.',

    chartTitle: 'Your Kundli',
    styleLegend: 'Style',
    seePositions: 'See every position in a table',
    planetsTitle: 'Planets & houses',
    planetsNoProfile:
      'There is no birth chart yet. Add your birth date, time and place and this fills in.',
    planetsSection: 'Planetary positions',
    housesSection: 'Houses',
    planetsEmpty: 'No planetary positions for this chart.',
    planetsCaption:
      'Planetary positions: sign, degree, house, nakshatra and dignity for each planet.',
    srTableCaption: 'Planetary positions — the same data as the chart above, as a table.',

    colPlanet: 'Planet',
    colSign: 'Sign',
    colDegree: 'Degree',
    colHouse: 'House',
    colNakshatra: 'Nakshatra',
    colStatus: 'Status',

    housesNoBirthTime:
      'Houses need a birth time. Without one the rising sign — and so every house — would be a guess, and a guess shown as a fact is worse than nothing. Add a birth time to your profile and this fills in.',
    houseEmpty: 'empty',
    housePlanetsHere: 'Planets here',
    houseNoPlanets: 'None. An empty house is read through its lord — {lord} here — not as an absence.',
    houseNumbered: '{ordinal} house',
    houseRowLabel: '{ordinal} house, {sign}, ruled by {lord}, {occupants}',

    vargaLegend: 'Chart',

    dashasTitle: 'Dasha periods',
    dashasLoading: 'Loading your dasha periods…',
    dashasNoProfile:
      'Dashas are read from your birth chart. Add your birth details and this fills in.',
    dashasNoBirthTime:
      'Dashas need a birth time. The sequence starts from the Moon’s exact position at birth, which cannot be pinned down without one.',
    dashasAddBirthTime: 'Add a birth time',
    dashaCurrent: 'Current period',
    dashaLevelFailed: 'These periods could not be loaded.',
    dashaChooseParent: 'Choose a {parent} above to see its periods.',
    dashaPeriodsOf: '{level} periods',

    transitsLoading: 'Loading transits…',
    transitsNoProfile:
      'Transits are read against your birth chart. Add your birth details and this fills in — a birth date is enough, a time is not needed here.',
    transitsNotYet:
      'Today’s sky is still being prepared. Positions are computed every six hours; this usually resolves within a few minutes of a fresh start.',
    transitsPageTitle: 'Transits',
    transitsTitle: 'Right now in the sky',
    transitsFrame: 'Houses counted from your Moon in {sign}, the traditional frame for',
    transitsComputed: 'Computed {when}.',
    transitsEmpty: 'No transit positions have been computed yet. They refresh every six hours.',
    transitsList: 'Transiting planets',
    transitsFromMoon: '{ordinal} from Moon',
    transitRowLabel: '{planet}, in {sign}, {ordinal} house from your Moon',

    sadeSatiTitle: 'Sade Sati',
    sadeSatiInactive:
      'Not currently running. Saturn is in {sign}, the {ordinal} sign from your Moon.',
    sadeSatiActive: 'Saturn is in {sign}, the {ordinal} sign from your Moon.',
    sadeSatiPhaseLabel: 'Sade Sati phase',
    sadeSatiCurrentPhase: ' — current phase',
    sadeSatiRising: 'Rising',
    sadeSatiPeak: 'Peak',
    sadeSatiSetting: 'Setting',
    sadeSatiNoPhase: 'The phase was not reported for this reading.',

    yogasTitle: 'Yogas',
    yogasLoading: 'Looking for combinations…',
    yogasNoProfile:
      'Yogas are read from your birth chart. Add your birth details and this fills in.',
    yogasUnreadable:
      'This chart could not be read. Your birth details are saved; recomputing usually fixes it.',
    yogasNone:
      'The engine checked this chart for eleven classical combinations and found none of them. That is ordinary — most of them need placements that are uncommon by construction, which is what makes them worth naming when they do appear.',
    yogaStrong: 'Strong',
    yogaModerate: 'Moderate',
    yogaNoDescription: 'No description written for this combination yet.',
    yogaCardLabel: '{name}, {strength} strength, {planets}',
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

  birth: {
    stepOf: 'चरण {current} / {total}',

    dateTitle: 'आपका जन्म कब हुआ था?',
    dateSubtitle: 'आपकी जन्म तिथि, जैसी आपके दस्तावेज़ों में है।',
    dayLabel: 'दिन',
    monthLabel: 'महीना',
    yearLabel: 'वर्ष',
    dateInvalid: 'यह तिथि मौजूद नहीं है। कृपया दिन और महीना जाँचें।',
    dateFuture: 'यह तिथि भविष्य में है।',
    dateTooOld: 'कृपया 1900 के बाद का वर्ष दर्ज करें।',

    timeTitle: 'आपका जन्म किस समय हुआ था?',
    timeSubtitle: 'जितना आप जानते हैं। पंद्रह मिनट भी मायने रखते हैं।',
    hourLabel: 'घंटा',
    minuteLabel: 'मिनट',
    timeInvalid: 'कृपया 00:00 से 23:59 के बीच का समय दर्ज करें।',
    unknownLabel: 'मुझे अपना सही जन्म समय नहीं पता',
    unknownExplained:
      'हम आपके ग्रहों की स्थिति दिखाएँगे। आपकी लग्न राशि और दशा अवधि के लिए सही समय चाहिए — आप इसे बाद में कभी भी जोड़ सकते हैं।',

    placeTitle: 'आपका जन्म कहाँ हुआ था?',
    placeSubtitle: 'शहर या कस्बा। बाकी हम देख लेंगे।',
    placeLabel: 'जन्म स्थान',
    placePlaceholder: 'शहर या कस्बे का नाम लिखें',
    placeKeepTyping: 'खोजने के लिए लिखते रहें।',
    placeNoResults: 'कोई स्थान नहीं मिला। पास का कोई बड़ा शहर आज़माएँ।',
    placeSearching: 'खोजा जा रहा है…',
    placeSelected: 'चुना गया',
    placeRequired: 'कृपया सूची से अपना जन्म स्थान चुनें।',
    placeSearchFailed: 'हम अभी स्थान नहीं खोज सके।',

    submit: 'मेरी कुंडली देखें',

    computingTitle: 'आकाश पढ़ा जा रहा है',
    computingSubtitle: 'आपके जन्म के क्षण हर ग्रह कहाँ था, यह निकाला जा रहा है।',
    computingFailed: 'हम आपकी कुंडली नहीं बना सके।',
    computingRetry: 'पुनः प्रयास करें',
  },

  profiles: {
    title: 'जन्म प्रोफ़ाइल',
    subtitle: 'आपका विवरण, और जिन्हें आपने जोड़ा है।',
    empty: 'शुरू करने के लिए अपना जन्म विवरण जोड़ें।',
    emptyCta: 'जन्म विवरण जोड़ें',
    add: 'प्रोफ़ाइल जोड़ें',
    edit: 'संपादित करें',
    remove: 'हटाएँ',
    removeConfirm: 'यह प्रोफ़ाइल हटाएँ?',
    removeExplained:
      'पुराने पठन पढ़े जा सकेंगे। आप यह विवरण कभी भी दोबारा जोड़ सकते हैं।',
    versionLabel: 'संस्करण {version}',
    unknownTime: 'जन्म समय दर्ज नहीं है',
    unknownTimeCta: 'समय जोड़ें',
    unknownTimeBanner:
      'आपकी लग्न राशि और दशा अवधि के लिए सही जन्म समय चाहिए।',
    loading: 'आपकी प्रोफ़ाइल लोड हो रही हैं…',
    failed: 'हम आपकी प्रोफ़ाइल लोड नहीं कर सके।',

    editTitle: 'जन्म विवरण संपादित करें',
    editWarning:
      'सहेजने पर एक नया संस्करण बनता है। आपको पहले मिले पठन उन्हीं विवरणों पर आधारित रहेंगे जिनसे वे बने थे।',
    editSaved: 'संस्करण {version} के रूप में सहेजा गया।',
    history: 'संस्करण इतिहास',
    historyEmpty: 'कोई पुराना संस्करण नहीं।',
  },

  nav: {
    settings: 'सेटिंग्स',
    signOut: 'साइन आउट',
    home: 'होम',
    back: 'सेटिंग्स पर वापस',
  },

  home: {
    loading: 'आपका खाता लोड हो रहा है…',

    greetMorning: 'सुप्रभात',
    greetAfternoon: 'नमस्कार',
    greetEvening: 'शुभ संध्या',
    greetNeutral: 'नमस्ते',
    greetWithName: '{greeting}, {name}',

    todayLabel: 'आज',
    todayMoonIn: '{sign} में चंद्रमा',
    todayNoSky: 'आज का आकाश अभी तैयार हो रहा है। स्थितियाँ हर छह घंटे में गणना की जाती हैं।',
    todayNoNote: 'इस राशि के लिए अभी कोई टिप्पणी नहीं लिखी गई है।',

    askTitle: 'अपने AI ज्योतिषी से पूछें',
    askPlaceholder: 'आपके मन में क्या है?',
    askDisabled:
      'अभी उपलब्ध नहीं। इस स्क्रीन पर अब तक सब कुछ आपकी कुंडली से गणना किया गया है, बिना किसी AI के — वह हिस्सा बाद में आएगा।',
    askCareer: 'करियर',
    askLove: 'प्रेम',
    askMoney: 'धन',
    askMarriage: 'विवाह',

    periodLabel: 'आपकी वर्तमान दशा',
    periodNoBirthTime: 'दशाओं के लिए जन्म समय चाहिए। एक जोड़ें और यह भर जाएगा।',
    periodElapsed: '{percent}% बीत चुका',
    periodElapsedLabel: '{planet} महादशा बीत चुकी',
    periodAntardasha: 'अंतर्दशा: {planet}',
    periodAntardashaTo: 'अंतर्दशा: {planet} ({end} तक)',
    periodSeeAll: 'सभी दशाएँ देखें',

    viewKundli: 'मेरी पूरी कुंडली देखें',
    talkToAstrologer: 'ज्योतिषी से बात करें',
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

  chart: {
    defineTerm: '“{term}” का अर्थ',

    loading: 'कुंडली लोड हो रही है…',
    tryAgain: 'फिर कोशिश करें',
    addBirthDetails: 'जन्म विवरण जोड़ें',
    unreadable:
      'यह कुंडली पढ़ी नहीं जा सकी। कुछ भी खोया नहीं — आपके जन्म विवरण सुरक्षित हैं। पुनर्गणना से आमतौर पर ठीक हो जाता है।',

    chartTitle: 'आपकी कुंडली',
    styleLegend: 'शैली',
    seePositions: 'हर स्थिति तालिका में देखें',
    planetsTitle: 'ग्रह और भाव',
    planetsNoProfile:
      'अभी कोई जन्म कुंडली नहीं है। अपनी जन्म तिथि, समय और स्थान जोड़ें और यह भर जाएगी।',
    planetsSection: 'ग्रह स्थितियाँ',
    housesSection: 'भाव',
    planetsEmpty: 'इस कुंडली के लिए कोई ग्रह स्थिति नहीं।',
    planetsCaption: 'ग्रह स्थितियाँ: हर ग्रह की राशि, अंश, भाव, नक्षत्र और बल।',
    srTableCaption: 'ग्रह स्थितियाँ — ऊपर की कुंडली का ही डेटा, तालिका के रूप में।',

    colPlanet: 'ग्रह',
    colSign: 'राशि',
    colDegree: 'अंश',
    colHouse: 'भाव',
    colNakshatra: 'नक्षत्र',
    colStatus: 'स्थिति',

    housesNoBirthTime:
      'भावों के लिए जन्म समय चाहिए। उसके बिना लग्न — और इसलिए हर भाव — केवल अनुमान होगा, और अनुमान को तथ्य की तरह दिखाना कुछ न दिखाने से बुरा है। अपनी प्रोफ़ाइल में जन्म समय जोड़ें और यह भर जाएगा।',
    houseEmpty: 'खाली',
    housePlanetsHere: 'यहाँ के ग्रह',
    houseNoPlanets: 'कोई नहीं। खाली भाव उसके स्वामी — यहाँ {lord} — से पढ़ा जाता है, अनुपस्थिति के रूप में नहीं।',
    houseNumbered: '{ordinal} भाव',
    houseRowLabel: '{ordinal} भाव, {sign}, स्वामी {lord}, {occupants}',

    vargaLegend: 'कुंडली',

    dashasTitle: 'दशा काल',
    dashasLoading: 'आपकी दशाएँ लोड हो रही हैं…',
    dashasNoProfile: 'दशाएँ आपकी जन्म कुंडली से पढ़ी जाती हैं। जन्म विवरण जोड़ें और यह भर जाएगा।',
    dashasNoBirthTime:
      'दशाओं के लिए जन्म समय चाहिए। क्रम जन्म के समय चंद्रमा की ठीक स्थिति से शुरू होता है, जो समय के बिना तय नहीं हो सकती।',
    dashasAddBirthTime: 'जन्म समय जोड़ें',
    dashaCurrent: 'वर्तमान दशा',
    dashaLevelFailed: 'ये दशाएँ लोड नहीं हो सकीं।',
    dashaChooseParent: 'इसकी दशाएँ देखने के लिए ऊपर एक {parent} चुनें।',
    dashaPeriodsOf: '{level} काल',

    transitsLoading: 'गोचर लोड हो रहे हैं…',
    transitsNoProfile:
      'गोचर आपकी जन्म कुंडली के सापेक्ष पढ़े जाते हैं। जन्म विवरण जोड़ें और यह भर जाएगा — यहाँ जन्म तिथि पर्याप्त है, समय आवश्यक नहीं।',
    transitsNotYet:
      'आज का आकाश अभी तैयार हो रहा है। स्थितियाँ हर छह घंटे में गणना होती हैं; नई शुरुआत के कुछ मिनटों में यह ठीक हो जाता है।',
    transitsPageTitle: 'गोचर',
    transitsTitle: 'इस समय आकाश में',
    transitsFrame: '{sign} में आपके चंद्रमा से गिने गए भाव, जो इसका पारंपरिक आधार है —',
    transitsComputed: '{when} पर गणना की गई।',
    transitsEmpty: 'अभी तक कोई गोचर स्थिति गणना नहीं हुई है। ये हर छह घंटे में ताज़ा होती हैं।',
    transitsList: 'गोचर करते ग्रह',
    transitsFromMoon: 'चंद्रमा से {ordinal}',
    transitRowLabel: '{planet}, {sign} में, आपके चंद्रमा से {ordinal} भाव',

    sadeSatiTitle: 'साढ़े साती',
    sadeSatiInactive: 'अभी नहीं चल रही। शनि {sign} में है, आपके चंद्रमा से {ordinal} राशि।',
    sadeSatiActive: 'शनि {sign} में है, आपके चंद्रमा से {ordinal} राशि।',
    sadeSatiPhaseLabel: 'साढ़े साती चरण',
    sadeSatiCurrentPhase: ' — वर्तमान चरण',
    sadeSatiRising: 'आरोहण',
    sadeSatiPeak: 'शिखर',
    sadeSatiSetting: 'अवरोहण',
    sadeSatiNoPhase: 'इस गणना के लिए चरण नहीं बताया गया।',

    yogasTitle: 'योग',
    yogasLoading: 'योग खोजे जा रहे हैं…',
    yogasNoProfile: 'योग आपकी जन्म कुंडली से पढ़े जाते हैं। जन्म विवरण जोड़ें और यह भर जाएगा।',
    yogasUnreadable:
      'यह कुंडली पढ़ी नहीं जा सकी। आपके जन्म विवरण सुरक्षित हैं; पुनर्गणना से आमतौर पर ठीक हो जाता है।',
    yogasNone:
      'इंजन ने इस कुंडली में ग्यारह शास्त्रीय योग खोजे और उनमें से कोई नहीं मिला। यह सामान्य है — इनमें से अधिकांश के लिए ऐसी स्थितियाँ चाहिए जो स्वभाव से ही दुर्लभ हैं, और यही उन्हें तब उल्लेखनीय बनाता है जब वे बनते हैं।',
    yogaStrong: 'प्रबल',
    yogaModerate: 'मध्यम',
    yogaNoDescription: 'इस योग के लिए अभी कोई विवरण नहीं लिखा गया है।',
    yogaCardLabel: '{name}, {strength} बल, {planets}',
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
