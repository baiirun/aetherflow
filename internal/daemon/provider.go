package daemon

import "context"

// ProviderRuntimeStatus is the normalized runtime state across providers.
type ProviderRuntimeStatus string

const (
	ProviderRuntimeUnknown    ProviderRuntimeStatus = "unknown"
	ProviderRuntimeRunning    ProviderRuntimeStatus = "running"
	ProviderRuntimeWarm       ProviderRuntimeStatus = "warm"
	ProviderRuntimeCold       ProviderRuntimeStatus = "cold"
	ProviderRuntimeTerminated ProviderRuntimeStatus = "terminated"
)

// ProviderCreateRequest is the normalized create request used by daemon flows.
type ProviderCreateRequest struct {
	SpawnID   string
	RequestID string
	Project   string
	Prompt    string
}

// ProviderCreateResult is the normalized create response used by daemon flows.
type ProviderCreateResult struct {
	SandboxID     string
	CanonicalName string
	OperationID   string
	AttachRef     string
}

// ProviderStatusResult returns normalized status and optional endpoint hints.
type ProviderStatusResult struct {
	Status    ProviderRuntimeStatus
	AttachRef string
}

// Provider is a narrow daemon-facing provider boundary.
type Provider interface {
	Create(ctx context.Context, req ProviderCreateRequest) (ProviderCreateResult, error)
	GetStatus(ctx context.Context, sandboxID string) (ProviderStatusResult, error)
	Terminate(ctx context.Context, sandboxID string) error
}
