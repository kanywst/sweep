package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kanywst/sweep/internal/ps"
)

func TestRunArgs(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		want    string
		wantErr string
	}{
		{args: []string{"version"}, want: "sweep "},
		{args: []string{"-v"}, want: "sweep "},
		{args: []string{"--version"}, want: "sweep "},
		{args: []string{"help"}, want: "Usage:"},
		{args: []string{"-h"}, want: "Usage:"},
		{args: []string{"--bogus"}, wantErr: "flag provided but not defined: -bogus (try sweep help)"},
		{args: []string{"ports", "extra"}, wantErr: `unexpected argument "extra"`},
		{args: []string{"nope"}, wantErr: `unknown command "nope"`},
		{args: []string{"ports", "--older-than", "7d"}, wantErr: "--older-than goes with stop"},
		{args: []string{"stop", ":5173", "--older-than", "7d"}, wantErr: "not both"},
		{args: []string{"--older-than", "soon", "stop"}, wantErr: `invalid age "soon"`},
		{args: []string{"stop"}, wantErr: "--older-than"},
	} {
		var out bytes.Buffer
		err := run(t.Context(), tc.args, &out)
		switch {
		case tc.wantErr != "":
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%v: err = %v, want %q", tc.args, err, tc.wantErr)
			}
		case err != nil:
			t.Errorf("%v: %v", tc.args, err)
		case !strings.Contains(out.String(), tc.want):
			t.Errorf("%v: out = %q, want %q", tc.args, out.String(), tc.want)
		}
	}
}

func TestFlagsAfterCommandAndBefore(t *testing.T) {
	if _, err := ps.Tool("lsof"); err != nil {
		t.Skip(err)
	}
	for _, args := range [][]string{{"ports", "--json"}, {"--json", "ports"}, {"--json"}} {
		var out bytes.Buffer
		if err := run(t.Context(), args, &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if s := strings.TrimSpace(out.String()); !strings.HasPrefix(s, "[") {
			t.Errorf("%v: not JSON: %.80q", args, s)
		}
	}
}
