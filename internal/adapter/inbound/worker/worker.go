// Package worker atiende los trabajos en segundo plano (asynq) dentro del mismo proceso de la API.
// Para escalar, el mismo binario puede correr solo como worker en otra máquina.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/santiago-noe/qatu-api/internal/adapter/outbound/queue"
)

// PhotoWork es lo que el worker necesita de service.PhotoService.
type PhotoWork interface {
	Process(ctx context.Context, photoID string) error
	CleanupStale(ctx context.Context) (int, error)
}

// cleanupEvery: las subidas abandonadas se buscan cada hora. Unique evita que dos instancias de la
// API encolen la misma limpieza.
const (
	cleanupEvery  = "@every 1h"
	cleanupUnique = 50 * time.Minute
)

type Worker struct {
	server    *asynq.Server
	scheduler *asynq.Scheduler
	mux       *asynq.ServeMux
}

func New(rdb redis.UniversalClient, log zerolog.Logger, concurrency int, photos PhotoWork) *Worker {
	server := asynq.NewServerFromRedisClient(rdb, asynq.Config{
		Concurrency: concurrency,
		Logger:      logger{log},
		LogLevel:    asynq.WarnLevel,
		ErrorHandler: asynq.ErrorHandlerFunc(func(_ context.Context, task *asynq.Task, err error) {
			log.Warn().Err(err).Str("task", task.Type()).Msg("trabajo fallido; se reintentará")
		}),
	})
	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TaskProcessPhoto, func(ctx context.Context, t *asynq.Task) error {
		var p queue.PhotoPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
		}
		return photos.Process(ctx, p.PhotoID)
	})
	mux.HandleFunc(queue.TaskCleanupPhotos, func(ctx context.Context, _ *asynq.Task) error {
		n, err := photos.CleanupStale(ctx)
		if n > 0 {
			log.Info().Int("fotos", n).Msg("subidas abandonadas borradas")
		}
		return err
	})
	scheduler := asynq.NewSchedulerFromRedisClient(rdb, &asynq.SchedulerOpts{Logger: logger{log}, LogLevel: asynq.WarnLevel})
	return &Worker{server: server, scheduler: scheduler, mux: mux}
}

// Start atiende trabajos en segundo plano y programa las tareas periódicas, hasta Shutdown.
func (w *Worker) Start() error {
	if _, err := w.scheduler.Register(cleanupEvery, asynq.NewTask(queue.TaskCleanupPhotos, nil), asynq.Unique(cleanupUnique)); err != nil {
		return err
	}
	if err := w.scheduler.Start(); err != nil {
		return err
	}
	return w.server.Start(w.mux)
}

// Shutdown deja de programar y espera a que terminen los trabajos en curso.
func (w *Worker) Shutdown() {
	w.scheduler.Shutdown()
	w.server.Shutdown()
}

// logger adapta zerolog a la interfaz de asynq.
type logger struct{ log zerolog.Logger }

func (l logger) Debug(args ...any) { l.log.Debug().Msg(fmt.Sprint(args...)) }
func (l logger) Info(args ...any)  { l.log.Info().Msg(fmt.Sprint(args...)) }
func (l logger) Warn(args ...any)  { l.log.Warn().Msg(fmt.Sprint(args...)) }
func (l logger) Error(args ...any) { l.log.Error().Msg(fmt.Sprint(args...)) }
func (l logger) Fatal(args ...any) { l.log.Fatal().Msg(fmt.Sprint(args...)) }
