# Generic Use Case Pattern

This document explains the generic `useCase[Input, Output]` pattern used throughout this codebase and the architectural decisions behind it.

## What is the Generic Use Case Pattern?

The generic use case pattern is a simple, consistent interface for all application use cases:

```go
type useCase[Input any, Output any] interface {
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

## Interface Duplication vs. Coupling

You may notice that the `useCase` interface is defined in multiple packages:
- `pkg/app/use_cases.go`
- `pkg/container/use_cases.go`
- `pkg/interfaces/connectrpc/use_cases.go`

**This is intentional.**

### Why Duplicate Interfaces Instead of Importing?

Following Go best practices and [Rob Pike's advice on "Accept Interfaces, Return Structs"](https://go.dev/doc/effective_go#interfaces_and_types), each layer defines its own interface rather than importing from another layer.

This provides several benefits:

1. **Decoupling**: The `app` package doesn't depend on the `container` package, and vice versa. Each owns its own abstractions.

2. **Independence**: Layers can evolve independently. If the container needs additional functionality, it doesn't affect the app layer's interface.

3. **Clear Ownership**: Each package defines what it needs from its dependencies, not what dependencies export.

4. **No Package Cycles**: Defining interfaces locally prevents import cycles where package A imports B, B imports C, and C imports A.

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
