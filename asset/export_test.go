package asset

// FileNameForTest exposes the disk cache file name of a key, so that the
// external test can assert that every component of the key changes it and that
// no credential can appear in it.
func FileNameForTest(k Key) string { return k.fileName() }

// HintNameForTest exposes the file name of a revision hint for the same
// reason.
func HintNameForTest(namespace string, id ID) string { return hintName(namespace, id) }
