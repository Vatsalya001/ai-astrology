import type { Localised } from './types'

/**
 * The planetary combinations `astro-service` detects, described.
 *
 * ── The keys are a contract with Python ──
 *
 * `YogaResult.name` comes off the wire as a display string — "Gajakesari
 * Yoga", "Chandra-Mangal Yoga" — and that string is what looks the entry
 * up. A rename on either side produces a card with a name and no
 * description, which looks like a yoga nobody bothered to write about
 * rather than like a bug. `yogas.test.ts` reads `yoga.py` and compares.
 *
 * ── Where the framing rule is hardest ──
 *
 * Several of these are traditionally read as fortunate and one —
 * Kemadruma — is traditionally read as difficult. That asymmetry is
 * exactly where a corpus slips into telling somebody their chart is bad.
 *
 * The rule does not bend for it. Every entry says what the tradition
 * READS IN a combination, never what it will do to the person holding
 * it, and never that they are in for a hard life. A reader who came
 * looking for reassurance and found a paragraph about misfortune is a
 * reader the product has harmed, and "the classical texts say so" is not
 * a defence when the classical texts were not written to be delivered to
 * a stranger by an app at two in the morning.
 *
 * Phase 5 grounds a model in this text. Whatever register these are
 * written in is the register it will answer in.
 */

export type YogaKey =
  // The five Panch Mahapurusha yogas, one per true planet.
  | 'Ruchaka Yoga'
  | 'Bhadra Yoga'
  | 'Hamsa Yoga'
  | 'Malavya Yoga'
  | 'Sasa Yoga'
  // The rest.
  | 'Gajakesari Yoga'
  | 'Budhaditya Yoga'
  | 'Kemadruma Yoga'
  | 'Chandra-Mangal Yoga'
  | 'Neecha Bhanga Raja Yoga'
  | 'Raja Yoga'

