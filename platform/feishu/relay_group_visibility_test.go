package feishu

import (
	"testing"
)

// TestPlatform_RelayGroupVisibilityKey pins the contract feishu's
// Platform satisfies for core.RelayGroupVisibilityTarget: only feishu
// session keys whose third segment carries a non-empty "root:..." or
// "thread:..." prefix turn into a thread-scoped visibility key; every
// other shape (bare-user keys, short keys, empty tails, or anything
// from a foreign platform) returns ("", false) so core falls back to
// its legacy "<platform>:<chatID>:relay" default.
func TestPlatform_RelayGroupVisibilityKey(t *testing.T) {
	p := &Platform{}

	cases := []struct {
		name       string
		sessionKey string
		wantKey    string
		wantOK     bool
	}{
		// ── feishu thread shapes (hits) ──────────────────────────
		{"feishu root", "feishu:oc_chat:root:om_msg", "feishu:oc_chat:root:om_msg", true},
		{"feishu thread", "feishu:oc_chat:thread:omt_thr", "feishu:oc_chat:thread:omt_thr", true},

		// ── feishu non-thread shapes (misses) ────────────────────
		{"feishu bare user", "feishu:oc_chat:ou_user", "", false},
		{"feishu empty root tail", "feishu:oc_chat:root:", "", false},
		{"feishu empty thread tail", "feishu:oc_chat:thread:", "", false},
		{"feishu only two parts", "feishu:oc_chat", "", false},

		// ── non-Feishu adapters must miss even if shape coincides ─
		{"external t prefix", "external:C123:t:1717000000.000100", "", false},
		{"external bare user", "external:C123:U456", "", false},
		{"external numeric", "external:-100123:456:789", "", false},
		{"external type tag", "external:g:cid123:staff42", "", false},
		{"external room id", "external:!room:server.tld:@alice:server.tld", "", false},

		// ── adversarial: external adapter with feishu-looking 3rd
		//   segment must still miss ───────────────────────────────
		{"external with root prefix", "external:C123:root:fake", "", false},

		// ── degenerate inputs ────────────────────────────────────
		{"empty string", "", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotKey, gotOK := p.RelayGroupVisibilityKey(c.sessionKey)
			if gotKey != c.wantKey || gotOK != c.wantOK {
				t.Fatalf("RelayGroupVisibilityKey(%q) = (%q, %v), want (%q, %v)",
					c.sessionKey, gotKey, gotOK, c.wantKey, c.wantOK)
			}
		})
	}
}
