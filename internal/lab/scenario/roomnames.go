package scenario

import (
	"fmt"
	"hash/fnv"
	"sort"
)

// Room names (#2757). A company names its rooms: a site picks a theme (a
// headquarters after scientists, an office after the peaks or rivers near it)
// and each room takes one word of it, and the rooms that are known by what they
// are (a huddle room, an auditorium) are named for it and numbered. The name is
// the room system's label and the address (the location, "Hq Tower 12 01") is
// where it is: a real building has both, and the console shows both.
//
// The word lists live here and nowhere else: a manifest names a theme per
// archetype (room_names) and a function per standard (room_name), and the plan
// resolves each room's name from them as a pure function of the scenario, the
// site and the room's place in it, so every run and every node gives the same
// names.

// roomThemes are the naming themes by name: no adjectives, one word or short
// proper name each, long enough that a site seldom runs a theme dry.
var roomThemes = map[string][]string{
	"scientists": {
		"Curie", "Hopper", "Turing", "Lovelace", "Franklin", "Noether", "Feynman", "Darwin", "Faraday", "Galileo",
		"Kepler", "Hawking", "Planck", "Bohr", "Fermi", "Dirac", "Euler", "Gauss", "Newton", "Tesla",
		"Pasteur", "Mendel", "Hubble", "Sagan", "Goodall", "Meitner", "Hamilton", "Johnson", "Vaughan", "Jackson",
		"Easley", "Mirzakhani", "Germain", "Kovalevskaya", "Ramanujan", "Raman", "Bose", "Chandrasekhar", "Hertz", "Maxwell",
		"Boltzmann", "Schrodinger", "Heisenberg", "Pauli", "Born", "Rutherford", "Thomson", "Joule", "Kelvin", "Ohm",
		"Volta", "Ampere", "Watt", "Edison", "Babbage", "Boole", "Shannon", "Knuth", "Dijkstra", "Liskov",
		"Lamport", "Cerf", "Kahn", "Backus", "Ritchie", "Kernighan", "Hollerith", "Zuse", "Wilkes", "Hamming",
		"Perlman", "Goldberg", "Yonath", "Hodgkin", "Elion", "Lederberg", "Salk", "Fleming", "Jenner", "Lister",
		"Harvey", "Linnaeus", "Lamarck", "Wallace", "Huxley", "Leakey", "Carson", "McClintock", "Cori", "Krebs",
		"Sanger", "Watson", "Crick", "Wilkins", "Mayr", "Haldane", "Fisher", "Pearson", "Bayes", "Laplace",
		"Lagrange", "Fourier", "Poincare", "Cauchy", "Riemann", "Hilbert", "Cantor", "Godel", "Euclid", "Archimedes",
		"Pythagoras", "Hypatia", "Fibonacci", "Descartes", "Fermat", "Pascal", "Leibniz", "Napier", "Brahe", "Copernicus",
		"Halley", "Herschel", "Rubin", "Payne", "Leavitt", "Cannon", "Burnell", "Lovell", "Oort", "Zwicky",
		"Gamow", "Alfven", "Bardeen", "Brattain", "Shockley", "Noyce", "Kilby", "Moore", "Wozniak", "Berners-Lee",
	},
	"peaks": {
		"Rainier", "Whitney", "Hood", "Shasta", "Baker", "Adams", "Olympus", "Elbert", "Massive", "Harvard",
		"Princeton", "Yale", "Evans", "Longs", "Wilson", "Sneffels", "Bierstadt", "Audubon", "Antero", "Blanca",
		"Humphreys", "Washington", "Marcy", "Katahdin", "Mitchell", "Denali", "Foraker", "Logan", "Robson", "Assiniboine",
		"Temple", "Lefroy", "Victoria", "Columbia", "Alberta", "Fairweather", "Saint Elias", "Cook", "Stuart", "Jefferson",
		"Borah", "Cloud Peak", "Gannett", "Granite", "Moran", "Owen", "Teewinot", "Wister", "Timpanogos", "Nebo",
		"Kings", "Pikes", "Sopris", "Wetterhorn", "Uncompahgre", "Eolus", "Windom", "Sunlight", "Handies", "Redcloud",
		"Lincoln", "Bross", "Democrat", "Quandary", "Grays", "Torreys", "Crestone", "Missouri", "Oxford", "Belford",
	},
	"rivers": {
		"Colorado", "Hudson", "Columbia", "Snake", "Yukon", "Platte", "Arkansas", "Brazos", "Pecos", "Gila",
		"Salmon", "Willamette", "Klamath", "Skagit", "Yakima", "Sacramento", "Truckee", "Humboldt", "Missouri", "Ohio",
		"Tennessee", "Cumberland", "Potomac", "Delaware", "Susquehanna", "Connecticut", "Merrimack", "Kennebec", "Penobscot", "Allegheny",
		"Monongahela", "Wabash", "Illinois", "Kansas", "Osage", "Niobrara", "Cheyenne", "Powder", "Bighorn", "Yellowstone",
		"Madison", "Gallatin", "Clark Fork", "Flathead", "Kootenai", "Spokane", "Deschutes", "Umpqua", "Rogue", "Feather",
		"Merced", "Tuolumne", "Kern", "Mojave", "Verde", "Rio Grande", "Canadian", "Sabine", "Neches", "Trinity",
		"Guadalupe", "Nueces", "Suwannee", "Savannah", "Santee", "Roanoke", "James", "Rappahannock", "Shenandoah", "Mohawk",
	},
	"parks": {
		"Yosemite", "Zion", "Arches", "Canyonlands", "Capitol Reef", "Bryce", "Glacier", "Yellowstone", "Grand Teton", "Rocky Mountain",
		"Great Sand Dunes", "Mesa Verde", "Black Canyon", "Badlands", "Wind Cave", "Voyageurs", "Isle Royale", "Acadia", "Shenandoah", "Great Smoky",
		"Mammoth Cave", "Congaree", "Biscayne", "Everglades", "Big Bend", "Carlsbad", "Saguaro", "Petrified Forest", "Joshua Tree", "Death Valley",
		"Pinnacles", "Sequoia", "Kings Canyon", "Lassen", "Redwood", "Crater Lake", "Olympic", "North Cascades", "Katmai", "Kenai Fjords",
		"Haleakala", "Channel Islands", "Great Basin", "Cuyahoga Valley", "Indiana Dunes", "Hot Springs", "Wrangell", "Glacier Bay", "Kobuk Valley", "Dry Tortugas",
		"New River Gorge", "White Sands", "Guadalupe Mountains", "Theodore Roosevelt", "Gateway Arch", "Lake Clark", "Gates of the Arctic", "Virgin Islands", "American Samoa", "Hawaii Volcanoes",
	},
	"lakes": {
		"Tahoe", "Powell", "Mead", "Michigan", "Superior", "Huron", "Erie", "Ontario", "Champlain", "George",
		"Placid", "Winnipesaukee", "Sebago", "Moosehead", "Chelan", "Coeur d'Alene", "Pend Oreille", "Bear", "Utah", "Okeechobee",
		"Pontchartrain", "Ouachita", "Texoma", "Travis", "Austin", "Livingston", "Conroe", "Hartwell", "Lanier", "Murray",
		"Norman", "Mohave", "Havasu", "Pyramid", "Mono", "Oroville", "Berryessa", "Almanor", "Crescent", "Quinault",
		"Whatcom", "Sammamish", "Washington", "Union", "Roosevelt", "Sakakawea", "Oahe", "Itasca", "Vermilion", "Leech",
		"Mille Lacs", "Geneva", "Winnebago", "Okoboji", "Granby", "Dillon", "Pueblo", "Navajo", "Flaming Gorge", "Fontana",
	},
	"bridges": {
		"Brooklyn", "Manhattan", "Williamsburg", "Queensboro", "Triborough", "Verrazzano", "Whitestone", "Throgs Neck", "Golden Gate", "Bay",
		"Richmond", "Carquinez", "Benicia", "Dumbarton", "San Mateo", "Mackinac", "Ambassador", "Blue Water", "Skyway", "Huey Long",
		"Tamiami", "Rainbow", "Peace", "Roebling", "Kinzua", "Royal Gorge", "Bixby Creek", "Arroyo Seco", "Astoria", "Fremont",
		"Aurora", "Montlake", "Ballard", "Hawthorne", "Burnside", "Steel", "Morrison", "Broadway", "Sellwood", "Tilikum",
		"Ross Island", "St Johns", "Interstate", "Vista", "Navajo", "Stone Arch", "Hennepin", "Wabasha", "Eads", "Chain of Rocks",
		"Clark", "Mark Twain", "Poplar Street", "Hernando de Soto", "Frisco", "Natchez", "Vicksburg", "Sunshine", "Pasco-Kennewick", "Lewiston",
	},
}