export const YOGAS: Localised<YogaKey> = {
  'Ruchaka Yoga': {
    en: {
      name: 'Ruchaka Yoga',
      short: 'Mars strong, in an angular house.',
      long: 'One of the five Panch Mahapurusha combinations, formed when Mars sits in an angle from the ascendant and in a sign it rules or is exalted in. It is traditionally associated with initiative, physical energy and a direct manner.',
    },
    hi: {
      name: 'रुचक योग',
      short: 'मंगल बलवान, केंद्र भाव में।',
      long: 'पंच महापुरुष योगों में से एक, जो मंगल के लग्न से केंद्र में और अपनी या उच्च राशि में होने पर बनता है। परंपरा में इसे पहल, शारीरिक ऊर्जा और सीधे स्वभाव से जोड़ा जाता है।',
    },
  },
  'Bhadra Yoga': {
    en: {
      name: 'Bhadra Yoga',
      short: 'Mercury strong, in an angular house.',
      long: 'A Panch Mahapurusha combination formed by Mercury in an angle from the ascendant, in its own sign or exalted. It is traditionally associated with clear speech, analysis and skill with language or numbers.',
    },
    hi: {
      name: 'भद्र योग',
      short: 'बुध बलवान, केंद्र भाव में।',
      long: 'पंच महापुरुष योग, जो बुध के लग्न से केंद्र में, अपनी या उच्च राशि में होने पर बनता है। परंपरा में इसे स्पष्ट वाणी, विश्लेषण और भाषा या गणित के कौशल से जोड़ा जाता है।',
    },
  },
  'Hamsa Yoga': {
    en: {
      name: 'Hamsa Yoga',
      short: 'Jupiter strong, in an angular house.',
      long: 'A Panch Mahapurusha combination formed by Jupiter in an angle from the ascendant, in its own sign or exalted. It is traditionally associated with teaching, generosity and an interest in principle rather than advantage.',
    },
    hi: {
      name: 'हंस योग',
      short: 'गुरु बलवान, केंद्र भाव में।',
      long: 'पंच महापुरुष योग, जो गुरु के लग्न से केंद्र में, अपनी या उच्च राशि में होने पर बनता है। परंपरा में इसे शिक्षण, उदारता और लाभ के बजाय सिद्धांत में रुचि से जोड़ा जाता है।',
    },
  },
  'Malavya Yoga': {
    en: {
      name: 'Malavya Yoga',
      short: 'Venus strong, in an angular house.',
      long: 'A Panch Mahapurusha combination formed by Venus in an angle from the ascendant, in its own sign or exalted. It is traditionally associated with an eye for beauty, comfort and the arts.',
    },
    hi: {
      name: 'मालव्य योग',
      short: 'शुक्र बलवान, केंद्र भाव में।',
      long: 'पंच महापुरुष योग, जो शुक्र के लग्न से केंद्र में, अपनी या उच्च राशि में होने पर बनता है। परंपरा में इसे सौंदर्यबोध, सुख-सुविधा और कलाओं से जोड़ा जाता है।',
    },
  },
  'Sasa Yoga': {
    en: {
      name: 'Sasa Yoga',
      short: 'Saturn strong, in an angular house.',
      long: 'A Panch Mahapurusha combination formed by Saturn in an angle from the ascendant, in its own sign or exalted. It is traditionally associated with endurance, discipline and authority earned slowly rather than given.',
    },
    hi: {
      name: 'शश योग',
      short: 'शनि बलवान, केंद्र भाव में।',
      long: 'पंच महापुरुष योग, जो शनि के लग्न से केंद्र में, अपनी या उच्च राशि में होने पर बनता है। परंपरा में इसे सहनशीलता, अनुशासन और धीरे-धीरे अर्जित अधिकार से जोड़ा जाता है।',
    },
  },
  'Gajakesari Yoga': {
    en: {
      name: 'Gajakesari Yoga',
      short: 'Jupiter in an angle from the Moon.',
      long: 'Formed when Jupiter stands in the 1st, 4th, 7th or 10th house counted from the Moon. Named for the elephant and the lion, it is traditionally associated with steadiness of mind and with regard earned from others.',
    },
    hi: {
      name: 'गजकेसरी योग',
      short: 'चंद्रमा से केंद्र में गुरु।',
      long: 'तब बनता है जब गुरु चंद्रमा से पहले, चौथे, सातवें या दसवें भाव में हो। हाथी और सिंह के नाम पर, परंपरा में इसे मन की स्थिरता और दूसरों से मिलने वाले सम्मान से जोड़ा जाता है।',
    },
  },
  'Budhaditya Yoga': {
    en: {
      name: 'Budhaditya Yoga',
      short: 'Mercury with the Sun in one sign.',
      long: 'The Sun and Mercury together in the same sign. Common, because Mercury never travels far from the Sun, and traditionally read for intelligence applied to the affairs of whichever house they share.',
    },
    hi: {
      name: 'बुधादित्य योग',
      short: 'एक ही राशि में बुध और सूर्य।',
      long: 'सूर्य और बुध का एक ही राशि में होना। यह सामान्य है, क्योंकि बुध सूर्य से कभी दूर नहीं जाता, और परंपरा में जिस भाव में ये हों उसके विषयों में लगाई गई बुद्धि के लिए देखा जाता है।',
    },
  },
  /*
    The one traditionally read as difficult, and the reason the framing
    rule in this file's header is written at the length it is.

    The classical reading is bleak. Delivered verbatim to a stranger on a
    phone it is not information, it is a prediction of a hard life from
    something they cannot change — which this product does not make,
    about anything, under any framing.

    So: what the configuration IS, what the tradition attends to, and the
    part every serious text includes and every frightening summary drops
    — that it is cancelled by very ordinary placements. No adjective
    about the person's life, and nothing about what will happen.
  */
  'Kemadruma Yoga': {
    en: {
      name: 'Kemadruma Yoga',
      short: 'No planet in the signs on either side of the Moon.',
      long: 'A configuration in which the signs immediately before and after the Moon are both empty of planets. Classical texts treat it as a reason to read the Moon carefully rather than as a verdict, and every standard text also lists the placements that cancel it — a planet in an angle from the Moon, or the Moon aspected by Jupiter, among others — which are common enough that the configuration alone says little.',
    },
    hi: {
      name: 'केमद्रुम योग',
      short: 'चंद्रमा के दोनों ओर की राशियों में कोई ग्रह नहीं।',
      long: 'ऐसी स्थिति जिसमें चंद्रमा से ठीक पहले और ठीक बाद की राशियाँ दोनों ग्रहों से रिक्त हों। शास्त्र इसे किसी निर्णय के बजाय चंद्रमा को ध्यान से देखने का कारण मानते हैं, और हर मानक ग्रंथ उन स्थितियों को भी गिनाता है जो इसे भंग करती हैं — चंद्रमा से केंद्र में कोई ग्रह, या चंद्रमा पर गुरु की दृष्टि — जो इतनी सामान्य हैं कि अकेली यह स्थिति बहुत कम कहती है।',
    },
  },
  'Chandra-Mangal Yoga': {
    en: {
      name: 'Chandra-Mangal Yoga',
      short: 'Moon and Mars together or in mutual aspect.',
      long: 'The Moon and Mars conjunct in one sign, or looking at each other across the chart. It is traditionally read for drive attached to feeling — the tradition connects it with enterprise and with resourcefulness in practical matters.',
    },
    hi: {
      name: 'चंद्र-मंगल योग',
      short: 'चंद्रमा और मंगल साथ या परस्पर दृष्ट।',
      long: 'चंद्रमा और मंगल का एक राशि में होना, या चार्ट के आर-पार एक-दूसरे को देखना। परंपरा में इसे भावना से जुड़ी प्रेरणा के लिए पढ़ा जाता है — परंपरा इसे उद्यम और व्यावहारिक मामलों में साधन-संपन्नता से जोड़ती है।',
    },
  },
  'Neecha Bhanga Raja Yoga': {
    en: {
      name: 'Neecha Bhanga Raja Yoga',
      short: 'A debilitated planet whose weakness is cancelled.',
      long: 'Formed when a planet in its sign of debilitation has that weakness undone — most often because the lord of that sign, or the planet exalted there, sits in an angle from the ascendant or the Moon. The tradition treats the reversal itself as the significant part, and reads it for strength that arrives by an unlikely route.',
    },
    hi: {
      name: 'नीच भंग राज योग',
      short: 'नीच ग्रह जिसकी निर्बलता भंग हो जाए।',
      long: 'तब बनता है जब अपनी नीच राशि में बैठे ग्रह की निर्बलता समाप्त हो जाए — प्रायः तब, जब उस राशि का स्वामी, या उसमें उच्च होने वाला ग्रह, लग्न या चंद्रमा से केंद्र में हो। परंपरा इस उलटफेर को ही महत्वपूर्ण मानती है, और इसे असंभावित मार्ग से आने वाले बल के लिए पढ़ती है।',
    },
  },
  'Raja Yoga': {
    en: {
      name: 'Raja Yoga',
      short: 'An angular house lord joined with a trinal house lord.',
      long: 'Formed when the lord of an angular house (1st, 4th, 7th, 10th) and the lord of a trinal house (1st, 5th, 9th) come together — conjunct, in exchange, or aspecting each other. The name means "royal combination", and the tradition reads it for capability and circumstance arriving at the same time rather than for rank.',
    },
    hi: {
      name: 'राज योग',
      short: 'केंद्र भाव के स्वामी का त्रिकोण भाव के स्वामी से योग।',
      long: 'तब बनता है जब केंद्र भाव (1, 4, 7, 10) के स्वामी और त्रिकोण भाव (1, 5, 9) के स्वामी मिलें — युति में, राशि-परिवर्तन में, या परस्पर दृष्टि में। नाम का अर्थ "राजसी योग" है, और परंपरा इसे पद के बजाय सामर्थ्य और परिस्थिति के एक साथ आने के लिए पढ़ती है।',
    },
  },
}

export const YOGA_KEYS = Object.keys(YOGAS) as YogaKey[]

/**
 * A yoga's description, or null.
 *
 * Null rather than a placeholder, for the same reason `defineTerm`
 * returns null: a card headed "Gajakesari Yoga" with no paragraph is
 * honest, and one headed "Gajakesari Yoga" above invented filler is not.
 */
export function describeYoga(name: string, locale: 'en' | 'hi') {
  const entry = (YOGAS as Record<string, Record<'en' | 'hi', unknown>>)[name]
  if (!entry) return null
  return (entry[locale] as (typeof YOGAS)[YogaKey]['en'] | undefined) ?? null
}

export function hasYoga(name: string): name is YogaKey {
  return Object.prototype.hasOwnProperty.call(YOGAS, name)
}
