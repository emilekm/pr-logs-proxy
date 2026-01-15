package services

import (
	"sync"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/emilekm/go-prbf2/logs"
	"github.com/nxadm/tail"
	"google.golang.org/grpc"
)

type UpdateService_UpdateLogsServer[T v1.AdminLogUpdatesResponse | v1.JoinLogUpdatesResponse | v1.PlayerProfilesUpdatesResponse] interface {
	Send(*T) error
	grpc.ServerStream
}

type updateService[
	T logs.AdminEntry | logs.JoinEntry | logs.PlayerProfileEntry,
	V v1.AdminLogUpdatesResponse | v1.JoinLogUpdatesResponse | v1.PlayerProfilesUpdatesResponse,
] struct {
	logParser     func(string) (*T, error)
	entryToProto  func(*T) *V
	updateStreams []UpdateService_UpdateLogsServer[V]
	logPath       string
	mutex         sync.RWMutex
}

func newUpdateService[
	T logs.AdminEntry | logs.JoinEntry | logs.PlayerProfileEntry,
	V v1.AdminLogUpdatesResponse | v1.JoinLogUpdatesResponse | v1.PlayerProfilesUpdatesResponse,
](logPath string, parser func(string) (*T, error), entryToProto func(*T) *V) *updateService[T, V] {
	return &updateService[T, V]{
		logParser:     parser,
		entryToProto:  entryToProto,
		updateStreams: make([]UpdateService_UpdateLogsServer[V], 0),
		logPath:       logPath,
	}
}

func (s *updateService[T, V]) startTailing(sub UpdateService_UpdateLogsServer[V]) error {
	s.mutex.Lock()
	s.updateStreams = append(s.updateStreams, sub)
	if len(s.updateStreams) == 1 {
		err := s.startTailer()
		if err != nil {
			s.mutex.Unlock()
			return err
		}
	}
	s.mutex.Unlock()

	<-sub.Context().Done()

	return nil
}

func (s *updateService[T, V]) startTailer() error {
	tailer, err := tail.TailFile(s.logPath, tail.Config{Follow: true, CompleteLines: true})
	if err != nil {
		return err
	}

	go func() {
		defer tailer.Stop()
		for line := range tailer.Lines {
			entry, err := s.logParser(line.Text)
			if err != nil {
				continue
			}

			resp := s.entryToProto(entry)

			toRemove := make([]int, 0)
			s.mutex.RLock()
			for i, sub := range s.updateStreams {
				err = sub.Send(resp)
				if err != nil {
					toRemove = append(toRemove, i)
				}
			}
			s.mutex.RUnlock()

			s.mutex.Lock()
			for _, i := range toRemove {
				s.updateStreams = append(s.updateStreams[:i], s.updateStreams[i+1:]...)
			}
			if len(s.updateStreams) == 0 {
				s.mutex.Unlock()
				return
			}
			s.mutex.Unlock()

		}
	}()

	return nil
}
