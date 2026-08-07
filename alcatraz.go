package alcatraz

import (
	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/recognizers"
)

// Re-exported framework types so most callers only need this package.
type (
	// Engine runs recognizers over text. See analyzer.Engine.
	Engine = analyzer.Engine
	// Result is a single detection. See analyzer.Result.
	Result = analyzer.Result
	// Options tunes an Analyze call. See analyzer.Options.
	Options = analyzer.Options
	// Recognizer is the detector contract. See analyzer.Recognizer.
	Recognizer = analyzer.Recognizer
	// Registry holds recognizers by language. See analyzer.Registry.
	Registry = analyzer.Registry
)

// Score bounds, re-exported for convenience.
const (
	MinScore = analyzer.MinScore
	MaxScore = analyzer.MaxScore
)

// NewEngine builds an engine pre-loaded with the full built-in recognizer set
// for the given languages. With no arguments it defaults to English.
func NewEngine(languages ...string) *Engine {
	if len(languages) == 0 {
		languages = []string{"en"}
	}
	reg := analyzer.NewRegistry(languages...)
	for _, lang := range languages {
		recognizers.LoadDefaults(reg, lang)
	}
	return analyzer.NewEngine(reg, languages)
}
