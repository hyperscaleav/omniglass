package storage

import (
	"context"
	"testing"
)

func TestProvenanceDetailStampsTemplateAndBatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		name  string
		ctx   context.Context
		image any
		want  string
	}{
		{"none", ctx, []byte(`{"a":1}`), `{"a":1}`},
		{"template", WithTemplate(ctx, "t"), []byte(`{"a":1}`), `{"a":1,"from_template":"t"}`},
		{"batch", WithBatch(ctx, "i"), []byte(`{"a":1}`), `{"a":1,"from_batch":"i"}`},
		{"both", WithBatch(WithTemplate(ctx, "t"), "i"), []byte(`{"a":1}`), `{"a":1,"from_batch":"i","from_template":"t"}`},
		{"own template kept", WithBatch(WithTemplate(ctx, "t"), "i"), []byte(`{"from_template":"own"}`), `{"from_batch":"i","from_template":"own"}`},
		{"not an object", WithBatch(ctx, "i"), []byte(`[1]`), `[1]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := withProvenanceDetail(c.ctx, c.image)
			if string(got.([]byte)) != c.want {
				t.Fatalf("detail = %s, want %s", got, c.want)
			}
		})
	}
	if got := withProvenanceDetail(WithBatch(ctx, "i"), nil); got != nil {
		t.Fatalf("a nil image = %v, want nil", got)
	}
	if WithBatch(ctx, "") != ctx {
		t.Fatal("an empty batch id changed the context")
	}
}
