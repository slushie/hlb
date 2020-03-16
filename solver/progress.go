package solver

import (
	"context"
	"os"

	"github.com/docker/buildx/util/progress"
	"golang.org/x/sync/errgroup"
)

type ProgressOption func(*ProgressInfo) error

type ProgressInfo struct {
	LogOutput LogOutput
}

type LogOutput int

const (
	LogOutputTTY LogOutput = iota
	LogOutputPlain
	LogOutputJSON
	LogOutputRaw
)

func WithLogOutput(logOutput LogOutput) ProgressOption {
	return func(info *ProgressInfo) error {
		info.LogOutput = logOutput
		return nil
	}
}

func NewProgress(ctx context.Context, opts ...ProgressOption) (*Progress, error) {
	info := &ProgressInfo{}
	for _, opt := range opts {
		err := opt(info)
		if err != nil {
			return nil, err
		}
	}

	// Not using shared context to not disrupt display on errors, and allow
	// graceful exit and report error.
	pctx, cancel := context.WithCancel(context.Background())

	var pw progress.Writer

	switch info.LogOutput {
	case LogOutputTTY:
		pw = progress.NewPrinter(pctx, os.Stderr, "tty")
	case LogOutputPlain:
		pw = progress.NewPrinter(pctx, os.Stderr, "plain")
	case LogOutputJSON, LogOutputRaw:
		panic("unimplemented")
		// return StreamSolveStatus(ctx, info.LogOutput, os.Stdout, ch)
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		// Only after pw.Done is unblocked can we cleanly cancel the one-off context
		// passed to the progress printer.
		defer cancel()

		// After *Progress is released, there is still a display rate on the progress
		// UI, so we must ensure the root progress.Writer is done, which indicates it
		// is completely finished writing.
		<-pw.Done()
		return pw.Err()
	})

	mw := progress.NewMultiWriter(pw)
	done := make(chan struct{})

	// While using *Progress, there may be gaps between solves. So to ensure the
	// build is not finished, we create a progress writer that remains unfinished
	// until *Progress is released by the user to indicate they are really done.
	g.Go(func() error {
		final := mw.WithPrefix("progress", false)
		defer close(final.Status())
		<-done
		return nil
	})

	return &Progress{mw, ctx, g, done}, nil
}

type Progress struct {
	mw   *progress.MultiWriter
	ctx  context.Context
	g    *errgroup.Group
	done chan struct{}
}

func (p *Progress) MultiWriter() *progress.MultiWriter {
	return p.mw
}

func (p *Progress) Go(fn func() error) {
	p.g.Go(fn)
}

func (p *Progress) WithPrefix(pfx string, fn func(ctx context.Context, pw progress.Writer) error) {
	pw := p.mw.WithPrefix(pfx, false)
	p.g.Go(func() error {
		<-pw.Done()
		return pw.Err()
	})

	p.g.Go(func() error {
		return fn(p.ctx, pw)
	})
}

func (p *Progress) Release() {
	close(p.done)
}

func (p *Progress) Wait() error {
	return p.g.Wait()
}