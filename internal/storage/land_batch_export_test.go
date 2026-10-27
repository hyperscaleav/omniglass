package storage

// ExportHealthLockKey is the string an owner's health lock hashes, for a test
// that holds or probes it.
func ExportHealthLockKey(ownerKind, ownerID string) string { return healthLockKey(ownerKind, ownerID) }
