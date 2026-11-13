package flow

import (
	"fmt"
	"path"
	"strings"
)

// ReplaceNamespace returns the destination path of a source repository.
// It uses the Harbor flattening rules. The code comes from replaceNamespace in
// goharbor/harbor src/controller/replication/flow/stage.go (Apache-2.0,
// Copyright Project Harbor Authors). It does not have the
// dstRepoComponentPathType check, because the destination is always Harbor.
//
//	replaceCount -1: namespace/<last component>           ("Flatten All Levels")
//	replaceCount  0: namespace/<full source path>         ("No Flatten")
//	replaceCount  N: namespace/<source path minus N leading components>
//
// The function always keeps the last component (the image name). Thus N must
// not be more than len(components)-1. If namespace is empty, the function
// returns the repository without changes.
func ReplaceNamespace(repository, namespace string, replaceCount int) (string, error) {
	if len(namespace) == 0 {
		return repository, nil
	}

	src := strings.Split(repository, "/")
	n := len(src)

	var prefix string
	switch {
	case replaceCount < 0:
		prefix = namespace
	case replaceCount > n-1:
		return "", fmt.Errorf("the source repository %q contains only %d path components %v excepting the last one, but the destination namespace flattening level is %d",
			repository, n-1, src[:n-1], replaceCount)
	default:
		prefix = namespace + "/" + strings.Join(src[replaceCount:n-1], "/")
	}
	return path.Join(prefix, src[n-1]), nil
}
