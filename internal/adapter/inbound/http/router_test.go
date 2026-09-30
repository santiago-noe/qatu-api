package http

import (
	"bufio"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

// El contrato publicado (api/openapi.yaml) y el router dicen lo mismo: una ruta nueva sin
// documentar, o documentada y borrada, rompe esta prueba.
func TestOpenAPIMatchesRouter(t *testing.T) {
	pass := func(c fiber.Ctx) error { return c.Next() }
	app := NewRouter(zerolog.Nop(), Handlers{}, Middlewares{
		Session:   pass,
		RateLimit: func(string) fiber.Handler { return pass },
		Human:     func(string) fiber.Handler { return pass },
	}, Options{})

	param := regexp.MustCompile(`:(\w+)`)
	var routed []string
	for _, r := range app.GetRoutes(true) {
		if r.Method == fiber.MethodHead || !strings.HasPrefix(r.Path, "/api/v1/") {
			continue
		}
		path := strings.TrimSuffix(strings.TrimPrefix(r.Path, "/api/v1"), "/")
		if path == "" {
			path = "/"
		}
		routed = append(routed, r.Method+" "+param.ReplaceAllString(path, "{$1}"))
	}

	documented := openAPIOperations(t, "../../../../api/openapi.yaml")
	slices.Sort(routed)
	routed = slices.Compact(routed)
	for _, op := range routed {
		if !slices.Contains(documented, op) {
			t.Errorf("%s está en el router pero no en api/openapi.yaml", op)
		}
	}
	for _, op := range documented {
		if !slices.Contains(routed, op) {
			t.Errorf("%s está en api/openapi.yaml pero no en el router", op)
		}
	}
}

// openAPIOperations lee "MÉTODO /ruta" de la sección paths (dos espacios la ruta, cuatro el método).
func openAPIOperations(t *testing.T, file string) []string {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pathLine := regexp.MustCompile(`^  (/\S*):\s*$`)
	methodLine := regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	var ops []string
	var current string
	inPaths := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && line != "" && !strings.HasPrefix(line, " "):
			inPaths = false
		case inPaths && pathLine.MatchString(line):
			current = pathLine.FindStringSubmatch(line)[1]
		case inPaths && methodLine.MatchString(line):
			ops = append(ops, strings.ToUpper(methodLine.FindStringSubmatch(line)[1])+" "+current)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return ops
}
