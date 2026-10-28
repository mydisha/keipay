import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useForm, type UseFormSetError } from "react-hook-form";
import { errorMessage } from "@/shared/api/error-message";
import { isApiError } from "@/shared/api/errors";
import { canonicalEmail } from "@/shared/lib/credentials";
import { applyFieldErrors } from "@/shared/lib/field-errors";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { Form } from "@/shared/ui/form";
import { useCreateUser, useUpdateUser, type UpdateUserRequest, type User } from "../api/users";
import { userFormSchema, type UserFormValues } from "../schemas/user-form";
import { UserFormFields } from "./user-form-fields";

const FIELD_NAMES = ["email", "name", "role"] as const;

/** Places a server error on a field when it belongs to one; otherwise returns the alert text. */
function serverError(error: unknown, setError: UseFormSetError<UserFormValues>, fallback: string): string | null {
  if (isApiError(error) && error.code === "conflict") {
    setError("email", { message: strings.users.emailTaken }, { shouldFocus: true });
    return null;
  }
  if (isApiError(error) && error.code === "last_owner") return strings.users.lastOwner;
  if (applyFieldErrors(error, setError, FIELD_NAMES)) return null;
  return errorMessage(error, fallback);
}

interface UserFormDialogProps {
  title: string;
  description: string;
  defaultValues: UserFormValues;
  submitLabel: string;
  pendingLabel: string;
  pending: boolean;
  onSubmit: (values: UserFormValues, setFormError: (message: string | null) => void, setError: UseFormSetError<UserFormValues>) => void;
  onCancel: () => void;
}

function UserFormDialogContent({
  title,
  description,
  defaultValues,
  submitLabel,
  pendingLabel,
  pending,
  onSubmit,
  onCancel,
}: UserFormDialogProps) {
  const [formError, setFormError] = useState<string | null>(null);
  const form = useForm<UserFormValues>({ resolver: zodResolver(userFormSchema), defaultValues });

  return (
    <DialogContent showCloseButton={!pending}>
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </DialogHeader>
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit((values) => {
            setFormError(null);
            onSubmit(values, setFormError, form.setError);
          })}
          noValidate
          className="grid gap-4"
        >
          {formError && (
            <Alert variant="danger">
              <CircleAlertIcon aria-hidden="true" />
              <AlertDescription>{formError}</AlertDescription>
            </Alert>
          )}
          <fieldset disabled={pending} className="grid gap-4">
            <UserFormFields control={form.control} />
          </fieldset>
          <DialogFooter>
            <Button variant="outline" onClick={onCancel} disabled={pending}>
              {strings.common.cancel}
            </Button>
            <Button type="submit" disabled={pending}>
              {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
              {pending ? pendingLabel : submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </Form>
    </DialogContent>
  );
}

interface AddUserDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onAdded: (user: User, temporaryPassword: string) => void;
}

export function AddUserDialog({ open, onOpenChange, onAdded }: AddUserDialogProps) {
  const create = useCreateUser();
  return (
    <Dialog open={open} onOpenChange={(next) => !create.isPending && onOpenChange(next)}>
      {open && (
        <UserFormDialogContent
          title={strings.users.addTitle}
          description={strings.users.addDescription}
          defaultValues={{ email: "", name: "", role: "viewer" }}
          submitLabel={strings.users.add}
          pendingLabel={strings.users.adding}
          pending={create.isPending}
          onCancel={() => onOpenChange(false)}
          onSubmit={(values, setFormError, setError) =>
            create.mutate(values, {
              onSuccess: (result) => onAdded(result.user, result.temporary_password),
              onError: (error) => setFormError(serverError(error, setError, strings.users.addFailed)),
            })
          }
        />
      )}
    </Dialog>
  );
}

/** Only the fields that changed: the API records exactly those. */
function changedFields(user: User, values: UserFormValues): UpdateUserRequest {
  const body: UpdateUserRequest = {};
  if (canonicalEmail(values.email) !== user.email) body.email = values.email;
  if (values.name !== user.name) body.name = values.name;
  if (values.role !== user.role) body.role = values.role;
  return body;
}

interface EditUserDialogProps {
  user: User | null;
  onClose: () => void;
  onSaved: (user: User) => void;
}

export function EditUserDialog({ user, onClose, onSaved }: EditUserDialogProps) {
  const update = useUpdateUser();
  return (
    <Dialog open={user !== null} onOpenChange={(open) => !open && !update.isPending && onClose()}>
      {user && (
        <UserFormDialogContent
          key={user.id}
          title={strings.users.editTitle(user.name)}
          description={strings.users.editDescription}
          defaultValues={{ email: user.email, name: user.name, role: user.role }}
          submitLabel={strings.common.save}
          pendingLabel={strings.common.saving}
          pending={update.isPending}
          onCancel={onClose}
          onSubmit={(values, setFormError, setError) => {
            const body = changedFields(user, values);
            if (Object.keys(body).length === 0) {
              onClose();
              return;
            }
            update.mutate(
              { id: user.id, body },
              {
                onSuccess: (result) => onSaved(result.user),
                onError: (error) => setFormError(serverError(error, setError, strings.users.updateFailed)),
              },
            );
          }}
        />
      )}
    </Dialog>
  );
}
