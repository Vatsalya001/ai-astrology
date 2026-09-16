import type { Localised } from './types'

/**
 * What the tradition reads into the Moon's sign on a given day.
 *
 * ── Why these are keyed on the SIGN, not the nakshatra ──
 *
 * The spec's Today card shows "Moon in Rohini · Taurus" — nakshatra and
 * sign. `GET /astrology/transits` returns the transiting Moon's sign,
 * degree and longitude, and no nakshatra. Deriving one from the
 * longitude here is arithmetic anybody could write in four lines, and it
 * is exactly the four lines this project forbids: astrology is computed
 * by `astro-service` or it is not shown. So the card shows the sign,
 * which the engine reports, and not the nakshatra, which it does not.
 *
 * Same for tithi, which the spec also lists: `astro-service` has no
 * tithi calculation at all. Both are gaps to close in the engine.
 *
 * ── Why this is the same line for everybody ──
 *
 * The transiting Moon is in one sign for everyone alive that day. This
 * is a general note about the day, not a reading of anybody's chart, and
 * the copy says so — "traditionally read as", never "your day will be".
 * A personalised daily line needs the natal chart and arrives in Phase 6
 * when a model composes it against real context.
 *
 * Twelve entries, one per sign, and the Moon changes sign about every
 * two and a quarter days — so a reader sees roughly twelve distinct
 * lines a month rather than the same one twice running.
 */

export type MoonSign =
  | 'Aries'
  | 'Taurus'
  | 'Gemini'
  | 'Cancer'
  | 'Leo'
  | 'Virgo'
  | 'Libra'
  | 'Scorpio'
  | 'Sagittarius'
  | 'Capricorn'
  | 'Aquarius'
  | 'Pisces'

