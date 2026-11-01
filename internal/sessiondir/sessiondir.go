package sessiondir

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var idPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z(-[0-9]+)?$`)

// Manager creates unique session directories beneath a sessions root. The
// console creates the directory before starting fluxzy so it knows the active
// session id immediately instead of guessing from meta.json timestamps.
type Manager struct {
	root string
}

func New(root string) *Manager {
	return &Manager{root: root}
}

func (m *Manager) Root() string { return m.root }

// Next creates and returns a new session id and directory. The first candidate
// is the UTC second; collisions get a numeric suffix.
func (m *Manager) Next(now time.Time) (string, string, error) {
	if err := os.MkdirAll(m.root, 0o750); err != nil {
		return "", "", fmt.Errorf("create sessions root: %w", err)
	}
	base := now.UTC().Format("20060102T150405Z")
	for i := 0; i < 1000; i++ {
		id := base
		if i > 0 {
			id = fmt.Sprintf("%s-%d", base, i+1)
		}
		dir := filepath.Join(m.root, id)
		err := os.Mkdir(dir, 0o750)
		if err == nil {
			return id, dir, nil
		}
		if !os.IsExist(err) {
			return "", "", fmt.Errorf("create session dir %s: %w", id, err)
		}
	}
	return "", "", fmt.Errorf("could not allocate a unique session id for %s", base)
}

func ValidID(id string) bool { return idPattern.MatchString(id) }
