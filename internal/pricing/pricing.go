package pricing

import "strings"

type Rate struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

var table = map[string]Rate{
	"claude-opus-4":      {Input: 15, Output: 75, CacheRead: 1.5, CacheWrite: 18.75},
	"claude-sonnet-4":    {Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
	"claude-sonnet-4-5":  {Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
	"claude-haiku-4":     {Input: 0.8, Output: 4, CacheRead: 0.08, CacheWrite: 1},
	"claude-3-5-sonnet":  {Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
	"claude-3-5-haiku":   {Input: 0.8, Output: 4, CacheRead: 0.08, CacheWrite: 1},
	"gpt-4o":             {Input: 2.5, Output: 10},
	"gpt-4o-mini":        {Input: 0.15, Output: 0.6},
	"gpt-4.1":            {Input: 2, Output: 8},
	"gpt-4.1-mini":       {Input: 0.4, Output: 1.6},
	"gpt-4.1-nano":       {Input: 0.1, Output: 0.4},
	"o1":                 {Input: 15, Output: 60},
	"o3":                 {Input: 10, Output: 40},
	"o3-mini":            {Input: 1.1, Output: 4.2},
	"o4-mini":            {Input: 1.1, Output: 4.2},
	"gemini-2.5-pro":     {Input: 1.25, Output: 10},
	"gemini-2.5-flash":   {Input: 0.075, Output: 0.3},
}

func Lookup(model string) (Rate, bool) {
	if r, ok := table[model]; ok {
		return r, true
	}
	for k, r := range table {
		if strings.HasPrefix(model, k) {
			return r, true
		}
	}
	return Rate{}, false
}

func Cost(model string, in, out, cacheRead, cacheWrite int) float64 {
	r, ok := Lookup(model)
	if !ok {
		return 0
	}
	return float64(in)*r.Input/1e6 +
		float64(out)*r.Output/1e6 +
		float64(cacheRead)*r.CacheRead/1e6 +
		float64(cacheWrite)*r.CacheWrite/1e6
}
