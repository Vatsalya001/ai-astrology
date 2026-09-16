import type { Localised } from './types'

/**
 * Every jargon term the UI can show, defined in plain language.
 *
 * The gate requires that every astrology term appearing in the interface
 * resolves to a definition here — the product's stated goal is that "a
 * user sees their Kundli and understands it without knowing astrology",
 * and an undefined term on screen is the exact failure of that goal.
 *
 * Definitions describe what the tradition says a thing IS, never what it
 * will do to the reader. See `safety.test.ts`.
 */

export type GlossaryKey =
  // The chart itself
  | 'kundli' | 'rasi' | 'bhava' | 'graha' | 'lagna' | 'ascendant'
  | 'navamsa' | 'dasamsa' | 'varga' | 'chart_style_north' | 'chart_style_south'
  // Positions
  | 'longitude' | 'degree' | 'sign' | 'house' | 'house_lord'
  | 'nakshatra' | 'pada' | 'ayanamsa' | 'sidereal' | 'tropical'
  // Planetary states
  | 'retrograde' | 'combust' | 'exalted' | 'debilitated' | 'own_sign'
  | 'moolatrikona' | 'dignity' | 'conjunction' | 'aspect' | 'drishti'
  // The nodes
  | 'rahu' | 'ketu' | 'lunar_node'
  // Time
  | 'dasha' | 'mahadasha' | 'antardasha' | 'pratyantardasha' | 'vimshottari'
  | 'gochara' | 'transit' | 'sade_sati' | 'sade_sati_rising'
  | 'sade_sati_peak' | 'sade_sati_setting'
  // Combinations
  | 'yoga' | 'kendra' | 'trikona' | 'dusthana' | 'panch_mahapurusha'
  | 'raja_yoga' | 'neecha_bhanga'
  // House groupings
  | 'lagna_bhava' | 'dhana_bhava' | 'sukha_bhava' | 'putra_bhava'
  | 'ripu_bhava' | 'kalatra_bhava' | 'ayur_bhava' | 'bhagya_bhava'
  | 'karma_bhava' | 'labha_bhava' | 'vyaya_bhava'
  // Practical
  | 'birth_time_accuracy' | 'whole_sign_houses' | 'ephemeris'

