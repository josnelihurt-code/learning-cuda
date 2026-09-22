# Generic Use Case Pattern

This document explains the generic `UseCase[Input, Output]` contract defined by this package and the architectural decisions behind it.

## What is the Generic Use Case Pattern?

The generic use case pattern is a simple, consistent interface for all application use cases. The canonical, exported definition lives in [`use_case.go`](use_case.go):

```go
type UseCase[Input any, Output any] interface {
    Execute(ctx context.Context, input Input) (Output, error)
}
```

Every use case in the application implements this single-method interface with:
- A generic `Input` type that defines the use case's dependencies
- A generic `Output` type that defines the use case's results
- A single `Execute` method that encapsulates the use case logic

## Why This Pattern?

### 1. **Consistency and Predictability**
All use cases follow the same structure, making the codebase easier to navigate and understand. When you see a use case, you immediately know:
- How to invoke it: `Execute(ctx, input)`
- What it returns: `(Output, error)`
- That it accepts a context for cancellation/tracing

### 2. **Type Safety and Compile-Time Guarantees**
The generic parameters ensure that:
- Input types are explicitly defined as structs (no loose parameters)
- Output types are explicitly defined as structs (no loose returns)
- Mismatches are caught at compile time, not runtime

### 3. **Testability**
Each use case can be easily mocked for testing:
```go
type MockProcessImageUseCase struct {}

func (m *MockProcessImageUseCase) Execute(
    ctx context.Context, 
    input ProcessImageUseCaseInput,
) (ProcessImageUseCaseOutput, error) {
    // Mock implementation
}
```

### 4. **Composability**
Use cases can be composed and chained:
```go
func (s *Service) ProcessAndSave(ctx context.Context, img Image) error {
    result, err := s.processUC.Execute(ctx, ProcessImageUseCaseInput{Image: img})
    if err != nil {
        return err
    }
    
    _, err = s.saveUC.Execute(ctx, SaveImageUseCaseInput{Image: result.Image})
    return err
}
```

## One Canonical Contract

Earlier revisions declared a private `useCase` copy in each consuming package (`pkg/app`, `pkg/container`, `pkg/interfaces/connectrpc`). The copies were identical and bought no decoupling — every consumer already depends on this package for the input/output types — so they were pure drift risk. The contract now lives once, in the exported `UseCase` type ([`use_case.go`](use_case.go)), and all layers reference `application.UseCase[...]`.

Consumers owning the interfaces they accept ([Rob Pike, "Accept Interfaces, Return Structs"](https://go.dev/doc/effective_go#interfaces_and_types)) still holds for the repository ports (e.g. `FeatureFlagEvaluator` and `FeatureFlagAdmin` in `pkg/application/flags`). The use-case contract's natural owner is the application layer itself, which defines every input and output type, so a single exported interface is the simplest drift-free option.

### The Go Philosophy

From [Rob Pike's "Go Proverbs"](https://go-proverbs.github.io/):
- **"Don't communicate by sharing memory; share memory by communicating."**
- **"The bigger the interface, the weaker the abstraction."**
- **"Accept interfaces, return structs."**

The generic use case pattern embodies these principles:
- Small, focused interfaces (one method)
- Each layer accepts the interface it needs
- Use cases return concrete output structs, not interfaces

## Comparison: Before and After

### Before: Two-Method Interface

```go
// Direct protobuf dependency, breaks pattern
type streamVideoUseCase interface {
    Start(ctx context.Context, req *pb.StartVideoPlaybackRequest) (*pb.StartVideoPlaybackResponse, error)
    Stop(ctx context.Context, req *pb.StopVideoPlaybackRequest) (*pb.StopVideoPlaybackResponse, error)
}
```

**Problems:**
- Inconsistent with other use cases (two methods vs one)
- Direct protobuf dependency couples application layer to transport layer
- Can't use generic wiring in container/app

### After: Generic Pattern

> **Note:** The streaming use case shown below (`StartVideoPlaybackUseCase` / `VideoSessionManager`) was removed in v4.6.0 when video playback support was dropped. The snippet is kept as an illustrative example of the pattern only.

```go
// Application layer: clean domain types
type StartVideoPlaybackUseCaseInput struct {
    VideoID   string
    SessionID string
    Filters   []domain.FilterType
    // ... other domain types
}

type StartVideoPlaybackUseCaseOutput struct {
    Code      int32
    Message   string
    SessionID string
    // ... other fields
}

type StartVideoPlaybackUseCase struct {
    sessionManager  *VideoSessionManager
    videoRepository videoRepository
    // ... dependencies
}

func (uc *StartVideoPlaybackUseCase) Execute(
    ctx context.Context,
    input StartVideoPlaybackUseCaseInput,
) (StartVideoPlaybackUseCaseOutput, error) {
    // Use case logic
}

// Handler layer: converts protobuf to domain types
func (h *Handler) StartVideoPlayback(ctx context.Context, req *pb.StartVideoPlaybackRequest) (*pb.StartVideoPlaybackResponse, error) {
    input := h.toUseCaseInput(req)  // Protobuf -> Domain
    result, err := h.uc.Execute(ctx, input)
    if err != nil {
        return nil, err
    }
    return h.toProtobufResponse(result), nil  // Domain -> Protobuf
}
```

**Benefits:**
- Consistent with all other use cases
- Application layer uses domain types (protobuf is a transport detail)
- Generic wiring works everywhere
- Clear separation of concerns

## Input/Output DTO Guidelines

### Input DTOs should:
- Be plain structs with public fields
- Contain only domain types (not protobuf/gRPC types)
- Validate required fields in the use case, not the struct
- Be passed by value (cheap to copy)

### Output DTOs should:
- Be plain structs with public fields
- Contain the results of the use case
- Include error information in the error return, not the struct
- Be passed by value

## References

- [Effective Go: Interfaces and Types](https://go.dev/doc/effective_go#interfaces_and_types)
- [Rob Pike: "Accept Interfaces, Return Structs" (Gopherfest 2015)](https://www.youtube.com/watch?v=yyyzjYA72SM)
- [Go Proverbs](https://go-proverbs.github.io/)
- [Dave Cheney: "Don't just check errors, handle them gracefully"](https://dave.cheney.net/2016/04/27/dont-just-check-errors-handle-them-gracefully)
