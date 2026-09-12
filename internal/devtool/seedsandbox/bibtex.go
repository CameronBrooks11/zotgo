package main

import (
	"fmt"
	"strings"
)

// The corpus is meant to look like a library somebody actually keeps.
//
// An earlier version numbered one title per type — "Scale-up of photobioreactors,
// part 1..15" — which was distinct enough for a test and wrong for everything
// else: it reads as the same entry repeated, and it exercises search, sorting and
// title rendering against fifteen near-identical strings, which is the shape those
// paths are least likely to meet in the wild. A person watching it import assumed
// something was broken, which was the useful signal.
//
// Titles and creators are composed from small banks indexed by entry number, so
// every entry differs, the set is reproducible, and two runs never disagree.
var (
	subjects = []string{
		"photobioreactor", "stirred-tank bioreactor", "airlift column", "membrane module",
		"continuous flow reactor", "single-use vessel", "microalgal culture", "perfusion system",
		"packed-bed column", "hollow-fibre contactor", "bubble column", "fed-batch process",
		"rocking-motion bioreactor",
	}
	aspects = []string{
		"scale-up", "oxygen transfer", "mixing time", "shear stress", "fouling behaviour",
		"thermal control", "residence time distribution", "biomass yield", "power draw",
		"gas holdup", "mass transfer", "contamination risk",
	}
	settings = []string{
		"at pilot scale", "under nutrient limitation", "in high-density culture",
		"during extended operation", "across three geometries", "with in-line monitoring",
		"under variable illumination",
	}

	// Names deliberately span scripts and diacritics as braced TeX escapes, which
	// is what a real .bib contains and what a hand-written fake would never think
	// to include. No backticks: these are raw string literals.
	surnames = []string{
		`M{\"u}ller`, `Fern{\'a}ndez`, `{\O}stergaard`, `Silva`, `Nakamura`, `Baranowski`,
		`Lee`, `Zhang`, `Doran`, `Brooks`, `Ib{\'a}{\~n}ez`, `Gr{\"o}nqvist`,
		`Ka{\v r}{\'a}sek`, `{\L}ukasiewicz`, `Aalto`,
	}
	forenames = []string{
		`Anna`, `Luc{\'i}a`, `Mette`, `Jo{\~a}o Pedro`, `Hiroshi`, `Krzysztof`,
		`Ji-won`, `Wei`, `Pauline M.`, `Cameron`, `Mar{\'i}a`, `Eero`,
		`Zden{\v e}k`, `Agnieszka`, `Ren{\'e}`,
	}
	publishers  = []string{"Academic Press", "Springer", "Elsevier", "CRC Press"}
	addresses   = []string{"Amsterdam", "Berlin", "Boca Raton", "London"}
	institutes  = []string{"Department of Mechanical Engineering", "National Renewable Energy Laboratory", "Institute for Bioprocess Engineering"}
	schools     = []string{`University of S{\~a}o Paulo`, "KTH Royal Institute of Technology", `Universit{\'e} de Lyon`}
	venues      = []string{"Western University", "NREL", "TU Delft"}
	publishedAs = []string{"Trade press", "Preprint", "Technical blog"}
)

// entryShape is one recurring row of the corpus. The no-identifier tail —
// patents, conference papers, theses, reports — is over-represented on purpose:
// it is the half no identifier lookup can reach, and the half most likely to
// expose a field-mapping gap.
type entryShape struct {
	bibType string
	body    func(n int, title, creators string) string
}

