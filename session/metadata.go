package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// MetadataOf returns the metadata of a recorded session.
//
// The metadata is the session's header: the first line of its log. The
// header is read without the authority, so a caller may learn what a
// session was before it takes the lock on it.
//
// A session the directory holds no log for is the store's not-found
// failure, and a log in a format this build does not know is the
// codec's mismatch.
func MetadataOf(dir, id string) (Metadata, error) {
	path := filepath.Join(dir, id+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Metadata{}, ErrSessionNotFound
		}
		return Metadata{}, &Error{Code: CodeStoreError, Message: "reading " + path, Err: err}
	}
	lines := bytesLines(data)
	if len(lines) == 0 {
		return Metadata{}, &Error{Code: CodeCorruptLog, Message: "the log holds no session"}
	}
	codec := NewCodec()
	format, err := logFormat(lines[0])
	if err != nil {
		return Metadata{}, err
	}
	if format != codec.Version() {
		// A log in another format is migrated before it is read, and
		// the migration may not know it.
		migrated, err := codec.Migrate(format, lines)
		if err != nil {
			return Metadata{}, err
		}
		lines = migrated
	}
	return metadataOf(lines[0])
}
