import { z } from "zod";
import { emailField, nameField, newPasswordField } from "@/shared/lib/credentials";
import { strings } from "@/shared/lib/strings";

// Mirrors LoginRequest, SetupRequest and ChangePasswordRequest in api/openapi.yaml.

export const loginSchema = z.object({
  // Not format-checked: the server answers a malformed email like a wrong one.
  email: z.string().trim().min(1, strings.auth.emailRequired),
  password: z.string().min(1, strings.auth.passwordRequired),
});

export type LoginValues = z.infer<typeof loginSchema>;

export function setupSchema(tokenRequired: boolean) {
  return z
    .object({
      name: nameField(),
      email: emailField(),
      password: newPasswordField(),
      confirm_password: z.string(),
      setup_token: z.string(),
    })
    .refine((v) => v.confirm_password === v.password, {
      path: ["confirm_password"],
      message: strings.auth.confirmMismatch,
    })
    .refine((v) => !tokenRequired || v.setup_token.length > 0, {
      path: ["setup_token"],
      message: strings.setup.setupTokenRequired,
    });
}

export type SetupValues = z.infer<ReturnType<typeof setupSchema>>;

export const changePasswordSchema = z
  .object({
    current_password: z.string().min(1, strings.auth.currentRequired),
    new_password: newPasswordField(),
    confirm_password: z.string(),
  })
  .refine((v) => v.confirm_password === v.new_password, {
    path: ["confirm_password"],
    message: strings.auth.confirmMismatch,
  })
  .refine((v) => v.new_password !== v.current_password, {
    path: ["new_password"],
    message: strings.auth.newSameAsCurrent,
  });

export type ChangePasswordValues = z.infer<typeof changePasswordSchema>;
