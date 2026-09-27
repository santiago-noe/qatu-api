package domain

// CodePurpose separa los códigos de un solo uso por finalidad: un código de verificación
// de correo nunca sirve para recuperar la contraseña.
type CodePurpose string

const (
	CodeEmailVerification CodePurpose = "email_verification"
	CodePasswordReset     CodePurpose = "password_reset"
	CodeTwoFactor         CodePurpose = "two_factor"
)

// CodeLength: códigos de 6 dígitos (decisión de clarify de la feature 001).
const CodeLength = 6
