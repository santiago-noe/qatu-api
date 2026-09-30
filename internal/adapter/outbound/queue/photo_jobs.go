// Package queue encola trabajos en segundo plano con asynq sobre el mismo Redis de la API.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// TaskProcessPhoto procesa una foto recién subida (tamaños sin EXIF). Lo atiende inbound/worker.
const TaskProcessPhoto = "photo:process"

// PhotoPayload es el contenido de TaskProcessPhoto.
type PhotoPayload struct {
	PhotoID string `json:"photo_id"`
}

// Límites del trabajo: una foto grande tarda uno o dos segundos; los reintentos cubren caídas
// cortas del almacenamiento.
const (
	photoMaxRetry = 5
	photoTimeout  = 2 * time.Minute
)

// PhotoJobs implementa port.PhotoJobs.
type PhotoJobs struct {
	client *asynq.Client
}

func NewPhotoJobs(rdb redis.UniversalClient) *PhotoJobs {
	return &PhotoJobs{client: asynq.NewClientFromRedisClient(rdb)}
}

// EnqueuePhoto usa el ID de la foto como ID del trabajo: confirmar dos veces no la procesa dos veces.
func (j *PhotoJobs) EnqueuePhoto(ctx context.Context, photoID string) error {
	payload, err := json.Marshal(PhotoPayload{PhotoID: photoID})
	if err != nil {
		return err
	}
	_, err = j.client.EnqueueContext(ctx, asynq.NewTask(TaskProcessPhoto, payload),
		asynq.TaskID(TaskProcessPhoto+":"+photoID), asynq.MaxRetry(photoMaxRetry), asynq.Timeout(photoTimeout))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}
