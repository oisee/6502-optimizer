package result

import (
	"encoding/gob"
	"os"
)

// Checkpoint holds the state for resuming a search.
type Checkpoint struct {
	Rules          []Rule
	CompletedTarget int
	TargetLen      int
}

// SaveCheckpoint writes a checkpoint to a gob file.
func SaveCheckpoint(path string, ckpt Checkpoint) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gob.NewEncoder(f).Encode(ckpt)
}

// LoadCheckpoint reads a checkpoint from a gob file.
func LoadCheckpoint(path string) (Checkpoint, error) {
	f, err := os.Open(path)
	if err != nil {
		return Checkpoint{}, err
	}
	defer f.Close()
	var ckpt Checkpoint
	if err := gob.NewDecoder(f).Decode(&ckpt); err != nil {
		return Checkpoint{}, err
	}
	return ckpt, nil
}