export const GLOSSARY: Localised<GlossaryKey> = {
  kundli: {
    en: {
      name: 'Kundli',
      short: 'Your birth chart.',
      long: 'A diagram of where the planets stood at the moment and place you were born. Everything else in this app is read from it.',
    },
    hi: {
      name: 'कुंडली',
      short: 'आपका जन्म चार्ट।',
      long: 'आपके जन्म के समय और स्थान पर ग्रह कहाँ थे, इसका चित्र। इस ऐप में बाकी सब कुछ इसी से पढ़ा जाता है।',
    },
  },
  rasi: {
    en: {
      name: 'Rasi',
      short: 'A zodiac sign, and the name for the main chart.',
      long: 'One of the twelve 30-degree divisions of the zodiac. "Rasi chart" also means the main birth chart, the D1, as distinct from the divisional charts.',
    },
    hi: {
      name: 'राशि',
      short: 'राशिचक्र का एक भाग, और मुख्य चार्ट का नाम।',
      long: 'राशिचक्र के बारह 30-अंश भागों में से एक। "राशि चार्ट" का अर्थ मुख्य जन्म चार्ट (D1) भी है, जो वर्ग चार्टों से अलग है।',
    },
  },
  bhava: {
    en: {
      name: 'Bhava',
      short: 'A house — one of twelve areas of life.',
      long: 'The twelve houses divide the chart into areas of life such as self, wealth, family and work. Which sign falls in which house depends on your rising sign.',
    },
    hi: {
      name: 'भाव',
      short: 'एक घर — जीवन के बारह क्षेत्रों में से एक।',
      long: 'बारह भाव चार्ट को जीवन के क्षेत्रों में बाँटते हैं, जैसे स्वयं, धन, परिवार और कार्य। कौन सी राशि किस भाव में आती है, यह आपकी लग्न राशि पर निर्भर करता है।',
    },
  },
  graha: {
    en: {
      name: 'Graha',
      short: 'A planet, in the Vedic sense.',
      long: 'The nine grahas are the Sun, Moon, Mars, Mercury, Jupiter, Venus, Saturn, Rahu and Ketu. The last two are points, not bodies.',
    },
    hi: {
      name: 'ग्रह',
      short: 'वैदिक अर्थ में ग्रह।',
      long: 'नौ ग्रह हैं — सूर्य, चंद्र, मंगल, बुध, गुरु, शुक्र, शनि, राहु और केतु। अंतिम दो बिंदु हैं, पिंड नहीं।',
    },
  },
  lagna: {
    en: {
      name: 'Lagna',
      short: 'The rising sign. Also called the ascendant.',
      long: 'The zodiac sign rising on the eastern horizon at your birth. It changes roughly every two hours, which is why an accurate birth time matters so much.',
    },
    hi: {
      name: 'लग्न',
      short: 'उदय होती राशि। इसे लग्न या उदय लग्न कहते हैं।',
      long: 'आपके जन्म के समय पूर्वी क्षितिज पर उदय होती राशि। यह लगभग हर दो घंटे में बदलती है, इसीलिए सही जन्म समय इतना महत्वपूर्ण है।',
    },
  },
  ascendant: {
    en: {
      name: 'Ascendant',
      short: 'The English name for the lagna.',
      long: 'The rising sign, and the start of the first house. Without an exact birth time it cannot be determined, so charts without one show no ascendant rather than a guess.',
    },
    hi: {
      name: 'उदय लग्न',
      short: 'लग्न का अंग्रेज़ी नाम।',
      long: 'उदय होती राशि, और पहले भाव की शुरुआत। सही जन्म समय के बिना यह तय नहीं हो सकती, इसलिए समय न होने पर चार्ट अनुमान के बजाय कोई लग्न नहीं दिखाता।',
    },
  },
  navamsa: {
    en: {
      name: 'Navamsa (D9)',
      short: 'A chart made by dividing each sign into nine.',
      long: 'The most used divisional chart. It is traditionally consulted alongside the main chart for questions of partnership and inner strength.',
    },
    hi: {
      name: 'नवांश (D9)',
      short: 'प्रत्येक राशि को नौ भागों में बाँटकर बना चार्ट।',
      long: 'सबसे अधिक उपयोग किया जाने वाला वर्ग चार्ट। परंपरा में इसे साझेदारी और आंतरिक बल के प्रश्नों पर मुख्य चार्ट के साथ देखा जाता है।',
    },
  },
  dasamsa: {
    en: {
      name: 'Dasamsa (D10)',
      short: 'A chart made by dividing each sign into ten.',
      long: 'Traditionally consulted for questions of work and profession. Like the navamsa it is derived from the main chart, not observed separately.',
    },
    hi: {
      name: 'दशांश (D10)',
      short: 'प्रत्येक राशि को दस भागों में बाँटकर बना चार्ट।',
      long: 'परंपरा में कार्य और व्यवसाय के प्रश्नों पर देखा जाता है। नवांश की तरह यह मुख्य चार्ट से निकाला जाता है, अलग से देखा नहीं जाता।',
    },
  },
  varga: {
    en: {
      name: 'Varga',
      short: 'A divisional chart.',
      long: 'Any chart made by subdividing the signs of the main chart. The navamsa and dasamsa are the two this app computes.',
    },
    hi: {
      name: 'वर्ग',
      short: 'एक वर्ग (विभाजन) चार्ट।',
      long: 'मुख्य चार्ट की राशियों को उपविभाजित करके बना कोई भी चार्ट। यह ऐप नवांश और दशांश दो बनाता है।',
    },
  },
  chart_style_north: {
    en: {
      name: 'North Indian chart',
      short: 'A diamond layout where the houses stay put.',
      long: 'House 1 is always the top-centre diamond and the sign numbers move. Common across northern India.',
    },
    hi: {
      name: 'उत्तर भारतीय चार्ट',
      short: 'हीरे जैसी रचना जिसमें भाव स्थिर रहते हैं।',
      long: 'पहला भाव हमेशा ऊपर बीच का हीरा होता है और राशि संख्याएँ बदलती हैं। उत्तर भारत में प्रचलित।',
    },
  },
  chart_style_south: {
    en: {
      name: 'South Indian chart',
      short: 'A grid layout where the signs stay put.',
      long: 'Each sign always occupies the same cell, and the ascendant is marked with a diagonal. Common across southern India.',
    },
    hi: {
      name: 'दक्षिण भारतीय चार्ट',
      short: 'ग्रिड रचना जिसमें राशियाँ स्थिर रहती हैं।',
      long: 'हर राशि हमेशा एक ही खाने में रहती है, और लग्न को तिरछी रेखा से चिह्नित किया जाता है। दक्षिण भारत में प्रचलित।',
    },
  },
  longitude: {
    en: {
      name: 'Longitude',
      short: 'A planet’s position measured around the zodiac.',
      long: 'A number from 0 to 360 degrees. Every other placement in the chart — sign, house, nakshatra — is worked out from it.',
    },
    hi: {
      name: 'देशांतर',
      short: 'राशिचक्र में ग्रह की स्थिति।',
      long: '0 से 360 अंश के बीच की संख्या। चार्ट की बाकी सभी स्थितियाँ — राशि, भाव, नक्षत्र — इसी से निकाली जाती हैं।',
    },
  },
  degree: {
    en: {
      name: 'Degree',
      short: 'How far into its sign a planet sits.',
      long: 'A number from 0 to 30. A planet at 29 degrees is about to change sign, which some traditions read as a weak placement.',
    },
    hi: {
      name: 'अंश',
      short: 'ग्रह अपनी राशि में कितना आगे है।',
      long: '0 से 30 के बीच की संख्या। 29 अंश पर स्थित ग्रह राशि बदलने वाला है, जिसे कुछ परंपराएँ कमज़ोर स्थिति मानती हैं।',
    },
  },
  sign: {
    en: {
      name: 'Sign',
      short: 'One of the twelve, from Aries to Pisces.',
      long: 'A 30-degree slice of the zodiac. This app uses sidereal signs, which are measured against the stars rather than the seasons.',
    },
    hi: {
      name: 'राशि',
      short: 'मेष से मीन तक बारह में से एक।',
      long: 'राशिचक्र का 30-अंश भाग। यह ऐप निरयण राशियाँ उपयोग करता है, जो ऋतुओं के बजाय तारों के सापेक्ष मापी जाती हैं।',
    },
  },
  house: {
    en: {
      name: 'House',
      short: 'One of twelve areas of life. Same as bhava.',
      long: 'Counted from the rising sign. The first house is the rising sign itself, not the one after it.',
    },
    hi: {
      name: 'भाव',
      short: 'जीवन के बारह क्षेत्रों में से एक। भाव के समान।',
      long: 'लग्न से गिने जाते हैं। पहला भाव स्वयं लग्न राशि है, उसके बाद वाली नहीं।',
    },
  },
  house_lord: {
    en: {
      name: 'House lord',
      short: 'The planet that rules a house’s sign.',
      long: 'Each sign has a ruling planet, and that planet is said to be the lord of any house the sign falls in. Where the lord sits is read alongside the house itself.',
    },
    hi: {
      name: 'भावेश',
      short: 'भाव की राशि का स्वामी ग्रह।',
      long: 'हर राशि का एक स्वामी ग्रह होता है, और वह ग्रह उस भाव का स्वामी कहलाता है जिसमें राशि पड़ती है। स्वामी कहाँ बैठा है, यह भाव के साथ देखा जाता है।',
    },
  },
  nakshatra: {
    en: {
      name: 'Nakshatra',
      short: 'One of 27 lunar mansions.',
      long: 'The zodiac divided into 27 rather than 12. The Moon’s nakshatra at birth decides where your dasha sequence begins.',
    },
    hi: {
      name: 'नक्षत्र',
      short: '27 चंद्र भवनों में से एक।',
      long: 'राशिचक्र को 12 के बजाय 27 भागों में बाँटना। जन्म के समय चंद्रमा का नक्षत्र तय करता है कि आपकी दशा कहाँ से शुरू होती है।',
    },
  },
  pada: {
    en: {
      name: 'Pada',
      short: 'A quarter of a nakshatra.',
      long: 'Each nakshatra divides into four padas of 3 degrees 20 minutes. Together the 108 padas map exactly onto the navamsa chart.',
    },
    hi: {
      name: 'पाद',
      short: 'नक्षत्र का एक चौथाई भाग।',
      long: 'हर नक्षत्र चार पादों में बँटता है, प्रत्येक 3 अंश 20 कला का। कुल 108 पाद ठीक नवांश चार्ट पर मिलते हैं।',
    },
  },
  ayanamsa: {
    en: {
      name: 'Ayanamsa',
      short: 'The gap between the sidereal and tropical zodiacs.',
      long: 'Currently about 24 degrees and growing slowly. This app uses the Lahiri value, the Indian standard.',
    },
    hi: {
      name: 'अयनांश',
      short: 'निरयण और सायन राशिचक्र के बीच का अंतर।',
      long: 'इस समय लगभग 24 अंश, और धीरे-धीरे बढ़ रहा है। यह ऐप लाहिरी मान उपयोग करता है, जो भारतीय मानक है।',
    },
  },
  sidereal: {
    en: {
      name: 'Sidereal',
      short: 'Measured against the fixed stars.',
      long: 'The system Indian astrology uses. It differs from the tropical zodiac used in Western astrology by the ayanamsa, currently about 24 degrees.',
    },
    hi: {
      name: 'निरयण',
      short: 'स्थिर तारों के सापेक्ष मापा गया।',
      long: 'भारतीय ज्योतिष की पद्धति। पाश्चात्य ज्योतिष के सायन राशिचक्र से यह अयनांश जितना भिन्न है, इस समय लगभग 24 अंश।',
    },
  },
  tropical: {
    en: {
      name: 'Tropical',
      short: 'Measured against the seasons.',
      long: 'The system Western astrology uses, anchored to the spring equinox. This app does not use it, but the engine computes it before subtracting the ayanamsa.',
    },
    hi: {
      name: 'सायन',
      short: 'ऋतुओं के सापेक्ष मापा गया।',
      long: 'पाश्चात्य ज्योतिष की पद्धति, जो वसंत विषुव पर आधारित है। यह ऐप इसका उपयोग नहीं करता, पर गणना में अयनांश घटाने से पहले यही निकाला जाता है।',
    },
  },
  retrograde: {
    en: {
      name: 'Retrograde',
      short: 'A planet appearing to move backwards.',
      long: 'An optical effect of the Earth and the planet moving at different speeds. Marked with the sign ℞. The Sun and Moon are never retrograde; Rahu and Ketu always are.',
    },
    hi: {
      name: 'वक्री',
      short: 'ग्रह का पीछे की ओर चलता प्रतीत होना।',
      long: 'पृथ्वी और ग्रह की अलग-अलग गति से बनने वाला दृष्टि-प्रभाव। ℞ चिह्न से दर्शाया जाता है। सूर्य और चंद्र कभी वक्री नहीं होते; राहु और केतु सदा वक्री रहते हैं।',
    },
  },
  combust: {
    en: {
      name: 'Combust',
      short: 'A planet too close to the Sun to be seen.',
      long: 'Within a traditional distance of the Sun, a planet is said to be combust and its significations weakened. Shown with a ring around the glyph.',
    },
    hi: {
      name: 'अस्त',
      short: 'सूर्य के इतने निकट कि दिखाई न दे।',
      long: 'सूर्य से परंपरागत दूरी के भीतर आने पर ग्रह अस्त कहलाता है और उसके फल क्षीण माने जाते हैं। चिह्न के चारों ओर वृत्त से दर्शाया जाता है।',
    },
  },
  exalted: {
    en: {
      name: 'Exalted',
      short: 'A planet in its strongest sign.',
      long: 'Each planet has one sign where tradition places it at its strongest, and the opposite sign where it is weakest.',
    },
    hi: {
      name: 'उच्च',
      short: 'अपनी सबसे बलवान राशि में ग्रह।',
      long: 'हर ग्रह की एक राशि होती है जहाँ परंपरा उसे सबसे बलवान मानती है, और सामने वाली राशि जहाँ सबसे कमज़ोर।',
    },
  },
  debilitated: {
    en: {
      name: 'Debilitated',
      short: 'A planet in its weakest sign.',
      long: 'The sign opposite its exaltation. Traditionally read as a placement needing support from elsewhere in the chart, not as a verdict.',
    },
    hi: {
      name: 'नीच',
      short: 'अपनी सबसे कमज़ोर राशि में ग्रह।',
      long: 'उच्च राशि के सामने की राशि। परंपरा में इसे चार्ट के अन्य भागों से सहारे की आवश्यकता वाली स्थिति माना जाता है, कोई निर्णय नहीं।',
    },
  },
  own_sign: {
    en: {
      name: 'Own sign',
      short: 'A planet in a sign it rules.',
      long: 'Traditionally a comfortable, steady placement — the planet is said to be at home.',
    },
    hi: {
      name: 'स्वराशि',
      short: 'अपनी स्वामित्व वाली राशि में ग्रह।',
      long: 'परंपरा में सहज और स्थिर स्थिति — ग्रह अपने घर में माना जाता है।',
    },
  },
  moolatrikona: {
    en: {
      name: 'Moolatrikona',
      short: 'A particularly strong part of a planet’s own sign.',
      long: 'A specific degree range within a planet’s own sign, traditionally read as stronger than the rest of it.',
    },
    hi: {
      name: 'मूलत्रिकोण',
      short: 'ग्रह की स्वराशि का विशेष बलवान भाग।',
      long: 'स्वराशि के भीतर अंशों की एक विशेष सीमा, जिसे परंपरा में शेष राशि से अधिक बलवान माना जाता है।',
    },
  },
  dignity: {
    en: {
      name: 'Dignity',
      short: 'How comfortable a planet is where it sits.',
      long: 'A summary of exalted, own sign, moolatrikona, debilitated or neutral. It describes the placement, not the person.',
    },
    hi: {
      name: 'बल-स्थिति',
      short: 'ग्रह अपनी स्थिति में कितना सहज है।',
      long: 'उच्च, स्वराशि, मूलत्रिकोण, नीच या सम का सार। यह स्थिति का वर्णन है, व्यक्ति का नहीं।',
    },
  },
  conjunction: {
    en: {
      name: 'Conjunction',
      short: 'Two or more planets in the same sign.',
      long: 'Traditionally read as the planets blending their significations. Several conjunctions form named yogas.',
    },
    hi: {
      name: 'युति',
      short: 'एक ही राशि में दो या अधिक ग्रह।',
      long: 'परंपरा में इसे ग्रहों के फलों के मिलने के रूप में पढ़ा जाता है। कई युतियाँ नामित योग बनाती हैं।',
    },
  },
  aspect: {
    en: {
      name: 'Aspect',
      short: 'A planet influencing another position.',
      long: 'Every graha aspects the seventh house from itself. Mars, Jupiter and Saturn have additional aspects of their own.',
    },
    hi: {
      name: 'दृष्टि',
      short: 'एक ग्रह का दूसरी स्थिति पर प्रभाव।',
      long: 'हर ग्रह अपने से सातवें भाव को देखता है। मंगल, गुरु और शनि की अपनी अतिरिक्त दृष्टियाँ भी हैं।',
    },
  },
  drishti: {
    en: {
      name: 'Drishti',
      short: 'The Sanskrit word for aspect.',
      long: 'Literally "sight". Graha drishti is a planet’s influence on another house, counted forward from where it sits.',
    },
    hi: {
      name: 'दृष्टि',
      short: 'दृष्टि का संस्कृत शब्द।',
      long: 'शाब्दिक अर्थ "देखना"। ग्रह दृष्टि किसी ग्रह का दूसरे भाव पर प्रभाव है, जो उसकी स्थिति से आगे गिना जाता है।',
    },
  },
  rahu: {
    en: {
      name: 'Rahu',
      short: 'The north lunar node.',
      long: 'Not a body but a point where the Moon’s path crosses the Sun’s. Always moves backwards through the zodiac.',
    },
    hi: {
      name: 'राहु',
      short: 'उत्तर चंद्र पात।',
      long: 'कोई पिंड नहीं, बल्कि वह बिंदु जहाँ चंद्रमा का मार्ग सूर्य के मार्ग को काटता है। सदा राशिचक्र में पीछे की ओर चलता है।',
    },
  },
  ketu: {
    en: {
      name: 'Ketu',
      short: 'The south lunar node, exactly opposite Rahu.',
      long: 'The other crossing point, always 180 degrees from Rahu. If the two are ever not opposite, the chart is wrong.',
    },
    hi: {
      name: 'केतु',
      short: 'दक्षिण चंद्र पात, राहु के ठीक सामने।',
      long: 'दूसरा कटान बिंदु, सदा राहु से 180 अंश पर। यदि दोनों कभी आमने-सामने न हों, तो चार्ट ग़लत है।',
    },
  },
  lunar_node: {
    en: {
      name: 'Lunar node',
      short: 'Where the Moon’s path crosses the Sun’s.',
      long: 'There are two, Rahu and Ketu, always opposite each other. Eclipses happen when a new or full Moon falls near one.',
    },
    hi: {
      name: 'चंद्र पात',
      short: 'जहाँ चंद्रमा का मार्ग सूर्य के मार्ग को काटता है।',
      long: 'दो हैं, राहु और केतु, सदा आमने-सामने। ग्रहण तब होते हैं जब अमावस्या या पूर्णिमा इनके पास पड़े।',
    },
  },
  dasha: {
    en: {
      name: 'Dasha',
      short: 'A planetary period.',
      long: 'A stretch of time ruled by one planet. Your sequence and its starting point come from the Moon’s nakshatra at birth.',
    },
    hi: {
      name: 'दशा',
      short: 'ग्रह की अवधि।',
      long: 'एक ग्रह के अधिकार वाला समय। आपका क्रम और उसका आरंभ बिंदु जन्म के समय चंद्रमा के नक्षत्र से आता है।',
    },
  },
  mahadasha: {
    en: {
      name: 'Mahadasha',
      short: 'The major planetary period.',
      long: 'The outermost level, lasting from 6 to 20 years depending on the planet. The nine together span 120 years.',
    },
    hi: {
      name: 'महादशा',
      short: 'मुख्य ग्रह अवधि।',
      long: 'सबसे बाहरी स्तर, ग्रह के अनुसार 6 से 20 वर्ष तक। नौ मिलकर 120 वर्ष बनाते हैं।',
    },
  },
  antardasha: {
    en: {
      name: 'Antardasha',
      short: 'A sub-period inside a mahadasha.',
      long: 'Each mahadasha divides into nine antardashas in the same planetary order, proportional to each planet’s share.',
    },
    hi: {
      name: 'अंतर्दशा',
      short: 'महादशा के भीतर की उप-अवधि।',
      long: 'हर महादशा उसी ग्रह-क्रम में नौ अंतर्दशाओं में बँटती है, प्रत्येक ग्रह के अंश के अनुपात में।',
    },
  },
  pratyantardasha: {
    en: {
      name: 'Pratyantardasha',
      short: 'A sub-period inside an antardasha.',
      long: 'The third level, dividing each antardasha again the same way. This app computes all three.',
    },
    hi: {
      name: 'प्रत्यंतर्दशा',
      short: 'अंतर्दशा के भीतर की उप-अवधि।',
      long: 'तीसरा स्तर, जो हर अंतर्दशा को उसी प्रकार फिर बाँटता है। यह ऐप तीनों बनाता है।',
    },
  },
  vimshottari: {
    en: {
      name: 'Vimshottari',
      short: 'The 120-year dasha system this app uses.',
      long: 'The most widely used dasha system in Indian astrology. The name means "one hundred and twenty".',
    },
    hi: {
      name: 'विंशोत्तरी',
      short: 'इस ऐप की 120-वर्षीय दशा पद्धति।',
      long: 'भारतीय ज्योतिष में सबसे प्रचलित दशा पद्धति। नाम का अर्थ है "एक सौ बीस"।',
    },
  },
  gochara: {
    en: {
      name: 'Gochara',
      short: 'Transits — where the planets are now.',
      long: 'The current sky read against your birth chart, traditionally counted from your natal Moon rather than your ascendant.',
    },
    hi: {
      name: 'गोचर',
      short: 'गोचर — ग्रह इस समय कहाँ हैं।',
      long: 'वर्तमान आकाश को आपके जन्म चार्ट के सापेक्ष पढ़ना, जो परंपरा में लग्न के बजाय जन्म के चंद्रमा से गिना जाता है।',
    },
  },
  transit: {
    en: {
      name: 'Transit',
      short: 'A planet’s current position. Same as gochara.',
      long: 'Where a planet is today, as opposed to where it was at your birth. Transits are the same for everyone; only the house they fall in differs.',
    },
    hi: {
      name: 'गोचर',
      short: 'ग्रह की वर्तमान स्थिति। गोचर के समान।',
      long: 'ग्रह आज कहाँ है, बनाम जन्म के समय कहाँ था। गोचर सबके लिए एक ही होता है; केवल वह किस भाव में पड़ता है, यह भिन्न होता है।',
    },
  },
  sade_sati: {
    en: {
      name: 'Sade Sati',
      short: 'Saturn’s seven-and-a-half-year passage around your Moon.',
      long: 'Saturn transiting the 12th, 1st and 2nd signs from your natal Moon, about two and a half years in each. Traditionally read as a demanding period calling for patience.',
    },
    hi: {
      name: 'साढ़े साती',
      short: 'आपके चंद्रमा के आसपास शनि का साढ़े सात वर्ष का भ्रमण।',
      long: 'जन्म के चंद्रमा से बारहवीं, पहली और दूसरी राशि में शनि का गोचर, प्रत्येक में लगभग ढाई वर्ष। परंपरा में इसे धैर्य माँगने वाला कठिन समय माना जाता है।',
    },
  },
  sade_sati_rising: {
    en: {
      name: 'Rising phase',
      short: 'Saturn in the 12th from your Moon.',
      long: 'The first of the three phases. Traditionally associated with endings and with letting go of what no longer holds.',
    },
    hi: {
      name: 'आरोहण चरण',
      short: 'आपके चंद्रमा से बारहवें भाव में शनि।',
      long: 'तीन चरणों में पहला। परंपरा में इसे समाप्ति और जो टिक नहीं रहा उसे छोड़ने से जोड़ा जाता है।',
    },
  },
  sade_sati_peak: {
    en: {
      name: 'Peak phase',
      short: 'Saturn over your Moon itself.',
      long: 'The middle phase, traditionally considered the most demanding of the three.',
    },
    hi: {
      name: 'शिखर चरण',
      short: 'स्वयं आपके चंद्रमा पर शनि।',
      long: 'मध्य चरण, जिसे परंपरा में तीनों में सबसे कठिन माना जाता है।',
    },
  },
  sade_sati_setting: {
    en: {
      name: 'Setting phase',
      short: 'Saturn in the 2nd from your Moon.',
      long: 'The last of the three, traditionally associated with consolidating what the earlier phases reshaped.',
    },
    hi: {
      name: 'अवरोहण चरण',
      short: 'आपके चंद्रमा से दूसरे भाव में शनि।',
      long: 'तीनों में अंतिम, जिसे परंपरा में पिछले चरणों द्वारा बदले गए को स्थिर करने से जोड़ा जाता है।',
    },
  },
  yoga: {
    en: {
      name: 'Yoga',
      short: 'A named combination of planets.',
      long: 'A recognised pattern — a conjunction, an aspect, a placement — that tradition gives a name and a reading to.',
    },
    hi: {
      name: 'योग',
      short: 'ग्रहों का नामित संयोग।',
      long: 'एक पहचाना गया प्रारूप — युति, दृष्टि या स्थिति — जिसे परंपरा नाम और फल देती है।',
    },
  },
  kendra: {
    en: {
      name: 'Kendra',
      short: 'The angular houses: 1, 4, 7 and 10.',
      long: 'Traditionally the strongest houses. A planet in a kendra is said to act more visibly.',
    },
    hi: {
      name: 'केंद्र',
      short: 'केंद्र भाव: 1, 4, 7 और 10।',
      long: 'परंपरा में सबसे बलवान भाव। केंद्र में स्थित ग्रह का प्रभाव अधिक प्रकट माना जाता है।',
    },
  },
  trikona: {
    en: {
      name: 'Trikona',
      short: 'The trine houses: 1, 5 and 9.',
      long: 'Traditionally the most favourable houses, associated with merit and support.',
    },
    hi: {
      name: 'त्रिकोण',
      short: 'त्रिकोण भाव: 1, 5 और 9।',
      long: 'परंपरा में सबसे शुभ भाव, जो पुण्य और सहारे से जुड़े हैं।',
    },
  },
  dusthana: {
    en: {
      name: 'Dusthana',
      short: 'The difficult houses: 6, 8 and 12.',
      long: 'Traditionally associated with obstacles, change and loss. Many readings treat them as areas needing effort rather than as verdicts.',
    },
    hi: {
      name: 'दुःस्थान',
      short: 'कठिन भाव: 6, 8 और 12।',
      long: 'परंपरा में बाधा, परिवर्तन और हानि से जुड़े। कई पाठ इन्हें निर्णय के बजाय प्रयास माँगने वाले क्षेत्र मानते हैं।',
    },
  },
  panch_mahapurusha: {
    en: {
      name: 'Panch Mahapurusha',
      short: 'Five "great person" yogas.',
      long: 'Formed when Mars, Mercury, Jupiter, Venus or Saturn sits in its own or exaltation sign, in an angular house. Each has its own name.',
    },
    hi: {
      name: 'पंच महापुरुष',
      short: 'पाँच "महापुरुष" योग।',
      long: 'तब बनते हैं जब मंगल, बुध, गुरु, शुक्र या शनि अपनी स्वराशि या उच्च राशि में, केंद्र भाव में हो। हर एक का अपना नाम है।',
    },
  },
  raja_yoga: {
    en: {
      name: 'Raja Yoga',
      short: 'A lord of a kendra joined with a lord of a trikona.',
      long: 'Traditionally associated with rising standing and opportunity. A chart may contain several.',
    },
    hi: {
      name: 'राज योग',
      short: 'केंद्र के स्वामी का त्रिकोण के स्वामी से संबंध।',
      long: 'परंपरा में बढ़ती प्रतिष्ठा और अवसर से जुड़ा। एक चार्ट में कई हो सकते हैं।',
    },
  },
  neecha_bhanga: {
    en: {
      name: 'Neecha Bhanga',
      short: 'A cancelled debilitation.',
      long: 'When a debilitated planet meets specific conditions, tradition holds that the weakness is undone. Worth knowing, because a debilitation on its own is not the whole reading.',
    },
    hi: {
      name: 'नीच भंग',
      short: 'नीचता का रद्द होना।',
      long: 'जब नीच ग्रह विशेष शर्तें पूरी करता है, तो परंपरा मानती है कि कमज़ोरी समाप्त हो जाती है। जानना उपयोगी है, क्योंकि अकेली नीचता पूरा फल नहीं है।',
    },
  },
  lagna_bhava: {
    en: {
      name: 'First house',
      short: 'Self, body, how you come across.',
      long: 'The rising sign itself. Traditionally read for constitution and outward manner.',
    },
    hi: {
      name: 'प्रथम भाव',
      short: 'स्वयं, शरीर, आपकी छवि।',
      long: 'स्वयं लग्न राशि। परंपरा में शरीर-प्रकृति और बाहरी व्यवहार के लिए देखा जाता है।',
    },
  },
  dhana_bhava: {
    en: {
      name: 'Second house',
      short: 'Resources, family, speech.',
      long: 'Traditionally read for accumulated wealth, the family one is born into, and the voice.',
    },
    hi: {
      name: 'द्वितीय भाव',
      short: 'साधन, कुटुंब, वाणी।',
      long: 'परंपरा में संचित धन, जन्म के कुल और वाणी के लिए देखा जाता है।',
    },
  },
  sukha_bhava: {
    en: {
      name: 'Fourth house',
      short: 'Home, mother, comfort.',
      long: 'Traditionally read for the home, early life and peace of mind.',
    },
    hi: {
      name: 'चतुर्थ भाव',
      short: 'घर, माता, सुख।',
      long: 'परंपरा में घर, आरंभिक जीवन और मन की शांति के लिए देखा जाता है।',
    },
  },
  putra_bhava: {
    en: {
      name: 'Fifth house',
      short: 'Creativity, learning, children.',
      long: 'Traditionally read for intelligence, creative work and progeny.',
    },
    hi: {
      name: 'पंचम भाव',
      short: 'सृजन, विद्या, संतान।',
      long: 'परंपरा में बुद्धि, सृजनात्मक कार्य और संतान के लिए देखा जाता है।',
    },
  },
  ripu_bhava: {
    en: {
      name: 'Sixth house',
      short: 'Obstacles, service, health.',
      long: 'Traditionally read for competition, daily work and the body’s resilience.',
    },
    hi: {
      name: 'षष्ठ भाव',
      short: 'बाधा, सेवा, स्वास्थ्य।',
      long: 'परंपरा में प्रतिस्पर्धा, दैनिक कार्य और शरीर की सहनशक्ति के लिए देखा जाता है।',
    },
  },
  kalatra_bhava: {
    en: {
      name: 'Seventh house',
      short: 'Partnership and agreements.',
      long: 'Traditionally read for marriage, business partners and anything entered into jointly.',
    },
    hi: {
      name: 'सप्तम भाव',
      short: 'साझेदारी और अनुबंध।',
      long: 'परंपरा में विवाह, व्यापारिक साझेदार और मिलकर किए गए किसी भी कार्य के लिए देखा जाता है।',
    },
  },
  ayur_bhava: {
    en: {
      name: 'Eighth house',
      short: 'Change, the hidden, shared resources.',
      long: 'Traditionally read for transformation, inheritance and matters not on the surface.',
    },
    hi: {
      name: 'अष्टम भाव',
      short: 'परिवर्तन, गुप्त, साझा साधन।',
      long: 'परंपरा में रूपांतरण, उत्तराधिकार और सतह पर न दिखने वाली बातों के लिए देखा जाता है।',
    },
  },
  bhagya_bhava: {
    en: {
      name: 'Ninth house',
      short: 'Fortune, belief, teachers.',
      long: 'Traditionally read for higher learning, long journeys and guidance received.',
    },
    hi: {
      name: 'नवम भाव',
      short: 'भाग्य, आस्था, गुरु।',
      long: 'परंपरा में उच्च शिक्षा, लंबी यात्रा और प्राप्त मार्गदर्शन के लिए देखा जाता है।',
    },
  },
  karma_bhava: {
    en: {
      name: 'Tenth house',
      short: 'Work, standing, public life.',
      long: 'Traditionally read for profession and reputation. The dasamsa chart is consulted alongside it.',
    },
    hi: {
      name: 'दशम भाव',
      short: 'कार्य, प्रतिष्ठा, सार्वजनिक जीवन।',
      long: 'परंपरा में व्यवसाय और यश के लिए देखा जाता है। इसके साथ दशांश चार्ट भी देखा जाता है।',
    },
  },
  labha_bhava: {
    en: {
      name: 'Eleventh house',
      short: 'Gains, networks, elder siblings.',
      long: 'Traditionally read for income, friendships and things that come through others.',
    },
    hi: {
      name: 'एकादश भाव',
      short: 'लाभ, संपर्क, बड़े भाई-बहन।',
      long: 'परंपरा में आय, मित्रता और दूसरों के माध्यम से आने वाली बातों के लिए देखा जाता है।',
    },
  },
  vyaya_bhava: {
    en: {
      name: 'Twelfth house',
      short: 'Release, expenditure, retreat.',
      long: 'Traditionally read for what is spent or let go, foreign places, and time spent apart.',
    },
    hi: {
      name: 'द्वादश भाव',
      short: 'त्याग, व्यय, एकांत।',
      long: 'परंपरा में जो खर्च या छोड़ा जाता है, विदेश, और एकांत में बिताए समय के लिए देखा जाता है।',
    },
  },
  birth_time_accuracy: {
    en: {
      name: 'Birth time accuracy',
      short: 'How precisely your birth time is known.',
      long: 'The rising sign changes about every two hours, so even fifteen minutes can change your chart. Where the time is unknown, this app shows no ascendant rather than a guess.',
    },
    hi: {
      name: 'जन्म समय की शुद्धता',
      short: 'आपका जन्म समय कितना सही ज्ञात है।',
      long: 'लग्न लगभग हर दो घंटे में बदलता है, इसलिए पंद्रह मिनट भी चार्ट बदल सकते हैं। समय अज्ञात होने पर यह ऐप अनुमान के बजाय कोई लग्न नहीं दिखाता।',
    },
  },
  whole_sign_houses: {
    en: {
      name: 'Whole sign houses',
      short: 'Each house is exactly one sign.',
      long: 'The default in Indian astrology and in this app. The rising sign is the whole first house, the next sign the whole second, and so on.',
    },
    hi: {
      name: 'पूर्ण राशि भाव',
      short: 'हर भाव ठीक एक राशि है।',
      long: 'भारतीय ज्योतिष और इस ऐप की मानक पद्धति। लग्न राशि पूरा पहला भाव है, अगली राशि पूरा दूसरा, और इसी क्रम में।',
    },
  },
  ephemeris: {
    en: {
      name: 'Ephemeris',
      short: 'The table of planetary positions over time.',
      long: 'This app uses a published astronomical dataset from NASA’s Jet Propulsion Laboratory, so positions are computed rather than estimated.',
    },
    hi: {
      name: 'पंचांग-सारणी',
      short: 'समय के अनुसार ग्रह स्थितियों की सारणी।',
      long: 'यह ऐप नासा की जेट प्रोपल्शन लेबोरेटरी का प्रकाशित खगोलीय डेटा उपयोग करता है, इसलिए स्थितियाँ अनुमानित नहीं, गणना की हुई होती हैं।',
    },
  },
}

export const GLOSSARY_KEYS = Object.keys(GLOSSARY) as GlossaryKey[]
