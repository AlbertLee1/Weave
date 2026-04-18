package oms

import "testing"

func TestMediaAsset_Validate(t *testing.T) {
	t.Parallel()
	base := MediaAsset{
		RID:       "ri.media.main.asset.abc",
		Realm:     "main",
		MIME:      "image/png",
		SizeBytes: 128,
		SHA256:    "deadbeef",
		Path:      "main/2026/04/deadbeef",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("base row rejected: %v", err)
	}

	cases := []struct {
		name string
		mut  func(a *MediaAsset)
	}{
		{"missing rid", func(a *MediaAsset) { a.RID = "" }},
		{"missing realm", func(a *MediaAsset) { a.Realm = "" }},
		{"missing sha256", func(a *MediaAsset) { a.SHA256 = "" }},
		{"missing path", func(a *MediaAsset) { a.Path = "" }},
		{"negative size", func(a *MediaAsset) { a.SizeBytes = -1 }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := base
			tc.mut(&a)
			if err := a.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}
