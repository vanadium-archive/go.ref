package impl

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"veyron.io/veyron/veyron2/vlog"
)

const (
	checksum = "checksum"
	data     = "data"
	lock     = "lock"
)

// checksumExists checks whether the given part path is valid and
// contains a checksum. The implementation uses the existence of
// the path dir to determine whether the part is valid, and the
// existence of checksum to determine whether the binary part
// exists.
func checksumExists(path string) error {
	switch _, err := os.Stat(path); {
	case os.IsNotExist(err):
		return errInvalidPart
	case err != nil:
		vlog.Errorf("Stat(%v) failed: %v", path, err)
		return errOperationFailed
	}
	checksumFile := filepath.Join(path, checksum)
	_, err := os.Stat(checksumFile)
	switch {
	case os.IsNotExist(err):
		return errNotFound
	case err != nil:
		vlog.Errorf("Stat(%v) failed: %v", checksumFile, err)
		return errOperationFailed
	default:
		return nil
	}
}

// generatePartPath generates a path for the given binary part.
func (i *binaryService) generatePartPath(part int) string {
	return generatePartPath(i.path, part)
}

func generatePartPath(dir string, part int) string {
	return filepath.Join(dir, fmt.Sprintf("%d", part))
}

// getParts returns a collection of paths to the parts of the binary.
func getParts(path string) ([]string, error) {
	infos, err := ioutil.ReadDir(path)
	if err != nil {
		vlog.Errorf("ReadDir(%v) failed: %v", path, err)
		return []string{}, errOperationFailed
	}
	nDirs := 0
	for _, info := range infos {
		if info.IsDir() {
			nDirs++
		}
	}
	result := make([]string, nDirs)
	for _, info := range infos {
		if info.IsDir() {
			partName := info.Name()
			idx, err := strconv.Atoi(partName)
			if err != nil {
				vlog.Errorf("Atoi(%v) failed: %v", partName, err)
				return []string{}, errOperationFailed
			}
			if idx < 0 || idx >= len(infos) || result[idx] != "" {
				return []string{}, errOperationFailed
			}
			result[idx] = filepath.Join(path, partName)
		} else {
			if info.Name() == "name" || info.Name() == "mediainfo" {
				continue
			}
			// The only entries should correspond to the part dirs.
			return []string{}, errOperationFailed
		}
	}
	return result, nil
}

// createObjectNameTree returns a tree of all the valid object names in the
// repository.
func (i *binaryService) createObjectNameTree() *treeNode {
	pattern := i.state.root
	for d := 0; d < i.state.depth; d++ {
		pattern = filepath.Join(pattern, "*")
	}
	pattern = filepath.Join(pattern, "*", "name")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	tree := newTreeNode()
	for _, m := range matches {
		name, err := ioutil.ReadFile(m)
		if err != nil {
			continue
		}
		elems := strings.Split(string(name), string(filepath.Separator))
		tree.find(elems, true)
	}
	return tree
}

type treeNode struct {
	children map[string]*treeNode
}

func newTreeNode() *treeNode {
	return &treeNode{children: make(map[string]*treeNode)}
}

func (n *treeNode) find(names []string, create bool) *treeNode {
	for {
		if len(names) == 0 {
			return n
		}
		if next, ok := n.children[names[0]]; ok {
			n = next
			names = names[1:]
			continue
		}
		if create {
			nn := newTreeNode()
			n.children[names[0]] = nn
			n = nn
			names = names[1:]
			continue
		}
		return nil
	}
}
