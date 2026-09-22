package application

import "context"

// UseCase is the inbound port used by handlers and the composition root.
type UseCase[Input any, Output any] interface {
	Execute(ctx context.Context, input Input) (Output, error)
}
