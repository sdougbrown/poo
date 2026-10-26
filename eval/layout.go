package eval

import (
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// runsDirName is the directory under the output directory that holds
	// the runs' logs.
	runsDirName = "runs"
	// seedDirPrefix starts the name of a seed's directory.
	seedDirPrefix = "seed"
	// workdirName is the directory a run works in, under its seed's
	// directory in the work root.
	workdirName = "work"
)

// jobDir returns the directory of a job under a root: the runs
// directory for the log, or the work root for the scratch.
//
// The layout is <root>/<task>/<model>/seed<N>. The runner writes it and
// the judge reads it back with parseRunDir.
func jobDir(root string, j job) string {
	return filepath.Join(root, j.task.ID, j.model.Name, seedDirPrefix+strconv.Itoa(j.seed))
}

// jobWorkdir returns the directory a job works in.
func jobWorkdir(workRoot string, j job) string {
	return filepath.Join(jobDir(workRoot, j), workdirName)
}

// parseRunDir is the inverse of jobDir. It reads the task, the model and
// the seed out of a run's directory, relative to the runs directory.
//
// The last three components name the run. The directories above them
// are the runner's own, and their names do not matter. A directory that
// does not end in a seed's name is not a run's.
func parseRunDir(runsDir, dir string) (task, model string, seed int, ok bool) {
	rel, err := filepath.Rel(runsDir, dir)
	if err != nil {
		return "", "", 0, false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 {
		return "", "", 0, false
	}
	seed, ok = seedFromName(parts[len(parts)-1])
	if !ok {
		return "", "", 0, false
	}
	return parts[len(parts)-3], parts[len(parts)-2], seed, true
}

// seedFromName reads the seed out of a directory name.
//
// The name is "seed" and a number. A name that is not that shape names
// no seed.
func seedFromName(name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, seedDirPrefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}
