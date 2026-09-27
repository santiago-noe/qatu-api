package redisclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// SessionStore implementa port.SessionStore.
//
//	{prefix}session:{id}          JSON de la sesión, vence en ExpiresAt
//	{prefix}user_sessions:{user}  set con los IDs de sus sesiones ("cerrar en todos")
type SessionStore struct {
	rdb    *redis.Client
	prefix string
}

func NewSessionStore(rdb *redis.Client, prefix string) *SessionStore {
	return &SessionStore{rdb: rdb, prefix: prefix}
}

func (s *SessionStore) sessionKey(id string) string  { return s.prefix + "session:" + id }
func (s *SessionStore) userKey(userID string) string { return s.prefix + "user_sessions:" + userID }

func (s *SessionStore) Save(ctx context.Context, session domain.Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	_, err = s.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.SetArgs(ctx, s.sessionKey(session.ID), data, redis.SetArgs{ExpireAt: session.ExpiresAt})
		p.SAdd(ctx, s.userKey(session.UserID), session.ID)
		// Todas las sesiones duran lo mismo: la última guardada es la que vence más tarde.
		p.ExpireAt(ctx, s.userKey(session.UserID), session.ExpiresAt)
		return nil
	})
	return err
}

func (s *SessionStore) Get(ctx context.Context, id string) (domain.Session, error) {
	data, err := s.rdb.Get(ctx, s.sessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return domain.Session{}, domain.ErrSessionInvalid
	}
	if err != nil {
		return domain.Session{}, err
	}
	return decodeSession(data)
}

// updateRetries: intentos si otra petición modificó la sesión entre la lectura y la escritura.
const updateRetries = 3

// Update lee, modifica y escribe con WATCH: si otra petición cambia o cierra la sesión en
// medio, la transacción se descarta y se reintenta sobre el valor nuevo. Así una renovación
// no borra la marca del segundo paso ni revive una sesión cerrada.
func (s *SessionStore) Update(ctx context.Context, id string, change func(*domain.Session)) (domain.Session, error) {
	key := s.sessionKey(id)
	var session domain.Session
	update := func(tx *redis.Tx) error {
		data, err := tx.Get(ctx, key).Bytes()
		if errors.Is(err, redis.Nil) {
			return domain.ErrSessionInvalid
		}
		if err != nil {
			return err
		}
		if session, err = decodeSession(data); err != nil {
			return err
		}
		change(&session)
		if data, err = json.Marshal(session); err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.SetArgs(ctx, key, data, redis.SetArgs{ExpireAt: session.ExpiresAt})
			// GT: el set de sesiones del usuario vive lo que su sesión más larga; nunca se acorta.
			p.ExpireGT(ctx, s.userKey(session.UserID), time.Until(session.ExpiresAt))
			return nil
		})
		return err
	}
	for range updateRetries {
		err := s.rdb.Watch(ctx, update, key)
		if !errors.Is(err, redis.TxFailedErr) {
			if err != nil {
				return domain.Session{}, err
			}
			return session, nil
		}
	}
	return domain.Session{}, fmt.Errorf("sesión: demasiados cambios simultáneos")
}

func decodeSession(data []byte) (domain.Session, error) {
	var session domain.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return domain.Session{}, fmt.Errorf("sesión corrupta: %w", err)
	}
	return session, nil
}

// Delete solo borra si la sesión es del usuario: nadie cierra sesiones ajenas por ID.
func (s *SessionStore) Delete(ctx context.Context, userID, id string) error {
	session, err := s.Get(ctx, id)
	if errors.Is(err, domain.ErrSessionInvalid) {
		return s.rdb.SRem(ctx, s.userKey(userID), id).Err()
	}
	if err != nil {
		return err
	}
	if session.UserID != userID {
		return domain.ErrNotFound
	}
	_, err = s.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Del(ctx, s.sessionKey(id))
		p.SRem(ctx, s.userKey(userID), id)
		return nil
	})
	return err
}

func (s *SessionStore) DeleteAllForUser(ctx context.Context, userID, exceptID string) error {
	ids, err := s.rdb.SMembers(ctx, s.userKey(userID)).Result()
	if err != nil {
		return err
	}
	_, err = s.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		for _, id := range ids {
			if id == exceptID {
				continue
			}
			p.Del(ctx, s.sessionKey(id))
			p.SRem(ctx, s.userKey(userID), id)
		}
		return nil
	})
	return err
}

// ListForUser devuelve las sesiones vigentes y limpia del set las que ya vencieron.
func (s *SessionStore) ListForUser(ctx context.Context, userID string) ([]domain.Session, error) {
	ids, err := s.rdb.SMembers(ctx, s.userKey(userID)).Result()
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = s.sessionKey(id)
	}
	values, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	sessions := make([]domain.Session, 0, len(values))
	var stale []any
	for i, v := range values {
		raw, ok := v.(string)
		if !ok {
			stale = append(stale, ids[i])
			continue
		}
		session, err := decodeSession([]byte(raw))
		if err != nil {
			stale = append(stale, ids[i])
			continue
		}
		sessions = append(sessions, session)
	}
	if len(stale) > 0 {
		s.rdb.SRem(ctx, s.userKey(userID), stale...)
	}
	return sessions, nil
}