var entryShapes = []entryShape{
	{"article", func(n int, title, creators string) string {
		return fmt.Sprintf("\n  author = {%s},\n  title = {%s},\n  journal = {Journal of Applied Phycology},\n  volume = {%d}, number = {%d}, pages = {%d--%d}, year = {%d},\n  doi = {10.1016/j.sandbox.%04d}",
			creators, title, 20+n%30, 1+n%12, 100+n, 100+n+11, 2014+n%11, n)
	}},
	{"book", func(n int, title, creators string) string {
		return fmt.Sprintf("\n  author = {%s},\n  title = {%s},\n  publisher = {%s}, address = {%s}, year = {%d},\n  isbn = {978-0-12-%06d-1}",
			creators, title, publishers[n%len(publishers)], addresses[n%len(addresses)], 2008+n%16, 200000+n)
	}},
	{"inproceedings", func(n int, title, creators string) string {
		return fmt.Sprintf("\n  author = {%s},\n  title = {%s},\n  booktitle = {Proceedings of the %d%s International Bioprocess Conference},\n  pages = {%d--%d}, year = {%d}",
			creators, title, 10+n%20, ordinal(10+n%20), 40+n, 40+n+7, 2015+n%10)
	}},
	{"patent", func(n int, title, creators string) string {
		return fmt.Sprintf("\n  author = {%s},\n  title = {%s},\n  number = {US%d B2}, year = {%d}, yearfiled = {%d}",
			creators, title, 10500000+n*137, 2016+n%9, 2014+n%9)
	}},
	{"techreport", func(n int, title, _ string) string {
		return fmt.Sprintf("\n  author = {{%s}},\n  title = {%s},\n  institution = {%s}, number = {TR-%d-%03d}, year = {%d}",
			institutes[n%len(institutes)], title, venues[n%len(venues)], 2012+n%13, n, 2012+n%13)
	}},
	{"mastersthesis", func(n int, title, creators string) string {
		return fmt.Sprintf("\n  author = {%s},\n  title = {%s},\n  school = {%s}, year = {%d}",
			creators, title, schools[n%len(schools)], 2013+n%12)
	}},
	{"incollection", func(n int, title, creators string) string {
		return fmt.Sprintf("\n  author = {%s},\n  title = {%s},\n  booktitle = {Handbook of Bioreactor Design}, editor = {Doran, Pauline M.},\n  publisher = {Springer}, pages = {%d--%d}, year = {%d}",
			creators, title, 200+n*3, 200+n*3+14, 2017+n%8)
	}},
	{"misc", func(n int, title, creators string) string {
		return fmt.Sprintf("\n  author = {%s},\n  title = {%s},\n  howpublished = {%s}, year = {%d},\n  note = {A note field, which Zotero turns into a child item the import response does not report}",
			creators, title, publishedAs[n%len(publishedAs)], 2019+n%7)
	}},
}

func ordinal(n int) string {
	switch {
	case n%100 >= 11 && n%100 <= 13:
		return "th"
	case n%10 == 1:
		return "st"
	case n%10 == 2:
		return "nd"
	case n%10 == 3:
		return "rd"
	default:
		return "th"
	}
}

// article picks a/an so the titles read as written rather than generated.
func article(word string) string {
	if word == "" {
		return "a"
	}
	switch word[0] {
	case 'a', 'e', 'i', 'o', 'u':
		return "an"
	}
	return "a"
}

// titleFor composes a distinct, plausible title for entry n.
//
// The bank lengths matter: aspects has 12 entries and subjects 13, so the pair
// repeats only every lcm(12,13)=156 — beyond the corpus. Equal lengths would
// cycle every 12, which is how an earlier version produced each title ten times
// over and looked, correctly, like something was wrong.
func titleFor(n int) string {
	aspect := aspects[(n*5)%len(aspects)]
	subject := subjects[(n*7)%len(subjects)]
	title := fmt.Sprintf("%s%s in %s %s", strings.ToUpper(aspect[:1]), aspect[1:], article(subject), subject)
	if n%3 == 0 {
		title += " " + settings[(n*11)%len(settings)]
	}
	return title
}

// creatorsFor composes one or two authors, so the corpus holds both the
// single-creator form and the multi-creator one — the latter being where Zotero's
// creatorSummary carries Unicode directional isolates.
func creatorsFor(n int) string {
	first := fmt.Sprintf("%s, %s", surnames[(n*3)%len(surnames)], forenames[(n*5)%len(forenames)])
	if n%4 == 0 {
		return first
	}
	second := fmt.Sprintf("%s, %s", surnames[(n*11)%len(surnames)], forenames[(n*13)%len(forenames)])
	if second == first {
		return first
	}
	return first + " and " + second
}

// generateBibTeX emits entries [start, end). Every entry carries the corpus tag,
// so "is this already seeded" is answered by asking the library rather than by
// remembering.
func generateBibTeX(start, end int) string {
	var out strings.Builder
	for i := start; i < end; i++ {
		n := i + 1
		shape := entryShapes[i%len(entryShapes)]
		fmt.Fprintf(&out, "@%s{sandbox%04d,%s,\n  keywords = {%s}\n}\n\n",
			shape.bibType, n, shape.body(n, titleFor(n), creatorsFor(n)), corpusMarker)
	}
	return out.String()
}
