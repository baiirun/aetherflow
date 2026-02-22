package daemon

// ProviderCreateRequest is the normalized create request used by daemon flows.
type ProviderCreateRequest struct {
	SpawnID   string
	RequestID string
	Project   string
	Prompt    string
}

// ProviderCreateResult is the normalized create response used by daemon flows.
type ProviderCreateResult struct {
	SandboxID   string
	OperationID string
	AttachRef   string
}
