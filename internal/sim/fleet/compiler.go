package fleet

import (
	"fmt"
	"hash/fnv"
	"net"
	"sort"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// CompiledDevice represents a single device in the compiled inventory.
type CompiledDevice struct {
	Hostname     string         // Location-based hostname (e.g., BOI-17C-415B-DS1)
	HostID       string         // Stable slug (e.g., kestrel-vroom-a1b2c3)
	LoopbackIP   net.IP         // Assigned from site CIDR
	SiteCode     string         // Owning site
	ProfileName  string         // Device profile name
	SystemName   string         // System profile name (the profile, e.g. large-conference-a)
	SystemType   string         // System type slug (Boardroom, Lecture Hall, etc.) for omniglass
	SystemLabel  string         // Human-readable system label (e.g. "Boardroom 415B")
	Role         string         // Role within the system
	Room         string         // Room identifier
	Building     string         // Building identifier
	LocationPath string         // Full location path segments joined
	DisplayName  string         // Docker-style friendly name for the system
	Tags         []string       // Tags from system device slot
	Tenant       string         // Tenant name (Acme Enterprises / Acme University / ...)
	Region       string         // Region (North America / EMEA / APAC ...)
	City         string         // City (Boise / Bangalore / ...)
	CampusName   string         // Human campus name (the site's display name)
	SiteLat      float64        // Campus latitude (0 if unset)
	SiteLng      float64        // Campus longitude
	BuildingLat  float64        // Building latitude (0 if unset; falls back to campus)
	BuildingLng  float64        // Building longitude
	Defaults     map[string]any // Initial state from device profile
}

// CompiledSite holds site-level metadata from the manifest.
type CompiledSite struct {
	Code      string
	Name      string
	CIDR      string
	Tenant    string
	Region    string
	City      string
	Latitude  float64
	Longitude float64
}

// CompiledFleet is the fully expanded inventory.
type CompiledFleet struct {
	Name    string
	Domain  string
	Sites   []CompiledSite
	Devices []CompiledDevice
}

// Compile expands a fleet manifest and profile store into a compiled inventory.
func Compile(manifest *Manifest, store *profile.Store) (*CompiledFleet, error) {
	c := &compiler{
		manifest:      manifest,
		store:         store,
		displayNames:  newDisplayNameGenerator(),
		devices:       nil,
		usedHostnames: make(map[string]bool),
		usedSlugs:     make(map[string]bool),
	}
	if err := c.compile(); err != nil {
		return nil, err
	}

	var sites []CompiledSite
	for _, s := range manifest.Sites {
		sites = append(sites, CompiledSite{
			Code:      s.Code,
			Name:      s.Name,
			CIDR:      s.CIDR,
			Tenant:    manifest.ResolveTenant(s),
			Region:    s.Region,
			City:      s.City,
			Latitude:  s.Latitude,
			Longitude: s.Longitude,
		})
	}

	return &CompiledFleet{
		Name:    manifest.Name,
		Domain:  manifest.Domain,
		Sites:   sites,
		Devices: c.devices,
	}, nil
}

type compiler struct {
	manifest      *Manifest
	store         *profile.Store
	displayNames  *displayNameGenerator
	devices       []CompiledDevice
	usedHostnames map[string]bool
	usedSlugs     map[string]bool
}

// siteContext carries site-level metadata down through the recursive
// compileLocation call so devices can be annotated with tenant/region/
// city/campus name and coordinates without threading 8 separate args.
type siteContext struct {
	site    Site
	tenant  string
	siteLat float64
	siteLng float64
	bldg    string  // current building (set as we recurse into a Location)
	bldgLat float64 // current building coords
	bldgLng float64
}

func (c *compiler) compile() error {
	for _, site := range c.manifest.Sites {
		if err := c.compileSite(site); err != nil {
			return fmt.Errorf("site %q: %w", site.Code, err)
		}
	}
	c.substituteRoomTokens()
	return nil
}

// substituteRoomTokens rewrites identity tokens ({site}, {building}, {room},
// {room-device:<profile>}) in every device's compiled Defaults. A device
// profile is shared across every room it lands in, so identity baked into
// its defaults as a literal would repeat on every instance; expanding the
// tokens per device makes it room-specific. {room-device:<profile>} resolves
// against the other devices in the same room, so this runs once after all
// devices are compiled.
func (c *compiler) substituteRoomTokens() {
	type roomKey struct{ site, building, room string }
	peersByRoom := make(map[roomKey]map[string]string)
	for i := range c.devices {
		d := &c.devices[i]
		k := roomKey{d.SiteCode, d.Building, d.Room}
		peers := peersByRoom[k]
		if peers == nil {
			peers = make(map[string]string)
			peersByRoom[k] = peers
		}
		peers[d.ProfileName] = d.Hostname
	}
	for i := range c.devices {
		d := &c.devices[i]
		peers := peersByRoom[roomKey{d.SiteCode, d.Building, d.Room}]
		d.Defaults = profile.ExpandIdentityTokens(d.Defaults, d.SiteCode, d.Building, d.Room, peers)
	}
}

func (c *compiler) compileSite(site Site) error {
	ipAlloc, err := newIPAllocator(site.CIDR)
	if err != nil {
		return fmt.Errorf("parsing CIDR %q: %w", site.CIDR, err)
	}

	ctx := siteContext{
		site:    site,
		tenant:  c.manifest.ResolveTenant(site),
		siteLat: site.Latitude,
		siteLng: site.Longitude,
	}

	// Flat systems directly on site (no building/floor).
	for _, sv := range site.Systems {
		rooms := c.expandRooms(sv)
		for _, room := range rooms {
			path := []string{site.Code, room}
			if err := c.compileSystem(ctx, sv, path, room, ipAlloc); err != nil {
				return err
			}
		}
	}

	// Nested locations.
	for _, loc := range site.Locations {
		if err := c.compileLocation(ctx, []string{site.Code}, loc, ipAlloc); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileLocation(ctx siteContext, pathPrefix []string, loc Location, ipAlloc *ipAllocator) error {
	// Building lands in the hostname path whenever it's declared,
	// room numbers don't reliably encode the building (e.g. `415B`
	// only encodes floor 4 and a slot suffix; nothing in the room
	// name says it's in 17C vs 18A). Manifest authors who want to
	// pack building into the room name do that themselves and leave
	// the Building field empty (e.g. SJC rooms named `11345C`); the
	// compiler honors whichever choice was made. Floor is never a
	// path segment, room numbers carry it where it matters, and a
	// separate floor segment would just duplicate that information.
	currentPath := pathPrefix
	if loc.Building != "" {
		ctx.bldg = loc.Building
		if loc.Latitude != 0 || loc.Longitude != 0 {
			ctx.bldgLat = loc.Latitude
			ctx.bldgLng = loc.Longitude
		}
		currentPath = append(append([]string{}, pathPrefix...), loc.Building)
	}

	// Systems at this level.
	for _, sv := range loc.Systems {
		rooms := c.expandRooms(sv)
		for _, room := range rooms {
			fullPath := append(append([]string{}, currentPath...), room)
			if err := c.compileSystem(ctx, sv, fullPath, room, ipAlloc); err != nil {
				return err
			}
		}
	}

	// Recurse into legacy nested floors. Floor itself is dropped
	// from the path (see header comment), we still need to walk
	// the structure so manifest authors using the older form don't
	// have to migrate to keep their fleets compiling.
	for _, sub := range loc.Floors {
		if err := c.compileLocation(ctx, currentPath, sub, ipAlloc); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileSystem(ctx siteContext, sv SystemVolume, locationPath []string, room string, ipAlloc *ipAllocator) error {
	sysprof, ok := c.store.Systems[sv.Profile]
	if !ok {
		return fmt.Errorf("unknown system profile %q", sv.Profile)
	}

	pathStr := strings.Join(locationPath, "-")
	systemLabel := renderSystemLabel(sv.Label, room, ctx.bldg, ctx.site.Code)
	// Hash on (site, path, room, system) so display names are spread
	// pseudo-randomly across the adjective×surname space and stable
	// across reruns. The earlier serial generator clumped every
	// system into "boring_*" / "admiring_*" buckets in alpha order.
	displayName := c.displayNames.next(c.manifest.Name + "/" + ctx.site.Code + "/" + pathStr + "/" + room + "/" + sv.Profile)

	for _, slot := range sysprof.Devices {
		devprof, ok := c.store.Devices[slot.Profile]
		if !ok {
			return fmt.Errorf("system profile %q references unknown device profile %q", sv.Profile, slot.Profile)
		}

		count := slot.Count
		if count < 1 {
			count = 1
		}

		for i := 1; i <= count; i++ {
			hostname := fmt.Sprintf("%s-%s%d", pathStr, slot.Suffix, i)
			if c.usedHostnames[hostname] {
				return fmt.Errorf("duplicate hostname: %s", hostname)
			}
			c.usedHostnames[hostname] = true

			ip, err := ipAlloc.next()
			if err != nil {
				return fmt.Errorf("IP allocation for %s: %w", hostname, err)
			}

			slug := generateSlug(devprof.SlugPrefix, hostname, c.manifest.Name)
			if c.usedSlugs[slug] {
				return fmt.Errorf("duplicate host slug: %s", slug)
			}
			c.usedSlugs[slug] = true

			dev := CompiledDevice{
				Hostname:     hostname,
				HostID:       slug,
				LoopbackIP:   ip,
				SiteCode:     ctx.site.Code,
				ProfileName:  devprof.Name,
				SystemName:   sysprof.Name,
				SystemType:   sv.SystemType,
				SystemLabel:  systemLabel,
				Role:         slot.Role,
				Room:         room,
				Building:     ctx.bldg,
				LocationPath: pathStr,
				DisplayName:  displayName,
				Tags:         slot.Tags,
				Tenant:       ctx.tenant,
				Region:       ctx.site.Region,
				City:         ctx.site.City,
				CampusName:   ctx.site.Name,
				SiteLat:      ctx.siteLat,
				SiteLng:      ctx.siteLng,
				BuildingLat:  ctx.bldgLat,
				BuildingLng:  ctx.bldgLng,
				Defaults:     deepCopyMap(devprof.Defaults),
			}
			enrichDefaults(&dev)
			c.devices = append(c.devices, dev)
		}
	}
	return nil
}

// renderSystemLabel substitutes {room}, {building}, {site} into the label
// template if one is provided. Empty template falls back to the system
// profile's display_name (set by the caller, here we leave it blank so
// the caller can substitute the docker-style display name).
func renderSystemLabel(template, room, building, site string) string {
	if template == "" {
		return ""
	}
	out := template
	out = strings.ReplaceAll(out, "{room}", room)
	out = strings.ReplaceAll(out, "{building}", building)
	out = strings.ReplaceAll(out, "{site}", site)
	return out
}

func (c *compiler) expandRooms(sv SystemVolume) []string {
	if len(sv.Rooms) > 0 {
		return sv.Rooms
	}
	rooms := make([]string, sv.Count)
	start := sv.StartRoom
	if start == 0 {
		start = 100
	}
	for i := range rooms {
		rooms[i] = fmt.Sprintf("%d", start+i)
	}
	return rooms
}

// enrichDefaults fills the per-instance identity fields that device
// profiles leave blank: the generated hostname and loopback IP, plus a
// deterministic serial and MAC derived from the device's stable HostID.
// Each field is written only when the profile declares it (the nested
// key exists) and left it blank, a profile that hard-codes a real value
// keeps it. Serial/MAC are derived from HostID so they stay identical
// across reruns and across a fresh reseed.
func enrichDefaults(d *CompiledDevice) {
	setBlankLeaf(d.Defaults, "configuration.network.hostname", d.Hostname)
	setBlankLeaf(d.Defaults, "configuration.device.serial", generateSerial(d.HostID))
	if d.LoopbackIP != nil {
		setBlankLeaf(d.Defaults, "status.network.ipv4.address", d.LoopbackIP.String())
	}
	setBlankLeaf(d.Defaults, "status.network.ipv4.mac", generateMAC(d.HostID))
}

// setBlankLeaf walks the dotted path through nested map[string]any and
// sets the leaf to value, but only when the leaf key already exists and
// currently holds an empty string. Missing intermediate keys, non-map
// intermediates, and non-blank or non-string leaves are all left
// untouched, so the profile schema stays the single source of which
// fields a device actually has.
func setBlankLeaf(m map[string]any, dotted, value string) {
	keys := strings.Split(dotted, ".")
	cur := m
	for i, k := range keys {
		if i == len(keys)-1 {
			if existing, ok := cur[k].(string); ok && existing == "" {
				cur[k] = value
			}
			return
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			return
		}
		cur = next
	}
}

// generateSerial produces a stable, realistic-looking serial number from
// the device's HostID. 10 uppercase hex digits behind an "SN-" prefix.
func generateSerial(hostID string) string {
	h := fnv.New64a()
	h.Write([]byte("serial/" + hostID))
	return fmt.Sprintf("SN-%010X", h.Sum64()&0xFFFFFFFFFF)
}

// generateMAC produces a stable locally-administered unicast MAC from the
// device's HostID. First octet 0x02 sets the locally-administered bit and
// clears the multicast bit; the remaining five octets come from the hash.
// MACs are cosmetic here (everything talks to localhost), so collisions
// across the fleet are harmless.
func generateMAC(hostID string) string {
	h := fnv.New64a()
	h.Write([]byte("mac/" + hostID))
	sum := h.Sum64()
	return fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x",
		byte(sum>>32), byte(sum>>24), byte(sum>>16), byte(sum>>8), byte(sum))
}

// generateSlug produces a deterministic stable slug from prefix + a hash suffix.
func generateSlug(prefix, hostname, fleetName string) string {
	h := fnv.New32a()
	h.Write([]byte(fleetName + "/" + hostname))
	sum := h.Sum32()
	suffix := fmt.Sprintf("%06x", sum&0xFFFFFF)
	return prefix + "-" + suffix
}

// ipAllocator hands out IPs from a CIDR, skipping network and broadcast.
type ipAllocator struct {
	network *net.IPNet
	current net.IP
	max     net.IP
}

func newIPAllocator(cidr string) (*ipAllocator, error) {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	// Start at network + 1 (skip network address).
	start := nextIP(ip.Mask(network.Mask))

	// Calculate broadcast (max) address.
	mask := network.Mask
	broadcast := make(net.IP, len(ip.Mask(network.Mask)))
	for i := range broadcast {
		broadcast[i] = ip.Mask(network.Mask)[i] | ^mask[i]
	}

	return &ipAllocator{
		network: network,
		current: start,
		max:     broadcast,
	}, nil
}

func (a *ipAllocator) next() (net.IP, error) {
	if !a.network.Contains(a.current) || a.current.Equal(a.max) {
		return nil, fmt.Errorf("CIDR %s exhausted", a.network)
	}
	ip := make(net.IP, len(a.current))
	copy(ip, a.current)
	a.current = nextIP(a.current)
	return ip, nil
}

func nextIP(ip net.IP) net.IP {
	next := make(net.IP, len(ip))
	copy(next, ip)
	// Treat as big-endian integer and increment.
	for i := len(next) - 1; i >= 0; i-- {
		next[i]++
		if next[i] != 0 {
			break
		}
	}
	return next
}

// deepCopyMap returns a deep copy of a map[string]any tree.
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			out[k] = deepCopyMap(val)
		case []any:
			out[k] = deepCopySlice(val)
		default:
			out[k] = v
		}
	}
	return out
}

func deepCopySlice(s []any) []any {
	out := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]any:
			out[i] = deepCopyMap(val)
		case []any:
			out[i] = deepCopySlice(val)
		default:
			out[i] = v
		}
	}
	return out
}

