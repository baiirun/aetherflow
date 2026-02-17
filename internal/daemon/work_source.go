package daemon

import "context"

// WorkSource abstracts task/work item operations so session lifecycle code does
// not hard-code a specific backend.
type WorkSource interface {
	Claim(ctx context.Context, workRef, project string) error
	Complete(ctx context.Context, workRef, project string) error
	GetMeta(ctx context.Context, workRef, project string) (TaskMeta, error)
}

// ProgWorkSource is the default WorkSource backed by the prog CLI.
type ProgWorkSource struct {
	runner CommandRunner
}

func NewProgWorkSource(runner CommandRunner) *ProgWorkSource {
	if runner == nil {
		runner = ExecCommandRunner
	}
	return &ProgWorkSource{runner: runner}
}

func (p *ProgWorkSource) Claim(ctx context.Context, workRef, project string) error {
	args := []string{"start", workRef}
	if project != "" {
		args = append(args, "-p", project)
	}
	_, err := p.runner(ctx, "prog", args...)
	return err
}

func (p *ProgWorkSource) Complete(ctx context.Context, workRef, project string) error {
	args := []string{"done", workRef}
	if project != "" {
		args = append(args, "-p", project)
	}
	_, err := p.runner(ctx, "prog", args...)
	return err
}

func (p *ProgWorkSource) GetMeta(ctx context.Context, workRef, project string) (TaskMeta, error) {
	return FetchTaskMeta(ctx, workRef, project, p.runner)
}
