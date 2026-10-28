import { z } from "zod";
import { emailField, nameField } from "@/shared/lib/credentials";

// Mirrors CreateUserRequest and UpdateUserRequest in api/openapi.yaml.

export const userFormSchema = z.object({
  email: emailField(),
  name: nameField(),
  role: z.enum(["owner", "admin", "viewer"]),
});

export type UserFormValues = z.infer<typeof userFormSchema>;
