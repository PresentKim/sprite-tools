package main

import (
	"slices"
	"testing"
)

func TestResolveCommand(t *testing.T) {
	cases := map[string]string{
		"reduce": "reduce", "rd": "reduce", "red": "reduce", "simplify": "reduce",
		"slice": "slice", "sl": "slice", "split": "slice",
		"downscale": "downscale", "ds": "downscale", "d": "downscale",
		"index": "index", "ix": "index", "i": "index", "palette": "index",
		"recolor": "recolor", "rc": "recolor", "rec": "recolor", "palette-convert": "recolor",
		"RD": "reduce",
	}
	for in, want := range cases {
		cmd, _ := resolveCommand(in)
		if cmd == nil || cmd.name != want {
			t.Errorf("%q: want %s, got %v", in, want, cmd)
		}
	}

	if cmd, cands := resolveCommand("re"); cmd != nil || !slices.Equal(cands, []string{"reduce", "recolor"}) {
		t.Errorf("ambiguous prefix: got %v %v", cmd, cands)
	}
	if cmd, _ := resolveCommand("xyz"); cmd != nil {
		t.Errorf("unknown command resolved to %s", cmd.name)
	}
}

func TestAliasesUnique(t *testing.T) {
	seen := map[string]string{}
	for _, c := range commands {
		for _, n := range append(append([]string{c.name}, c.aliases...), c.legacy...) {
			if prev, ok := seen[n]; ok {
				t.Errorf("%q used by both %s and %s", n, prev, c.name)
			}
			seen[n] = c.name
		}
	}
}
