package limits

import (
	"context"
	"time"

	"golang.org/x/sync/semaphore"

	"product-collection-form/backend/internal/apierror"
	"product-collection-form/backend/internal/config"
)

type Gate struct {
	sem        *semaphore.Weighted
	timeout    time.Duration
	timeoutErr error
}

type Manager struct {
	Upload      Gate
	PDFRebuild  Gate
	YearExport  Gate
	Bundle      Gate
	ImageDecode Gate
}

func New(cfg config.Concurrency) *Manager {
	timeout := time.Duration(cfg.AcquireTimeoutSeconds) * time.Second
	return &Manager{
		Upload: Gate{
			sem:        semaphore.NewWeighted(int64(cfg.MaxUploads)),
			timeout:    timeout,
			timeoutErr: apierror.ErrServerBusy,
		},
		PDFRebuild: Gate{
			sem:        semaphore.NewWeighted(int64(cfg.MaxPDFRebuilds)),
			timeout:    timeout,
			timeoutErr: apierror.ErrServerBusy,
		},
		YearExport: Gate{
			sem:        semaphore.NewWeighted(int64(cfg.MaxYearExports)),
			timeout:    timeout,
			timeoutErr: apierror.ErrRateLimited,
		},
		Bundle: Gate{
			sem:        semaphore.NewWeighted(int64(cfg.MaxBundleExports)),
			timeout:    timeout,
			timeoutErr: apierror.ErrServerBusy,
		},
		ImageDecode: Gate{
			sem: semaphore.NewWeighted(int64(cfg.MaxImageDecodes)),
		},
	}
}

func (g Gate) Acquire(ctx context.Context) (func(), error) {
	acquireCtx := ctx
	cancel := func() {}
	if g.timeout > 0 {
		acquireCtx, cancel = context.WithTimeout(ctx, g.timeout)
	}
	if err := g.sem.Acquire(acquireCtx, 1); err != nil {
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if g.timeoutErr != nil {
			return nil, g.timeoutErr
		}
		return nil, err
	}
	return func() {
		cancel()
		g.sem.Release(1)
	}, nil
}
