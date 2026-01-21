package players

import (
	"context"
	"log/slog"

	"github.com/Alliance-Community/pr-logs-proxy/internal/services/logs"
	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
)

// internalStreamAdapter adapts server streams to work without network calls
type adminLogStreamAdapter struct {
	service *logs.AdminLogService
	ch      chan *v1.AdminLogEntry
	ctx     context.Context
}

func newAdminLogStream(ctx context.Context, service *logs.AdminLogService) *adminLogStreamAdapter {
	adapter := &adminLogStreamAdapter{
		service: service,
		ch:      make(chan *v1.AdminLogEntry, 100),
		ctx:     ctx,
	}

	// Start streaming in background
	go func() {
		// Create internal server stream
		stream := &adminLogServerStream{
			ch:  adapter.ch,
			ctx: ctx,
		}

		if err := service.AdminLogUpdates(&v1.AdminLogUpdatesRequest{}, stream); err != nil {
			slog.Error("Admin log streaming error", slog.Any("error", err))
		}
	}()

	return adapter
}

func (a *adminLogStreamAdapter) Recv() (*v1.AdminLogEntry, error) {
	entry, ok := <-a.ch
	if !ok {
		return nil, context.Canceled
	}
	return entry, nil
}

type adminLogServerStream struct {
	ch  chan *v1.AdminLogEntry
	ctx context.Context
	v1.AdminLogService_AdminLogUpdatesServer
}

func (s *adminLogServerStream) Send(resp *v1.AdminLogUpdatesResponse) error {
	select {
	case s.ch <- resp.Entry:
		return nil
	case <-s.ctx.Done():
		close(s.ch)
		return s.ctx.Err()
	}
}

func (s *adminLogServerStream) Context() context.Context {
	return s.ctx
}

// Join log stream adapter
type joinLogStreamAdapter struct {
	service *logs.JoinLogService
	ch      chan *v1.JoinLogEntry
	ctx     context.Context
}

func newJoinLogStream(ctx context.Context, service *logs.JoinLogService) *joinLogStreamAdapter {
	adapter := &joinLogStreamAdapter{
		service: service,
		ch:      make(chan *v1.JoinLogEntry, 100),
		ctx:     ctx,
	}

	go func() {
		stream := &joinLogServerStream{
			ch:  adapter.ch,
			ctx: ctx,
		}

		if err := service.JoinLogUpdates(&v1.JoinLogUpdatesRequest{}, stream); err != nil {
			slog.Error("Join log streaming error", slog.Any("error", err))
		}
	}()

	return adapter
}

func (a *joinLogStreamAdapter) Recv() (*v1.JoinLogEntry, error) {
	entry, ok := <-a.ch
	if !ok {
		return nil, context.Canceled
	}
	return entry, nil
}

type joinLogServerStream struct {
	ch  chan *v1.JoinLogEntry
	ctx context.Context
	v1.JoinLogService_JoinLogUpdatesServer
}

func (s *joinLogServerStream) Send(resp *v1.JoinLogUpdatesResponse) error {
	select {
	case s.ch <- resp.Entry:
		return nil
	case <-s.ctx.Done():
		close(s.ch)
		return s.ctx.Err()
	}
}

func (s *joinLogServerStream) Context() context.Context {
	return s.ctx
}

// Player profile stream adapter
type playerProfileStreamAdapter struct {
	service *logs.PlayerProfilesService
	ch      chan *v1.PlayerProfileEntry
	ctx     context.Context
}

func newPlayerProfileStream(ctx context.Context, service *logs.PlayerProfilesService) *playerProfileStreamAdapter {
	adapter := &playerProfileStreamAdapter{
		service: service,
		ch:      make(chan *v1.PlayerProfileEntry, 100),
		ctx:     ctx,
	}

	go func() {
		stream := &playerProfileServerStream{
			ch:  adapter.ch,
			ctx: ctx,
		}

		if err := service.PlayerProfilesUpdates(&v1.PlayerProfilesUpdatesRequest{}, stream); err != nil {
			slog.Error("Player profile streaming error", slog.Any("error", err))
		}
	}()

	return adapter
}

func (a *playerProfileStreamAdapter) Recv() (*v1.PlayerProfileEntry, error) {
	entry, ok := <-a.ch
	if !ok {
		return nil, context.Canceled
	}
	return entry, nil
}

type playerProfileServerStream struct {
	ch  chan *v1.PlayerProfileEntry
	ctx context.Context
	v1.PlayerProfilesService_PlayerProfilesUpdatesServer
}

func (s *playerProfileServerStream) Send(resp *v1.PlayerProfilesUpdatesResponse) error {
	select {
	case s.ch <- resp.Entry:
		return nil
	case <-s.ctx.Done():
		close(s.ch)
		return s.ctx.Err()
	}
}

func (s *playerProfileServerStream) Context() context.Context {
	return s.ctx
}

// Update the streaming methods in PlayerQueryService
func (s *PlayerQueryService) streamAdminLogs(ctx context.Context) {
	stream := newAdminLogStream(ctx, s.adminLogService)

	for {
		entry, err := stream.Recv()
		if err != nil {
			slog.Error("Admin log stream error", slog.Any("error", err))
			return
		}

		s.processAdminLogEntry(entry)
	}
}

func (s *PlayerQueryService) streamJoinLogs(ctx context.Context) {
	stream := newJoinLogStream(ctx, s.joinLogService)

	for {
		entry, err := stream.Recv()
		if err != nil {
			slog.Error("Join log stream error", slog.Any("error", err))
			return
		}

		s.processJoinLogEntry(entry)
	}
}

func (s *PlayerQueryService) streamProfileUpdates(ctx context.Context) {
	stream := newPlayerProfileStream(ctx, s.playerProfilesService)

	for {
		entry, err := stream.Recv()
		if err != nil {
			slog.Error("Profile updates stream error", slog.Any("error", err))
			return
		}

		s.processPlayerProfileEntry(entry)
	}
}
