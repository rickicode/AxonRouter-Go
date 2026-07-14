package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/service"
)

func TestProgram_StartRunsRunInGoroutine(t *testing.T) {
	started := make(chan struct{})
	runDone := make(chan struct{})

	p := &service.Program{
		Run: func() error {
			close(started)
			<-runDone
			return nil
		},
		Shutdown: func() error { return nil },
	}

	if err := p.Start(nil); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Run was not started in a goroutine")
	}

	close(runDone)
}

func TestProgram_StopCallsShutdown(t *testing.T) {
	called := make(chan struct{})
	p := &service.Program{
		Shutdown: func() error {
			close(called)
			return nil
		},
	}

	if err := p.Stop(nil); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("Shutdown was not called")
	}
}

func TestProgram_StopReturnsShutdownError(t *testing.T) {
	want := errors.New("shutdown failed")
	p := &service.Program{
		Shutdown: func() error { return want },
	}

	if got := p.Stop(nil); !errors.Is(got, want) {
		t.Fatalf("Stop error = %v, want %v", got, want)
	}
}

func TestServiceConfigHasRequiredFields(t *testing.T) {
	cfg, err := service.ServiceConfig(false)
	if err != nil {
		t.Fatalf("ServiceConfig returned error: %v", err)
	}
	if cfg.Name == "" {
		t.Fatal("service Name is required")
	}
	if cfg.DisplayName == "" {
		t.Fatal("service DisplayName is required")
	}
	if cfg.Description == "" {
		t.Fatal("service Description is required")
	}
	if cfg.Executable == "" {
		t.Fatal("service Executable is required")
	}
}
