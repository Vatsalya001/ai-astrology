package transits

import "fmt"

// SignNames is the zodiac in order, Aries first.
//
// A second copy of a list that already exists in astro-service's
// constants.py, and copies across a language boundary are how a rename
// becomes a silent wrong answer. This one matters more than most: the
// natal Moon sign arrives from a stored chart as a NAME, and the
// rotation needs an INDEX, so a single reordering here shifts every
// user's gochara by however far the list moved — which still renders,
// still looks plausible, and is wrong for everybody.
//
// TestSignNamesMatchAstroService reads constants.py and compares, so a
// rename on either side fails the build rather than the reading.
var SignNames = [SignCount]string{
	"Aries", "Taurus", "Gemini", "Cancer", "Leo", "Virgo",
	"Libra", "Scorpio", "Sagittarius", "Capricorn", "Aquarius", "Pisces",
}

// signIndexByName is built from SignNames, so the two cannot disagree.
var signIndexByName = func() map[string]int {
	index := make(map[string]int, SignCount)
	for i, name := range SignNames {
		index[name] = i
	}
	return index
}()

// SignIndex resolves a sign name to its index.
//
// Returns an error rather than a zero. Aries IS index zero, so a
// not-found sentinel of 0 would silently place every unrecognised sign
// at the start of the zodiac — and the caller would compute houses from
// a Moon it never found.
func SignIndex(name string) (int, error) {
	index, ok := signIndexByName[name]
	if !ok {
		return 0, fmt.Errorf("transits: %q is not a zodiac sign", name)
	}
	return index, nil
}
