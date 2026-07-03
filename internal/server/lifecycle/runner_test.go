package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCancelsServicesFromRootContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	exited := make(chan struct{})

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, ServiceFunc(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(exited)
			return nil
		}))
	}()

	<-started
	cancel()

	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("service did not exit after root context cancellation")
	}
	require.NoError(t, <-errCh)
}

func TestRunReturnsStartupErrorAndCancelsSiblings(t *testing.T) {
	startupErr := errors.New("listen failed")
	siblingExited := make(chan struct{})

	err := Run(context.Background(),
		ServiceFunc(func(_ context.Context) error {
			return startupErr
		}),
		ServiceFunc(func(ctx context.Context) error {
			<-ctx.Done()
			close(siblingExited)
			return nil
		}),
	)

	require.ErrorIs(t, err, startupErr)
	select {
	case <-siblingExited:
	case <-time.After(time.Second):
		t.Fatal("sibling service was not cancelled after startup error")
	}
}

func TestRunWithNoServicesReturnsNil(t *testing.T) {
	assert.NoError(t, Run(context.Background()))
}
