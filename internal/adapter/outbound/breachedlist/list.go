// Package breachedlist implementa port.BreachedPasswords con una lista local incrustada:
// las contraseñas nunca salen del servidor (decisión de clarify de la feature 001).
//
// Fuente: SecLists, xato-net-10-million-passwords-1000000.txt (licencia MIT,
// Copyright (c) 2018 Daniel Miessler). Se conservan solo las de 10 a 128 caracteres,
// en minúsculas y ASCII, porque las más cortas ya las rechaza la política de longitud.
package breachedlist

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"
)

//go:embed passwords.txt.gz
var compressed []byte

type List struct {
	once sync.Once
	set  map[string]struct{}
	err  error
}

func New() *List { return &List{} }

// Load descomprime la lista; se llama al arrancar para no demorar el primer registro.
func (l *List) Load() error {
	l.once.Do(func() {
		zr, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			l.err = fmt.Errorf("breachedlist: %w", err)
			return
		}
		defer zr.Close()

		set := make(map[string]struct{}, 120_000)
		sc := bufio.NewScanner(zr)
		for sc.Scan() {
			if line := sc.Text(); line != "" {
				set[line] = struct{}{}
			}
		}
		if err := sc.Err(); err != nil {
			l.err = fmt.Errorf("breachedlist: %w", err)
			return
		}
		l.set = set
	})
	return l.err
}

// IsBreached compara sin distinguir mayúsculas: "Qwertyuiop" también es insegura.
func (l *List) IsBreached(_ context.Context, password string) (bool, error) {
	if err := l.Load(); err != nil {
		return false, err
	}
	_, found := l.set[strings.ToLower(password)]
	return found, nil
}

func (l *List) Size() int { return len(l.set) }
