// Package assets embeds the default development skin so installed binaries
// run cleanly from any working directory.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed dev-skin/*.bmp dev-skin/*.txt
var devSkinRaw embed.FS

// DevSkinFS returns an fs.FS rooted at the embedded dev-skin directory.
func DevSkinFS() fs.FS {
	sub, err := fs.Sub(devSkinRaw, "dev-skin")
	if err != nil {
		panic(err)
	}
	return sub
}
