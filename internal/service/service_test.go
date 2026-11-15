package service

import (
	"testing"

	"kestrix/internal/domain"
)

// newTestRunService creates a RunService configured for tests with
// allowLocal = true, permitting execution in local mode without being rejected
// by CheckLocalAllowed. Optional modifier functions can further customize
// the service.
func newTestRunService(runStore domain.RunStore, pipeline domain.PipelineRunner, taskRunner domain.TaskRunner, opts ...func(*RunService)) *RunService {
	svc := NewRunService(runStore, pipeline, taskRunner)
	svc.SetLocalExecutionPolicy(true, "local")
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// newTestRunServiceWithLocalAllowed is an alias for newTestRunService,
// explicitly noting the AllowLocal policy configuration.
func newTestRunServiceWithLocalAllowed(runStore domain.RunStore, pipeline domain.PipelineRunner, taskRunner domain.TaskRunner, opts ...func(*RunService)) *RunService {
	return newTestRunService(runStore, pipeline, taskRunner, opts...)
}

func TestNewTestRunService_CompilesAndInitializes(t *testing.T) {
	svc := newTestRunService(nil, nil, nil)
	if err := svc.CheckLocalAllowed("local"); err != nil {
		t.Fatalf("expected local to be allowed by test helper, got: %v", err)
	}
	svc2 := newTestRunServiceWithLocalAllowed(nil, nil, nil)
	if err := svc2.CheckLocalAllowed("local"); err != nil {
		t.Fatalf("expected local to be allowed by test helper, got: %v", err)
	}
}
