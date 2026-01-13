package services

import (
	"context"
	"sync"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/Alliance-Community/pr-logs-proxy/pkg/parsers"
	"github.com/nxadm/tail"
	"google.golang.org/grpc"
)

type UpdateService_UpdateLogsServer[T v1.AdminLogUpdatesResponse | v1.JoinLogUpdatesResponse | v1.PlayerProfilesUpdatesResponse] interface {
	Send(*T) error
	grpc.ServerStream
}

type updateService[
	T parsers.AdminEntry | parsers.JoinEntry | parsers.PlayerProfileEntry,
	V v1.AdminLogUpdatesResponse | v1.JoinLogUpdatesResponse | v1.PlayerProfilesUpdatesResponse,
] struct {
	logParser     func(string) (*T, error)
	entryToProto  func(*T) *V
	updateStreams []UpdateService_UpdateLogsServer[V]
	logPath       string
	mutex         sync.Mutex
	cancel        context.CancelFunc
}

func newUpdateService[
	T parsers.AdminEntry | parsers.JoinEntry | parsers.PlayerProfileEntry,
	V v1.AdminLogUpdatesResponse | v1.JoinLogUpdatesResponse | v1.PlayerProfilesUpdatesResponse,
](logPath string, parser func(string) (*T, error), entryToProto func(*T) *V) *updateService[T, V] {
	return &updateService[T, V]{
		logParser:     parser,
		entryToProto:  entryToProto,
		updateStreams: make([]UpdateService_UpdateLogsServer[V], 0),
		logPath:       logPath,
	}
}

func (s *updateService[T, V]) startTailing() error {
	if s.cancel != nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	tailer, err := tail.TailFile(s.logPath, tail.Config{Follow: true, CompleteLines: true})
	if err != nil {
		return err
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				s.cancel = nil
				return
			case line := <-tailer.Lines:
				entry, err := s.logParser(line.Text)
				if err != nil {
					continue
				}

				resp := s.entryToProto(entry)

				toRemove := []int{}
				s.mutex.Lock()
				for i, sub := range s.updateStreams {
					err = sub.Send(resp)
					if err != nil {
						toRemove = append(toRemove, i)
					}
				}
				for _, i := range toRemove {
					s.updateStreams = append(s.updateStreams[:i], s.updateStreams[i+1:]...)
				}
				if len(s.updateStreams) == 0 {
					s.cancel()
					s.cancel = nil
				}
				s.mutex.Unlock()
			}
		}
	}()

	return nil
}