// displayNameGenerator produces unique title-cased "Adjective Surname"
// names (e.g. "Affectionate Bose"). Humanizing at the source means
// every downstream consumer (lab console, omniglass UI, sync-emitted
// fields) sees the same friendly form without doing its own
// transformation.
//
// Hashed-seed lookup picks a starting cell so names spread across
// the (adjective × surname) space rather than clumping in the first
// few buckets. Linear probing on collision preserves uniqueness.
type displayNameGenerator struct {
	used  map[string]bool
	index int // empty-seed fallback counter
}

func newDisplayNameGenerator() *displayNameGenerator {
	return &displayNameGenerator{used: map[string]bool{}}
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

// next returns a Docker-flavored "Adjective Surname" for the given
// seed. Each system passes its identity (site + location path + room
// + system_name) so the picked pair is stable across reruns. The
// hash picks a starting (adjective, surname) cell; on collision we
// linear-probe forward, which keeps uniqueness while still spreading
// chosen pairs across the whole space.
//
// Empty seed falls back to a serial counter (preserves the original
// counter-based contract for any caller that doesn't have a stable
// identity to seed with).
func (g *displayNameGenerator) next(seed string) string {
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

// SortDevices sorts devices by site, location path, then hostname for deterministic output.
func SortDevices(devices []CompiledDevice) {
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].SiteCode != devices[j].SiteCode {
			return devices[i].SiteCode < devices[j].SiteCode
		}
		if devices[i].LocationPath != devices[j].LocationPath {
			return devices[i].LocationPath < devices[j].LocationPath
		}
		return devices[i].Hostname < devices[j].Hostname
	})
}
