package autoupdate

import (
	"fmt"
	"testing"

	"github.com/openshift/api/config/v1"
)

func TestNextUpdate(t *testing.T) {
	tests := []struct {
		avail []string
		want  string
	}{{
		avail: []string{"0.0.0", "0.0.1", "0.0.2"},
		want:  "0.0.2",
	}, {
		avail: []string{"0.0.2", "0.0.0", "0.0.1"},
		want:  "0.0.2",
	}, {
		avail: []string{"0.0.1", "0.0.0", "0.0.2"},
		want:  "0.0.2",
	}, {
		avail: []string{"0.0.0", "0.0.0+new.2", "0.0.0+new.3"},
		want:  "0.0.0+new.3",
	}, {
		avail: []string{"0.0.0", "0.0.0-new.2", "0.0.0-new.3"},
		want:  "0.0.0",
	}}
	for idx, test := range tests {
		t.Run(fmt.Sprintf("test: #%d", idx), func(t *testing.T) {
			ups := []v1.Update{}
			for _, v := range test.avail {
				ups = append(ups, v1.Update{Version: v})
			}

			got := nextUpdate(ups)
			if got.Version != test.want {
				t.Fatalf("mismatch: got %s want: %s", got, test.want)
			}
		})
	}
}
