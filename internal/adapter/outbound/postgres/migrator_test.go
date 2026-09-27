package postgres

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/santiago-noe/qatu-api/migrations"
)

func file(body string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(body)} }

func TestLoadMigrations(t *testing.T) {
	tests := []struct {
		name    string
		fsys    fstest.MapFS
		want    []int
		wantErr string
	}{
		{
			name: "ordena y empareja up/down",
			fsys: fstest.MapFS{
				"0002_b.up.sql": file("B"), "0002_b.down.sql": file("-B"),
				"0001_a.up.sql": file("A"), "0001_a.down.sql": file("-A"),
				"embed.go": file("package migrations"),
			},
			want: []int{1, 2},
		},
		{
			name:    "falta el down",
			fsys:    fstest.MapFS{"0001_a.up.sql": file("A")},
			wantErr: "debe tener",
		},
		{
			name: "hueco en la numeración",
			fsys: fstest.MapFS{
				"0001_a.up.sql": file("A"), "0001_a.down.sql": file("-A"),
				"0003_c.up.sql": file("C"), "0003_c.down.sql": file("-C"),
			},
			wantErr: "se esperaba la versión 0002",
		},
		{
			name:    "nombres distintos en la misma versión",
			fsys:    fstest.MapFS{"0001_a.up.sql": file("A"), "0001_x.down.sql": file("-A")},
			wantErr: "nombres distintos",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list, err := LoadMigrations(tt.fsys)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, se esperaba que contenga %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(list) != len(tt.want) {
				t.Fatalf("migraciones = %d, se esperaban %d", len(list), len(tt.want))
			}
			for i, v := range tt.want {
				if list[i].Version != v || list[i].Up == "" || list[i].Down == "" {
					t.Fatalf("migración %d inválida: %+v", i, list[i])
				}
			}
		})
	}
}

// Las migraciones reales del repositorio deben cumplir la convención.
func TestRepositoryMigrationsAreValid(t *testing.T) {
	list, err := LoadMigrations(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("no hay migraciones")
	}
}
