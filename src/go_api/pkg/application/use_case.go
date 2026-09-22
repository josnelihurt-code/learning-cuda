package application

import "context"

// UseCase is the canonical use-case contract shared by all layers, so the
// interface cannot drift between per-package copies.
type UseCase[Input any, Output any] interface {
	Execute(ctx context.Context, input Input) (Output, error)
}
