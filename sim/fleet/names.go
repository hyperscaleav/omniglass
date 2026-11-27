package fleet

import (
	"fmt"
	"hash/fnv"
)

// NameGenerator produces unique title-cased "Adjective Surname"
// names (e.g. "Affectionate Bose"). Humanizing at the source means
// every downstream consumer (lab console, omniglass UI, sync-emitted
// fields) sees the same friendly form without doing its own
// transformation.
//
// Hashed-seed lookup picks a starting cell so names spread across
// the (adjective × surname) space rather than clumping in the first
// few buckets. Linear probing on collision preserves uniqueness. The scale
// lab's scenario plan (internal/lab/scenario) labels each planned system with
// it.
type NameGenerator struct {
	used  map[string]bool
	index int // empty-seed fallback counter
}

// NewNameGenerator is a generator with no name handed out yet.
func NewNameGenerator() *NameGenerator {
	return &NameGenerator{used: map[string]bool{}}
}

var displayAdjectives = []string{
	"admiring", "adoring", "affectionate", "agitated", "amazing",
	"angry", "awesome", "beautiful", "blissful", "bold",
	"boring", "brave", "busy", "charming", "clever",
	"compassionate", "competent", "condescending", "confident", "cool",
	"cranky", "crazy", "dazzling", "determined", "distracted",
	"dreamy", "eager", "ecstatic", "elastic", "elated",
	"elegant", "eloquent", "epic", "exciting", "fervent",
	"festive", "flamboyant", "focused", "friendly", "frosty",
	"funny", "gallant", "gifted", "goofy", "gracious",
	"great", "happy", "hardcore", "heuristic", "hopeful",
	"hungry", "infallible", "inspiring", "intelligent", "interesting",
	"jolly", "jovial", "keen", "kind", "laughing",
	"loving", "lucid", "magical", "modest", "musing",
	"mystifying", "naughty", "nervous", "nice", "nifty",
	"nostalgic", "objective", "optimistic", "peaceful", "pedantic",
	"pensive", "practical", "priceless", "quirky", "quizzical",
	"recursing", "relaxed", "reverent", "romantic", "sad",
	"serene", "sharp", "silly", "sleepy", "stoic",
	"strange", "stupefied", "suspicious", "sweet", "tender",
	"thirsty", "trusting", "unruffled", "upbeat", "vibrant",
	"vigilant", "vigorous", "wizardly", "wonderful", "xenodochial",
	"youthful", "zealous", "zen",
}

var displaySurnames = []string{
	"albattani", "allen", "almeida", "antonelli", "babbage",
	"banach", "banzai", "bardeen", "bartik", "bassi",
	"beaver", "bell", "benz", "bhabha", "bhaskara",
	"blackburn", "blackwell", "bohr", "booth", "borg",
	"bose", "bouman", "boyd", "brahmagupta", "brattain",
	"brown", "buck", "burnell", "cannon", "carson",
	"cartwright", "cerf", "chandrasekhar", "chaplygin", "chatelet",
	"chatterjee", "chaum", "chebyshev", "cohen", "colden",
	"cori", "cray", "curie", "darwin", "davinci",
	"dewdney", "dhawan", "diffie", "dijkstra", "dirac",
	"driscoll", "dubinsky", "easley", "edison", "einstein",
	"elbakyan", "elgamal", "elion", "ellis", "engelbart",
	"euclid", "euler", "faraday", "feistel", "fermat",
	"fermi", "feynman", "franklin", "gagarin", "galileo",
	"galois", "gandhi", "gates", "gauss", "germain",
	"goldberg", "goldstine", "goldwasser", "golick", "goodall",
	"gould", "greider", "grothendieck", "haibt", "hamilton",
	"haslett", "hawking", "heisenberg", "hellman", "hermann",
	"herschel", "hertz", "heyrovsky", "hodgkin", "hofstadter",
	"hoover", "hopper", "hugle", "hypatia", "ishizaka",
	"jackson", "jang", "jennings", "jepsen", "johnson",
	"joliot", "jones", "kalam", "kapitsa", "kare",
	"keldysh", "keller", "kepler", "khayyam", "khorana",
	"kilby", "kirch", "knuth", "kowalevski", "lalande",
	"lamarr", "lamport", "leakey", "leavitt", "lederberg",
}

// Next returns a Docker-flavored "Adjective Surname" for the given
// seed. Each system passes its identity (site + location path + room
// + system_name) so the picked pair is stable across reruns. The
// hash picks a starting (adjective, surname) cell; on collision we
// linear-probe forward, which keeps uniqueness while still spreading
// chosen pairs across the whole space.
//
// Empty seed falls back to a serial counter (preserves the original
// counter-based contract for any caller that doesn't have a stable
// identity to seed with).
func (g *NameGenerator) Next(seed string) string {
	total := len(displayAdjectives) * len(displaySurnames)
	if total == 0 {
		return ""
	}
	var start int
	if seed == "" {
		start = g.index
		g.index++
	} else {
		h := fnv.New64a()
		_, _ = h.Write([]byte(seed))
		start = int(h.Sum64() % uint64(total))
	}
	for offset := 0; offset < total; offset++ {
		idx := (start + offset) % total
		ai := idx / len(displaySurnames)
		si := idx % len(displaySurnames)
		name := titleWord(displayAdjectives[ai]) + " " + titleWord(displaySurnames[si])
		if !g.used[name] {
			g.used[name] = true
			return name
		}
	}
	// Exhausted (>10k unique pairs available, won't happen at lab
	// scale). Last-resort numeric suffix.
	return fmt.Sprintf("System %d", start)
}

// titleWord returns the input with its first ASCII letter upper-
// cased and the rest left as-is. The adjective/surname tables are
// already lowercase ASCII so this is a one-byte mutation; we don't
// reach for unicode.ToUpper / strings.Title because we know the
// alphabet upfront and the per-system call rate makes the simpler
// path worth keeping.
func titleWord(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}
	return s
}
