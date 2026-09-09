// Package testutil supplies isolated, locally rooted fixture directories.
package testutil

import "os"

func ScratchBase() string {
	if base := os.Getenv("ARCHIE_TEST_SCRATCH"); base != "" {
		return base
	}
	dir, _ := os.Getwd()
	return dir
}
