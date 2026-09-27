// Package migrations incrusta los archivos SQL en el binario.
// Convención: NNNN_nombre.up.sql y NNNN_nombre.down.sql, numerados sin huecos.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