export const MOON_DAYS: Localised<MoonSign> = {
  Aries: {
    en: {
      name: 'Moon in Aries',
      short: 'Traditionally read as a day for starting rather than finishing.',
      long: 'The Moon in Aries is traditionally associated with impatience and initiative. The tradition reads it as a time when beginnings come easily and follow-through is the harder half.',
    },
    hi: {
      name: 'मेष राशि में चंद्रमा',
      short: 'परंपरा में इसे पूरा करने से अधिक शुरू करने का दिन माना जाता है।',
      long: 'मेष राशि का चंद्रमा परंपरा में अधीरता और पहल से जुड़ा है। परंपरा इसे ऐसे समय के रूप में पढ़ती है जब शुरुआत सहज होती है और उसे निभाना कठिन आधा हिस्सा।',
    },
  },
  Taurus: {
    en: {
      name: 'Moon in Taurus',
      short: 'Traditionally read as a steady day, suited to practical work.',
      long: 'The Moon is comfortable in Taurus by classical reckoning — it is exalted here. The tradition associates it with patience, appetite and work that rewards being done slowly.',
    },
    hi: {
      name: 'वृषभ राशि में चंद्रमा',
      short: 'परंपरा में इसे स्थिर दिन माना जाता है, व्यावहारिक कार्यों के लिए उपयुक्त।',
      long: 'शास्त्रीय गणना में चंद्रमा वृषभ में सहज है — यहाँ वह उच्च का होता है। परंपरा इसे धैर्य, रुचि और धीरे-धीरे करने से फलने वाले कार्य से जोड़ती है।',
    },
  },
  Gemini: {
    en: {
      name: 'Moon in Gemini',
      short: 'Traditionally read as a day for talking and reading.',
      long: 'The Moon in Gemini is traditionally associated with restlessness of mind, conversation and quick switching between subjects. The tradition reads it as favouring exchange over depth.',
    },
    hi: {
      name: 'मिथुन राशि में चंद्रमा',
      short: 'परंपरा में इसे बातचीत और पढ़ने का दिन माना जाता है।',
      long: 'मिथुन का चंद्रमा परंपरा में मन की चंचलता, संवाद और विषयों के बीच तेज़ी से बदलने से जुड़ा है। परंपरा इसे गहराई से अधिक आदान-प्रदान के अनुकूल पढ़ती है।',
    },
  },
  Cancer: {
    en: {
      name: 'Moon in Cancer',
      short: 'Traditionally read as a day for home and for familiar company.',
      long: 'The Moon rules Cancer, so classical texts treat it as being at home here. The tradition associates the placement with memory, family and a preference for the familiar.',
    },
    hi: {
      name: 'कर्क राशि में चंद्रमा',
      short: 'परंपरा में इसे घर और निकट के लोगों का दिन माना जाता है।',
      long: 'चंद्रमा कर्क का स्वामी है, इसलिए शास्त्र इसे यहाँ अपने घर में मानते हैं। परंपरा इस स्थिति को स्मृति, परिवार और परिचित के प्रति झुकाव से जोड़ती है।',
    },
  },
  Leo: {
    en: {
      name: 'Moon in Leo',
      short: 'Traditionally read as a day for being seen.',
      long: 'The Moon in Leo is traditionally associated with warmth, display and wanting work to be recognised. The tradition reads it as favouring the visible over the quiet.',
    },
    hi: {
      name: 'सिंह राशि में चंद्रमा',
      short: 'परंपरा में इसे दिखने-सामने आने का दिन माना जाता है।',
      long: 'सिंह का चंद्रमा परंपरा में ऊष्मा, प्रदर्शन और कार्य की पहचान की चाह से जुड़ा है। परंपरा इसे शांत के बजाय दृश्य के अनुकूल पढ़ती है।',
    },
  },
  Virgo: {
    en: {
      name: 'Moon in Virgo',
      short: 'Traditionally read as a day for detail and for tidying up.',
      long: 'The Moon in Virgo is traditionally associated with analysis, method and noticing what is out of place. The tradition reads it as favouring correction over invention.',
    },
    hi: {
      name: 'कन्या राशि में चंद्रमा',
      short: 'परंपरा में इसे बारीकी और व्यवस्थित करने का दिन माना जाता है।',
      long: 'कन्या का चंद्रमा परंपरा में विश्लेषण, पद्धति और अव्यवस्थित को पहचानने से जुड़ा है। परंपरा इसे नवाचार के बजाय सुधार के अनुकूल पढ़ती है।',
    },
  },
  Libra: {
    en: {
      name: 'Moon in Libra',
      short: 'Traditionally read as a day for agreements and for company.',
      long: 'The Moon in Libra is traditionally associated with balance, negotiation and an unwillingness to decide alone. The tradition reads it as favouring arrangement over assertion.',
    },
    hi: {
      name: 'तुला राशि में चंद्रमा',
      short: 'परंपरा में इसे समझौतों और साथ का दिन माना जाता है।',
      long: 'तुला का चंद्रमा परंपरा में संतुलन, बातचीत और अकेले निर्णय लेने की अनिच्छा से जुड़ा है। परंपरा इसे आग्रह के बजाय व्यवस्था के अनुकूल पढ़ती है।',
    },
  },
  Scorpio: {
    en: {
      name: 'Moon in Scorpio',
      short: 'Traditionally read as a day for the things that are not said out loud.',
      long: 'Classical texts place the Moon in its sign of debilitation here and read the placement as an intense one: the tradition associates it with depth, privacy and feelings held rather than shown.',
    },
    hi: {
      name: 'वृश्चिक राशि में चंद्रमा',
      short: 'परंपरा में इसे उन बातों का दिन माना जाता है जो कही नहीं जातीं।',
      long: 'शास्त्र चंद्रमा को यहाँ नीच राशि में रखते हैं और इस स्थिति को तीव्र मानते हैं — परंपरा इसे गहराई, एकांत और प्रकट न की गई भावनाओं से जोड़ती है।',
    },
  },
  Sagittarius: {
    en: {
      name: 'Moon in Sagittarius',
      short: 'Traditionally read as a day for plans and for the long view.',
      long: 'The Moon in Sagittarius is traditionally associated with optimism, travel and questions of principle. The tradition reads it as favouring the wide view over the near one.',
    },
    hi: {
      name: 'धनु राशि में चंद्रमा',
      short: 'परंपरा में इसे योजनाओं और दूरदृष्टि का दिन माना जाता है।',
      long: 'धनु का चंद्रमा परंपरा में आशावाद, यात्रा और सिद्धांत के प्रश्नों से जुड़ा है। परंपरा इसे निकट के बजाय व्यापक दृष्टि के अनुकूल पढ़ती है।',
    },
  },
  Capricorn: {
    en: {
      name: 'Moon in Capricorn',
      short: 'Traditionally read as a day for the work that has been waiting.',
      long: 'The Moon in Capricorn is traditionally associated with discipline, structure and a willingness to postpone comfort. The tradition reads it as favouring obligation over inclination.',
    },
    hi: {
      name: 'मकर राशि में चंद्रमा',
      short: 'परंपरा में इसे रुके हुए काम का दिन माना जाता है।',
      long: 'मकर का चंद्रमा परंपरा में अनुशासन, संरचना और सुख को टालने की इच्छा से जुड़ा है। परंपरा इसे रुचि के बजाय कर्तव्य के अनुकूल पढ़ती है।',
    },
  },
  Aquarius: {
    en: {
      name: 'Moon in Aquarius',
      short: 'Traditionally read as a day for groups and for unusual ideas.',
      long: 'The Moon in Aquarius is traditionally associated with detachment, collective concerns and thinking that steps outside the usual. The tradition reads it as favouring the general over the personal.',
    },
    hi: {
      name: 'कुंभ राशि में चंद्रमा',
      short: 'परंपरा में इसे समूहों और असामान्य विचारों का दिन माना जाता है।',
      long: 'कुंभ का चंद्रमा परंपरा में निर्लिप्तता, सामूहिक सरोकारों और सामान्य से हटकर सोचने से जुड़ा है। परंपरा इसे व्यक्तिगत के बजाय सामान्य के अनुकूल पढ़ती है।',
    },
  },
  Pisces: {
    en: {
      name: 'Moon in Pisces',
      short: 'Traditionally read as a day for rest and for imagination.',
      long: 'The Moon in Pisces is traditionally associated with sensitivity, imagination and a blurred sense of boundaries. The tradition reads it as favouring reflection over decision.',
    },
    hi: {
      name: 'मीन राशि में चंद्रमा',
      short: 'परंपरा में इसे विश्राम और कल्पना का दिन माना जाता है।',
      long: 'मीन का चंद्रमा परंपरा में संवेदनशीलता, कल्पना और सीमाओं के धुंधले बोध से जुड़ा है। परंपरा इसे निर्णय के बजाय चिंतन के अनुकूल पढ़ती है।',
    },
  },
}

export const MOON_SIGNS = Object.keys(MOON_DAYS) as MoonSign[]

/** The day's note for a Moon sign, or null for a name we do not have. */
export function moonDay(sign: string, locale: 'en' | 'hi') {
  const entry = (MOON_DAYS as Record<string, Record<'en' | 'hi', unknown>>)[sign]
  if (!entry) return null
  return (entry[locale] as (typeof MOON_DAYS)[MoonSign]['en'] | undefined) ?? null
}
