package sim

// A paged list API (#2765) hands its records a page at a time and a token for
// the next: the way a real cloud platform lists thousands of rooms. pageKeys is
// the template's mechanics, so a profile's route stays declarative.

const (
	// defaultPageSize is the page a request that names none gets.
	defaultPageSize = 30
	// maxPageSize is the most a request may ask for.
	maxPageSize = 300
)
