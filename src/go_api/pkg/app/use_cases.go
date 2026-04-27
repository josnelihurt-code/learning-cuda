package app

import (
	"context"
)

// useCase defines the generic interface for all application use cases.
//
// This pattern provides consistency, type safety, and testability across the codebase.
// Each layer (app, container, interfaces) defines its own useCase interface rather than
// importing from another layer, following Go best practices for decoupling.
//
// See: https://github.com/jrb/cuda-learning/src/go_api/pkg/application/README.md
type useCase[Input any, Output any] interface {
	Execute(ctx context.Context, input Input) (Output, error)
}
