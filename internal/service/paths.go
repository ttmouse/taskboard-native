package service

import (
	"fmt"
	"sync"

	"github.com/egoist/mygo"

	"taskboard-native/internal/store"
)

// AppName is the product name used to locate the data directory. It is
// pinned explicitly because mygo falls back to the executable name, which
// would give tb, taskboard-mcp and taskboard three different databases.
const AppName = "taskboard"

var pinName sync.Once

// userDataDir returns mygo's per-user application data directory for this
// product, created on first use.
func userDataDir() (string, error) {
	pinName.Do(func() { mygo.App.SetName(AppName) })
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", fmt.Errorf("resolve user data directory: %w", err)
	}
	return dir, nil
}

// DefaultDBFile is the database file name inside the user data directory.
const DefaultDBFile = "taskboard.db"

// Board is one project's column-and-card snapshot, as served by
// GET /api/projects/{id}/board.
type Board = store.Board
