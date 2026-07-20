package main

import (
	"reflect"
	"testing"
)

func TestSplitRunArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		dash    int
		wantN   string
		wantV   []string
		wantErr bool
	}{
		{name: "name only", args: []string{"deploy"}, dash: -1, wantN: "deploy"},
		{name: "values after dash", args: []string{"deploy", "prod", "a b"}, dash: 1,
			wantN: "deploy", wantV: []string{"prod", "a b"}},
		{name: "empty value after dash", args: []string{"deploy", ""}, dash: 1,
			wantN: "deploy", wantV: []string{""}},
		{name: "extra args without dash", args: []string{"deploy", "prod"}, dash: -1, wantErr: true},
		{name: "dash before name", args: []string{"prod"}, dash: 0, wantErr: true},
		{name: "two names before dash", args: []string{"a", "b", "v"}, dash: 2, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, v, err := splitRunArgs(tt.args, tt.dash)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got name=%q values=%v", n, v)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if n != tt.wantN || !reflect.DeepEqual(v, tt.wantV) {
				t.Errorf("got (%q, %v), want (%q, %v)", n, v, tt.wantN, tt.wantV)
			}
		})
	}
}
