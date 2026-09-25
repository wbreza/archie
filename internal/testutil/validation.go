package testutil

import (
	"fmt"
	"strings"
)

const ValidationTargets = 493

// ValidationFixture models 37 descriptors sharing 493 distinct source/test/doc/ADR
// references. Every component also repeats one shared pointer.
func ValidationFixture() map[string]string {
	files := map[string]string{}
	root := "schema_version: \"1\"\nid: app\nname: Application\nsummary: Root.\nlinks:\n"
	index := 0
	for area := 0; area < 36; area++ {
		id := fmt.Sprintf("component-%02d", area)
		root += fmt.Sprintf("  - {relation: depends_on, target: %s, description: Component.}\n", id)
		record := fmt.Sprintf("schema_version: \"1\"\nid: %s\nname: Component\nsummary: Architecture boundary.\nlinks:\n", id)
		count := 13
		if area < 25 {
			count++
		}
		for j := 0; j < count; j++ {
			target := ValidationTarget(index)
			relation := []string{"source", "test", "doc", "adr"}[index%4]
			record += fmt.Sprintf("  - {relation: %s, target: %s, description: Local reference.}\n", relation, target)
			files[target] = strings.Repeat("Representative file content.\n", 256)
			index++
		}
		record += fmt.Sprintf("  - {relation: doc, target: %s, description: Shared reference.}\n", ValidationTarget(0))
		files[fmt.Sprintf("component-%02d.archie.yaml", area)] = record
	}
	files["archie.yaml"] = root
	return files
}

func ValidationTarget(index int) string {
	return fmt.Sprintf("references/file-%04d.txt", index)
}
