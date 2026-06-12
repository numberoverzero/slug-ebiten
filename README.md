# slug-ebiten

GPU font rendering for Ebitengine using the Slug curve-coverage algorithm.

## Compiling a `font.ttf` to a `font.blob`

A blob is the font's glyph outlines precomputed into the curve-coverage form the
shader reads. Compile it once with `compile.Compile`, then write the bytes to
disk (or embed them) so your program loads the blob instead of the `.ttf`.

```go
package main

import (
	"log"
	"os"

	"github.com/numberoverzero/slug-ebiten/compile"
)

func main() {
	ttf, err := os.ReadFile("font.ttf")
	if err != nil {
		log.Fatal(err)
	}

	
	blob, err := compile.Compile(ttf, []compile.GlyphRange{{Lo: ' ', Hi: '~'}})
	// nil keeps every glyph in the font:
	// blob, err := compile.Compile(ttf, nil)
	
    if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile("font.blob", blob, 0o644); err != nil {
		log.Fatal(err)
	}
}
```

## Loading a `font.blob` to a `slug.Font`

`slug.Load` turns the blob bytes into a `*slug.Font` ready to draw. Load once at
startup and reuse the `Font` for every frame. The blob is just bytes, so you can
embed it in your binary:

```go
import (
	_ "embed"

	"github.com/numberoverzero/slug-ebiten/slug"
)

//go:embed font.blob
var fontBlob []byte

func loadFont() *slug.Font {
	font, err := slug.Load(fontBlob)
	if err != nil {
		log.Fatal(err)
	}
	return font
}
```

## Rendering a `slug.Font` to an `ebiten.Image`

`Font.Draw` lays out runs of text from a pen origin and draws them in one GPU
call. The `GeoM` sizes and positions the text: `Scale` is pixels per em,
`Translate` places the baseline's left corner, and an optional `Rotate` or
`Skew` before `Translate` acts about the pen origin. Pass several `Run`s to mix
colors in a single call.

```go
func (g *game) Draw(screen *ebiten.Image) {
	const size = 48 // pixels per em

	var geom ebiten.GeoM
	geom.Scale(size, size)
	geom.Translate(20, 80)

	g.font.Draw(screen, []slug.Run{
		{Text: "Hello, ", Color: color.RGBA{R: 0x33, G: 0x66, B: 0xcc, A: 0xff}},
		{Text: "World"}, // no Color falls back to DrawOptions.Color
	}, &slug.DrawOptions{GeoM: geom, Color: color.Black})
}
```

Text stays sharp at any scale, so the same blob renders crisply from caption to
banner size. Use `Font.Measure` to get a text run's bounding box for layout, and
`Font.LineMetrics` for ascent, descent, and line height.



## See also

* Slug reference shaders: https://github.com/EricLengyel/Slug
* Slug: a decade in review (algorithm + public-domain dedication): https://terathon.com/blog/decade-slug.html
* Slug Library (commercial reference): https://sluglibrary.com/
* Ebitengine: https://ebitengine.org/
* Ebitengine shaders (Kage): https://ebitengine.org/en/documents/shader.html
