package lifecycle

import (
	"context"
	"sync"
)

type Service interface {
	Run(ctx context.Context) error
}

type ServiceFunc func(ctx context.Context) error

func (f ServiceFunc) Run(ctx context.Context) error {
	return f(ctx)
}

func Run(ctx context.Context, services ...Service) error {
	if len(services) == 0 {
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(services))
	var wg sync.WaitGroup
	wg.Add(len(services))
	for _, service := range services {
		service := service
		go func() {
			defer wg.Done()
			if err := service.Run(runCtx); err != nil {
				errCh <- err
				cancel()
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case err := <-errCh:
		cancel()
		<-done
		return err
	case <-ctx.Done():
		cancel()
		<-done
		select {
		case err := <-errCh:
			return err
		default:
			return nil
		}
	case <-done:
		select {
		case err := <-errCh:
			return err
		default:
			return nil
		}
	}
}
