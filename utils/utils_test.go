package utils

import (
	"testing"
)

func TestBadgesInstalled(t *testing.T) {
	type test struct {
		s string
		n int
	}

	tests := []test{
		{s: "hello", n: 0},
		{s: "https://images.microbadger.com/badges/image/alpine.svg", n: 1},
		{s: "https://images.microbadger.com/badges/version/alpine.svg", n: 1},
		{s: "https://images.microbadger.com/badges/commit/alpine.svg", n: 1},
		{s: "https://images.microbadger.com/badges/license/alpine.svg", n: 1},
		{s: "https://images.microbadger.com/badges/version/bateau/alpine_baseimage.svg", n: 1},
		{s: "https://images.microbadger.com/badges/commit/linuxadmin/cuda-7.5.svg", n: 1},
		{s: "https://images.microbadger.com/badges/image/jetstack/kube-lego:tag.svg", n: 1},
		{s: `blah blah [![](https://images.microbadger.com/badges/version/jetstack/kube-lego:tag.svg)](http://microbadger.com/images/jetstack/kube-lego "Get your own image badge on microbadger.com") blah <a href="http://microbadger.com/images/jetstack/kube-lego" title="Get your own version badge on microbadger.com"><img src="https://images.microbadger.com/badges/license/jetstack/kube-lego:1.4.svg"></a> bladibalh`, n: 2},
		{s: "https://images.microbadger.com/badges/commit/j3tst4ck/kub3-l3g0:tag.svg", n: 1},
		{s: "https://images.microbadger.com/badges/license/j3tst4ck/kub3-l3g0:tag.svg", n: 1},
		// a link, but not a badge
		{s: "https://images.microbadger.com/image/alpine.svg", n: 0},
	}

	for id, tt := range tests {
		if n := BadgesInstalled(tt.s); n != tt.n {
			t.Errorf("#%d Unexpected count %d", id, n)
		}
	}
}
