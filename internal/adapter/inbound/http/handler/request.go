package handler

import "github.com/gofiber/fiber/v3"

// bindJSON lee el cuerpo JSON de la petición; si no es válido responde 400 con el mismo mensaje en toda la API.
func bindJSON(c fiber.Ctx, dst any) error {
	if err := c.Bind().JSON(dst); err != nil {
		return badRequest("El cuerpo debe ser JSON válido.")
	}
	return nil
}
