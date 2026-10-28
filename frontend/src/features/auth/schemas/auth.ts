import { z } from "zod";
import { strings } from "@/shared/lib/strings";

// Mirrors LoginRequest and ChangePasswordRequest in api/openapi.yaml.

export const loginSchema = z.object({
  username: z.string().trim().min(1, strings.auth.usernameRequired),
  password: z.string().min(1, strings.auth.passwordRequired),
});

export type LoginValues = z.infer<typeof loginSchema>;

export const PASSWORD_MIN_LENGTH = 8;

export const changePasswordSchema = z
  .object({
    current_password: z.string().min(1, strings.auth.currentRequired),
    new_password: z.string().min(PASSWORD_MIN_LENGTH, strings.auth.newTooShort),
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