// defaultRoomTheme names the rooms of an archetype that declares no theme.
const defaultRoomTheme = "scientists"

// RoomThemes is the names of the naming themes a manifest may ask for, sorted.
func RoomThemes() []string {
	out := make([]string, 0, len(roomThemes))
	for n := range roomThemes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// roomNamer names the rooms of one site: a theme's words each once (a theme run
// dry takes its words again numbered 2, 3 and so on), a function's rooms
// numbered in the order the site has them and a lone one unnumbered.
type roomNamer struct {
	seed  string
	theme []string
	used  map[string]bool
}

func newRoomNamer(scenario, site, theme string) *roomNamer {
	return &roomNamer{seed: scenario + "/" + site, theme: roomThemes[theme], used: map[string]bool{}}
}

// themed is the name of the k-th (from 1) room of a standard: a pure function
// of the site, the standard and k, probing forward past a word taken.
func (r *roomNamer) themed(standard string, k int) string {
	if len(r.theme) == 0 {
		return fmt.Sprintf("Room %d", k)
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(fmt.Sprintf("%s/%s/%d", r.seed, standard, k)))
	start := int(h.Sum64() % uint64(len(r.theme)))
	for pass := 1; ; pass++ {
		for i := 0; i < len(r.theme); i++ {
			name := r.theme[(start+i)%len(r.theme)]
			if pass > 1 {
				name = fmt.Sprintf("%s %d", name, pass)
			}
			if !r.used[name] {
				r.used[name] = true
				return name
			}
		}
	}
}

// function is the name of the k-th of n rooms known by what they are.
func function(name string, k, n int) string {
	if n == 1 {
		return name
	}
	return fmt.Sprintf("%s %d", name, k)
}
