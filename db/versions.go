package db

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

// VersionLayout is the stamp `dbmate new` gives a migration: the current UTC
// time to the second, before the first underscore of the file name. dbmate
// keys an applied migration on this version alone and orders by it, so the
// version is the migration's identity, not its contents.
const VersionLayout = "20060102150405"

// ClockSkew is how far past the checker's clock an added migration's stamp may
// sit. `dbmate new` stamps the moment it runs, so a stamp later than that is
// hand-typed, and one on main would fail every later `dbmate new` migration
// against the newer-than-base rule until its time passed.
const ClockSkew = 10 * time.Minute

// Names lists the embedded migration files.
func Names() ([]string, error) {
	entries, err := fs.ReadDir(FS, "migrations")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// Version returns the version of a migration file name, refusing a name that
// does not open with fourteen digits and an underscore, the shape of a
// `dbmate new` stamp. The digits are not parsed as a time: two applied
// migrations carry a minute of 60 (20260807116000, 20260807146000), and an
// applied migration is never renamed.
func Version(name string) (string, error) {
	version, _, ok := strings.Cut(name, "_")
	if !ok || len(version) != len(VersionLayout) || strings.Trim(version, "0123456789") != "" {
		return "", fmt.Errorf("%s: no dbmate new stamp (YYYYMMDDHHMMSS_) before the first underscore", name)
	}
	return version, nil
}

// CheckUnique reports each name that carries no version and each version two
// files share. Of two files with one version, dbmate records whichever runs
// first and treats the other as applied, so it never runs.
func CheckUnique(names []string) []error {
	var errs []error
	seen := map[string]string{}
	for _, name := range sorted(names) {
		version, err := Version(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if first, dup := seen[version]; dup {
			errs = append(errs, fmt.Errorf("version %s is taken by both %s and %s; dbmate would never run one of them", version, first, name))
			continue
		}
		seen[version] = name
	}
	return errs
}

// CheckAgainstBase reports each migration head adds over base (a name base
// lacks) whose version is not newer than base's newest, or is later than now
// by more than ClockSkew. A branch cut before another migration landed carries
// the first: dbmate, not in strict mode, still runs it on a database that ran
// the newer one, but after it, while a fresh database runs the chain in
// version order. Names that carry no version are CheckUnique's to report.
func CheckAgainstBase(base, head []string, now time.Time) []error {
	inBase := map[string]bool{}
	newest, newestName := "", ""
	for _, name := range base {
		inBase[name] = true
		if version, err := Version(name); err == nil && version > newest {
			newest, newestName = version, name
		}
	}
	stamp := now.UTC().Format(VersionLayout)
	latest := now.UTC().Add(ClockSkew).Format(VersionLayout)
	wait := ""
	if newest > stamp {
		wait = fmt.Sprintf(" (once the clock passes %s UTC: that one is stamped ahead of it, so no fresh stamp is newer yet)", newest)
	}
	var errs []error
	for _, name := range sorted(head) {
		if inBase[name] {
			continue
		}
		version, err := Version(name)
		if err != nil {
			continue
		}
		if version <= newest {
			errs = append(errs, fmt.Errorf("%s is not newer than the base's newest migration, %s: stamp a fresh one with make migration NAME=<name>%s, move the SQL into it, and delete this file", name, newestName, wait))
		}
		if version > latest {
			errs = append(errs, fmt.Errorf("%s is stamped later than the clock (%s UTC): dbmate new stamps the moment it runs, so create it with make migration NAME=<name>", name, now.UTC().Format(time.DateTime)))
		}
	}
	return errs
}

func sorted(names []string) []string {
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out
}
