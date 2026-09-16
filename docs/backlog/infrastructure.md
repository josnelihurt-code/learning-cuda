# Infrastructure & DevOps

> **Note: This file is archived for historical reference.**  
> All backlog items in this file have been migrated to GitHub Issues as part of the project's evolution from markdown-based backlog management to structured issue tracking. Each item was grouped with related tasks, and converted into actionable GitHub issues with proper labels, acceptance criteria, and context.
> 
> **Purpose**: This file is preserved to document the initial planning and evolution of the infrastructure aspects of the project. It serves as a historical record of how the project's scope was defined and refined over time.
>
> **Current Status**: All pending items have been converted to GitHub Issues. Completed items remain marked with their original issue numbers (#4-169).
>
> **See**: [GitHub Issues](https://github.com/josnelihurt-code/learning-cuda/issues) for active project management.

Microservices, observability, testing, and cloud deployment.

## gRPC Microservices

### Services to Build
- [x] #4 Image Processing Service (ProcessImage endpoint implemented)
- [x] #5 File Service (ListAvailableImages, UploadImage implemented)
- ~~#507 StreamProcessVideo implementation (defined but returns Unimplemented)~~ (obsolete — the `StreamProcessVideo` RPC is no longer defined in any proto; superseded by `SignalingStream` in `proto/webrtc_signal.proto`)
- [ ] #524 Video Management Service (ListVideos, StreamVideo, Upload)
- [ ] #524 Model Inference Service (Predict, StreamInference)

### Connect-RPC with Vanguard
- [x] #6 Implemented Connect-RPC server
- [x] #7 Integrated Vanguard for RESTful API transcoding
- [x] #8 HTTP annotations in proto (google.api.http)
- [x] #9 RESTful endpoints: GET /api/v1/images, POST /api/v1/images/upload, etc.
- [x] #10 Dual protocol support: REST + Connect + gRPC in single server
- [x] #11 Native HTTP/JSON support without gateway overhead
- [x] #507 Implement bidirectional streaming for video (via `SignalingStream` in `proto/webrtc_signal.proto`)
- [ ] #524 Add Connect-Web for browser clients

### Infrastructure
- [x] #12 Dynamic library loading with dlopen (plugin architecture)
- [x] #13 Processor versioning and API compatibility checks
- [x] #14 Processor registry with capability discovery
- [x] #15 API 2.0.0 with FilterDefinition and FilterParameter metadata
- [x] #16 Dynamic filter parameter discovery (type, options, defaults)
- [x] #17 Split Go build system (Makefile) from C++ (Bazel)
- [ ] #525 Service discovery (Consul optional)
- [ ] #525 Load balancing (round-robin, retry, circuit breaker)
- [x] #18 Clean arch: domain → application → infrastructure → interfaces
- [ ] #525 Circuit breaker for Flipt calls with fallback
- [ ] #526 Complete DI container (move processor loader from main.go)
- [ ] #525 Connection pooling monitoring and metrics
- [ ] #526 Plugin hot-reloading without server restart
- [ ] #526 Multiple processor plugins loaded simultaneously

## Observability

### Jaeger (Distributed Tracing)
- [x] #19 Add Jaeger to docker-compose
- [x] #20 OpenTelemetry SDK for Go
- [x] #21 OpenTelemetry SDK for frontend (browser)
- [x] #22 Distributed tracing from browser to backend
- [x] #23 Trace context propagation with W3C headers
- [ ] #505 Trace WebSocket, CGO calls, CUDA kernels
- [ ] #505 Span attributes: width, height, filter type
- [ ] #505 Enhanced trace attributes (user-agent, IP, request size)
- [ ] #505 Baggage for cross-service context
- [ ] #505 Intelligent trace sampling based on load

### Prometheus (Metrics)
- [ ] #503 Add Prometheus to docker-compose
- [ ] #503 Expose /metrics endpoint
- [ ] #503 Custom metrics: frames_processed, duration, connections
- [ ] #503 GPU metrics exporter
- [ ] #503 Processing latency histogram by accelerator and filter
- [ ] #503 Error counters by type and handler
- [ ] #503 Active WebSocket connections gauge

### Grafana (Dashboards)
- [ ] #504 Connect Prometheus + Jaeger
- [ ] #504 Dashboard 1: FPS, latency, GPU vs CPU
- [ ] #504 Dashboard 2: GPU utilization, memory, temp
- [ ] #504 Dashboard 3: Request rate, errors, latency
- [ ] #506 Alerts: high errors, latency, GPU temp

### Feature Flags
- [x] #24 Integrate Flipt for feature flag management
- [x] #25 Automatic flag synchronization from YAML to Flipt
- [x] #26 REST API endpoint for manual flag sync
- [x] #27 Web UI integration with sync button
- [x] #28 Fallback to YAML when Flipt unavailable
- [x] #29 Feature flags modal with Flipt iframe integration
- [x] #30 Production environment feature flag management
- [ ] Advanced flag rules and targeting

### Logging
- [x] #31 spdlog (C++)
- [x] #32 Health endpoint at /health with JSON response
- [x] #33 Add trace_id to logs
- [x] #34 Replace Go log package with structured logging (zerolog)
- [x] #35 Loki stack deployed (Loki + Promtail + Grafana)
- [x] #36 Trace-to-Logs correlation in Grafana
- [x] #37 Consistent log levels (debug/info/warn/error)
- [x] #38 Multi-line log parsing in Promtail
- [x] #39 Grafana dashboard and datasource provisioning
- [x] #40 Unified logging configuration for dev and prod environments
- [ ] #528 Add request correlation IDs across services
- [ ] #528 Detailed health checks with dependencies (readiness vs liveness)

### File Upload Service
- [x] #41 Create FileService proto with upload and list operations
- [x] #42 Implement UploadImage RPC with file validation
- [x] #43 PNG format validation (magic number check)
- [x] #44 File size limit enforcement (10MB max)
- [x] #45 Save uploaded files to static_images directory
- [x] #46 Drag-and-drop UI component with progress indicator
- [x] #47 Frontend file validation before upload
- [x] #48 BDD tests for upload scenarios (4 scenarios)
- [x] #49 Unit tests for upload use case (6 test cases)
- [x] #50 E2E tests for upload workflow (6 tests)
- [x] #51 OpenTelemetry tracing for upload operations
- [ ] #527 Support additional image formats (JPEG, WebP)
- [ ] #527 Image compression before storage
- [ ] #527 Thumbnail generation on upload
- [ ] #527 Image metadata extraction (dimensions, format)
- [ ] #527 Upload progress tracking with streaming
- [ ] #527 Batch upload support for multiple files
- [ ] #527 Image preview before upload confirmation

## Load Testing & BDD

### Git Hooks & CI
- [x] #52 Create pre-commit hook (C++ tests, Go tests, linters, frontend build)
- [x] #53 Modularize pre-commit into layer-specific scripts (6 scripts)
- [x] #54 Create pre-commit-cpp.sh, pre-commit-go.sh, pre-commit-frontend.sh
- [x] #55 Create pre-commit-lint-cpp.sh, pre-commit-lint-go.sh, pre-commit-lint-frontend.sh
- [x] #56 Create pre-push hook (full validation with all browsers)
- [x] #57 Add hooks installation script (`scripts/hooks/install.sh`)
- [x] #58 Document git hooks in README
- [x] #59 Configure golangci-lint with gosec exceptions and govet tweaks
- [ ] Add commit-msg hook for conventional commits validation
- [ ] Add prepare-commit-msg for commit message templates
- [ ] Integrate hooks with CI/CD pipeline
- [ ] Add hooks performance metrics and timing

### Unit Tests
- [x] #60 Setup test coverage reporting infrastructure
- [x] #61 Add unit tests for ProcessImageUseCase with mocks (12 test cases, 211 lines)
- [x] #62 Add unit tests for EvaluateFeatureFlagUseCase (7 test cases, 240 lines)
- [x] #63 Add unit tests for GetStreamConfigUseCase (3 test cases, 113 lines)
- [x] #64 Add unit tests for ListInputsUseCase (2 test cases, 87 lines)
- [x] #65 Add unit tests for SyncFeatureFlagsUseCase (3 test cases, 110 lines)
- [x] #66 Add unit tests for ListAvailableImagesUseCase (2 test cases, 96 lines)
- [x] #67 Add unit tests for StaticImageRepository infrastructure (2 test cases, 99 lines)
- [x] #68 Add unit tests for ToastContainer frontend component (8 test suites, 199 lines)
- [x] #69 Add unit tests for ImageSelectorModal frontend component (8 test suites, 128 lines)
- [x] #70 Create mocks for FeatureFlagRepository interface
- [x] #71 Create mocks for ImageProcessor interface
- [x] #72 Create mocks for StaticImageRepository interface
- [x] #73 Total application layer coverage: 6 use cases tested (29+ test cases, 857 lines)
- [x] #74 Total infrastructure layer coverage: 1 repository tested (99 lines)
- [x] Add unit tests for ConnectRPC handlers (config, file, webrtc)
- [ ] Add unit tests for HTTP handlers
- [x] Add unit tests for repositories (GOFF — `goff_repository_test.go` in `src/go_api/pkg/infrastructure/featureflags/`)
- [ ] Add unit tests for remaining infrastructure services
- [ ] Mock all remaining domain interfaces

### K6 Load Tests
- [ ] Single user baseline (30 FPS for 30s)
- [ ] Concurrent users (ramp to 100, sustain, ramp down)
- [ ] Stress test (find breaking point)
- [ ] gRPC tests with `ghz`

### Godog BDD
- [x] #75 Setup Godog + feature files
- [x] #76 Feature: Image processing (14 scenarios)
- [x] #77 Feature: WebSocket processing (4 scenarios)
- [x] #78 Feature: Streaming service (1 scenario)
- [x] #79 Feature: Feature flags (5 scenarios)
- [x] #80 Feature: Input sources (3 scenarios)
- [x] #81 Feature: Processor capabilities (4 scenarios)
- [x] #82 Feature: Tools configuration (6 scenarios)
- [x] #83 Feature: Available images (3 scenarios)
- [x] #84 Step definitions in Go
- [x] #85 CI integration with dockerized tests
- [x] #86 Cucumber HTML reports at port 5050
- [x] #87 40 scenarios passing with dynamic filter, tools, and image selection validation

### Playwright E2E Tests (Frontend)
- [x] #88 Setup Playwright with TypeScript
- [x] #89 Configure multi-browser testing (Chrome, Firefox, WebKit)
- [x] #90 Test: Drawer functionality (4 tests)
- [x] #91 Test: Filter configuration (5 tests)
- [x] #92 Test: Filter toggle behavior (4 tests)
- [x] #93 Test: Multi-source management (4 tests)
- [x] #94 Test: Panel synchronization (5 tests)
- [x] #95 Test: Resolution control (3 tests)
- [x] #96 Test: Source removal (2 tests)
- [x] #97 Test: UI validation (8 tests)
- [x] #98 Test: WebSocket management (2 tests)
- [x] #99 Test: Image selection (8 tests)
- [x] #100 Create reusable test helpers
- [x] #101 Add data-testid attributes to components
- [x] #102 Docker integration for CI/CD
- [x] #103 Test artifacts and HTML reports
- [x] #104 Total: 10 test suites with 45 E2E tests
- [ ] Visual regression testing with screenshots
- [ ] Performance testing with Lighthouse
- [ ] Accessibility testing with axe-core
- [ ] Mobile viewport testing

## Cloud Deployment

### Staging Environment
- [x] #105 Localhost staging environment with docker-compose.staging.yml (Oct 31, 2025)
- [x] #106 Traefik reverse proxy for staging with HTTPS auto-redirect
- [x] #107 Staging-local deployment scripts (start.sh, stop.sh, clean.sh)
- [x] #108 Services accessible via .localhost domains
- [x] #109 Production-like Docker deployment for local testing
- [x] #110 Staging configuration file (config.staging.yaml)
- [x] #111 Updated to use pre-built images from GHCR (Nov 2, 2025)
- [x] #112 Removed local Docker build requirement for staging
- [x] #113 Staging now tests images compiled in GitHub Actions CI/CD

### Jetson Nano Production Deployment
- [x] #114 Deploy application on Jetson Nano hardware
- [x] #116 Modular Ansible deployment system
- [x] #117 Production-optimized Docker configuration with CUDA separation
- [x] #118 Environment-specific configuration management
- [x] #119 Automated code synchronization from development to production
- [x] #120 Production deployment warning banner (refactored to information banner)
- [x] #121 Unified logging configuration for dev and prod environments
- [x] #122 Production URLs: cuda-demo.lab.josnelihurt.me, grafana-cuda-demo.josnelihurt.me, etc.
- [x] #123 Camera preview fixes with video grid filter application (Oct 26, 2025)

### Provider Research
- [ ] Vultr (A40, A100 pricing)
- [ ] Lambda Labs, Paperspace, AWS P3/G4, GCP
- [ ] Create comparison doc: cost, availability, latency
- [ ] Document choice with reasoning

### Terraform
- [ ] Setup IaC for cloud resources
- [ ] Cloud-init: Docker, NVIDIA toolkit, SSL
- [ ] State management (Terraform Cloud or S3)

### CI/CD
- [x] #124 GitHub Actions workflow for Docker multi-arch builds (Nov 2, 2025)
- [x] #125 Docker images published to GitHub Container Registry (GHCR)
- [x] #126 Automated builds on push to main branch
- [x] #127 Support for AMD64 and ARM64 architectures
- [x] #128 Staging environment uses pre-built GHCR images
- [ ] Auto deploy on merge to main
- [ ] Deployment strategy: blue-green, rolling, or canary

### Production Monitoring
- [ ] Cloud monitoring (CloudWatch/Stackdriver)
- [ ] Alerts (PagerDuty, Slack)
- [ ] Log aggregation

### Security
- [ ] #529 Secrets management (Vault, AWS Secrets Manager)
- [x] #129 TLS for HTTP server (native Go implementation)
- [ ] #529 TLS for gRPC
- [ ] #529 Auth (JWT, OAuth)
- [ ] #529 Rate limiting
- ~~#529 Fix WebSocket CORS validation (currently accepts all origins)~~ (obsolete — WebSocket transport removed in favor of WebRTC)
- [ ] #529 Input validation layer (image size limits, request validation)
- [ ] #529 Custom domain errors with proper error codes

## Code Quality

### Linters & Formatters
- [x] #131 Configure clang-tidy for C++ with 50+ code quality checks
- [x] #132 Configure golangci-lint with 20+ linters for Go
- [x] #133 Configure ESLint + Prettier for TypeScript/React frontend
- [x] #134 Create unified linters script `scripts/test/linters.sh` (supports --fix mode)
- [x] #135 Add Docker services for all linters (lint-frontend, lint-golang, lint-cpp)
- [x] #136 Enhanced .clang-format with comprehensive formatting rules
- [x] #137 Configure linter exclusions for tests, proto, and generated code
- [x] Pre-commit hooks for automatic linting (`scripts/hooks/pre-commit.sh` runs the C++/Go/frontend/language lint scripts)
- [ ] CI/CD integration for linting on pull requests

### Testing & Coverage
- [x] #138 Setup Vitest for frontend unit tests with coverage
- [x] #139 Create coverage script `scripts/test/coverage.sh` for all test coverage
- [x] #140 Add coverage-report-viewer Docker service (port 5052)
- [x] #141 Configure coverage thresholds (80% for all metrics)
- [x] #142 Implement Go unit tests for ProcessImageUseCase (12 test cases)
- [x] #143 Implement frontend unit tests for ToastContainer (8 test suites)
- [x] #144 Coverage reports: Frontend (HTML/JSON/LCOV), Go (HTML), C++ (LCOV)
- [ ] Increase Go test coverage to 80%
- [ ] Add unit tests for all use cases
- [ ] Add unit tests for all handlers
- [ ] Add unit tests for repositories
- [ ] Integration tests for processor plugins

### Go Backend
- [x] #145 Extract processor loading logic into separate loader package
- [x] #146 Implement version compatibility validation
- [x] #147 Add proper error handling for plugin lifecycle
- [x] #148 Refactor Viper configuration to use mapstructure tags
- [x] #149 Add environment variable for mock mode in testing
- [x] #150 Simplify config initialization with automatic unmarshaling
- [ ] Add godoc documentation for all exported types/functions
- [ ] Extract magic numbers to constants (timeouts, intervals, buffer sizes)
- [ ] Refactor long functions
- [ ] Add validation DTOs (separate from domain models)
- [ ] Error handling middleware for HTTP/gRPC
- [ ] Add tools.go for build dependencies versioning
- [ ] Configuration validation at startup (fail-fast)
- [ ] Organize config files by environment (dev, prod, test)

### Input Sources
- [x] #151 Load static images dynamically from /data directory
- [x] #152 Implement StaticImageRepository with filesystem scanning
- [x] #153 Support .png, .jpg, .jpeg image formats
- [x] #154 Add UI for selecting and changing static images
- [x] #155 ListAvailableImages endpoint with image metadata
- [ ] Support multiple server-side cameras
- [ ] Support video files as input sources
- [ ] Support remote stream URLs (RTSP, HLS)
- [x] Implement GetFlag in GoffRepository (`src/go_api/pkg/infrastructure/featureflags/goff_repository.go`)

## Notes

Start with observability first. Keep it simple initially. GPU instances are expensive, monitor costs.

