// Package schemas embeds the authoritative Archie v1 contracts.
package schemas

import (
	"embed"
	"fmt"
	"io"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const BaseURL = "https://archie.dev/schemas/v1/"

//go:embed *.schema.json
var Files embed.FS

// Compile resolves only bundled resources, never the network or local files.
func Compile(name string) (*jsonschema.Schema, error) {
	switch name {
	case "record", "root", "response":
	default:
		return nil, fmt.Errorf("unknown schema %q", name)
	}
	c := jsonschema.NewCompiler()
	c.LoadURL = func(url string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("external schema resource forbidden: %s", url)
	}
	for _, file := range []string{"record", "root", "response"} {
		f, err := Files.Open(file + ".schema.json")
		if err != nil {
			return nil, err
		}
		err = c.AddResource(BaseURL+file+".schema.json", f)
		f.Close()
		if err != nil {
			return nil, err
		}
	}
	return c.Compile(BaseURL + name + ".schema.json")
}
