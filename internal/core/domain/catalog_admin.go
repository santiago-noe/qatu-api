package domain

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Límites de edición del catálogo (los mismos CHECK de la migración 0003).
const (
	CategoryNameMax        = 80
	CategoryDescriptionMax = 280
)

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// Nombre de un ícono de lucide-react en PascalCase (HardHat, PaintRoller).
	iconPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{1,40}$`)
)

func (r RiskLevel) valid() bool { return r == RiskLow || r == RiskMedium || r == RiskHigh }

// NormalizeCategory limpia y valida los campos editables de una categoría. El JSON Schema de
// atributos lo valida el servicio con su port (necesita una librería).
func NormalizeCategory(c Category) (Category, error) {
	c.Slug = strings.TrimSpace(c.Slug)
	c.Name = strings.Join(strings.Fields(c.Name), " ")
	c.Description = strings.TrimSpace(c.Description)
	c.Icon = strings.TrimSpace(c.Icon)
	switch {
	case !slugPattern.MatchString(c.Slug):
		return Category{}, ErrInvalidSlug
	case c.Name == "" || utf8.RuneCountInString(c.Name) > CategoryNameMax:
		return Category{}, ErrInvalidName
	case utf8.RuneCountInString(c.Description) > CategoryDescriptionMax:
		return Category{}, ErrInvalidDescription
	case c.Icon != "" && !iconPattern.MatchString(c.Icon):
		return Category{}, ErrInvalidIcon
	case !c.RiskLevel.valid():
		return Category{}, ErrInvalidRisk
	case c.SortOrder < 0 || c.SortOrder > 32767:
		return Category{}, ErrInvalidSortOrder
	}
	if _, ok := ParseVertical(string(c.Vertical)); !ok {
		return Category{}, ErrInvalidVertical
	}
	return c, nil
}
