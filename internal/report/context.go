package report

import (
	"context"
	"os"
)

type key struct{}

func With(ctx context.Context, r *Reporter) context.Context {
	return context.WithValue(ctx, key{}, r)
}

func From(ctx context.Context) *Reporter {
	if r, ok := ctx.Value(key{}).(*Reporter); ok {
		return r
	}
	return New(os.Stdout, os.Stderr, nil)
}
