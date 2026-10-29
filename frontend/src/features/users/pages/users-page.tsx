import { PlusIcon, UsersIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useMe } from "@/features/auth/api/auth";
import { RequirePermission } from "@/features/auth/components/can";
import { errorMessage } from "@/shared/api/error-message";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { useInvalidCursorReset } from "@/shared/lib/use-cursor-pages";
import { Button } from "@/shared/ui/button";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { SecretRevealDialog } from "@/shared/ui/secret-reveal-dialog";
import { Section } from "@/shared/ui/section";
import { StatusBadge } from "@/shared/ui/status-badge";
import { useDisableUser, useEnableUser, useResetUserPassword, useUsers, type User } from "../api/users";
import { AddUserDialog, EditUserDialog } from "../components/user-dialogs";
import { UserRowActions, type UserAction } from "../components/user-row-actions";

type ConfirmAction = Exclude<UserAction, "edit">;

interface Revealed {
  email: string;
  password: string;
}

// Disable and reset are hard to undo, so they need the email typed; enable doesn't.
const confirmCopy: Record<
  ConfirmAction,
  { title: (name: string) => string; body: string; label: string; pendingLabel: string; typeEmail: boolean; destructive: boolean }
> = {
  reset: {
    title: strings.users.resetTitle,
    body: strings.users.resetBody,
    label: strings.users.resetPassword,
    pendingLabel: strings.users.resetting,
    typeEmail: true,
    destructive: true,
  },
  disable: {
    title: strings.users.disableTitle,
    body: strings.users.disableBody,
    label: strings.users.disable,
    pendingLabel: strings.users.disabling,
    typeEmail: true,
    destructive: true,
  },
  enable: {
    title: strings.users.enableTitle,
    body: strings.users.enableBody,
    label: strings.users.enable,
    pendingLabel: strings.users.enabling,
    typeEmail: false,
    destructive: false,
  },
};

function actionError(error: unknown, fallback: string): string {
  if (isApiError(error) && error.code === "last_owner") return strings.users.lastOwner;
  if (isApiError(error) && error.code === "cannot_modify_self") return strings.users.cannotModifySelf;
  return errorMessage(error, fallback);
}

/** `/settings/users`: owners only (`users:manage`). */
export function UsersTab() {
  return (
    <RequirePermission permission="users:manage">
      <UsersSection />
    </RequirePermission>
  );
}

function UsersSection() {
  const selfId = useMe().data?.user.id;
  const [cursor, setCursor] = useState<string | null>(null);
  const users = useUsers(cursor);
  const { resetting, resetKey } = useInvalidCursorReset(users.error, cursor, setCursor, strings.settings.cursorReset);

  const disable = useDisableUser();
  const enable = useEnableUser();
  const reset = useResetUserPassword();

  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<User | null>(null);
  const [confirming, setConfirming] = useState<{ action: ConfirmAction; user: User } | null>(null);
  const [revealed, setRevealed] = useState<Revealed | null>(null);

  function onAction(action: UserAction, user: User) {
    if (action === "edit") setEditing(user);
    else setConfirming({ action, user });
  }

  function confirm() {
    if (!confirming) return;
    const { action, user } = confirming;
    const done = () => setConfirming(null);
    if (action === "reset") {
      reset.mutate(user.id, {
        onSuccess: (result) => {
          done();
          toast.success(strings.users.resetDone(user.name));
          setRevealed({ email: result.user.email, password: result.temporary_password });
        },
        onError: (error) => toast.error(actionError(error, strings.users.resetFailed)),
      });
    } else if (action === "disable") {
      disable.mutate(user.id, {
        onSuccess: () => {
          done();
          toast.success(strings.users.disabled(user.name));
        },
        onError: (error) => toast.error(actionError(error, strings.users.disableFailed)),
      });
    } else {
      enable.mutate(user.id, {
        onSuccess: () => {
          done();
          toast.success(strings.users.enabled(user.name));
        },
        onError: (error) => toast.error(actionError(error, strings.users.enableFailed)),
      });
    }
  }

  const columns: DataColumn<User>[] = [
    {
      id: "user",
      header: strings.users.user,
      card: "title",
      className: "max-w-72",
      cell: (u) => (
        <div className="grid min-w-0">
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate font-medium" title={u.name}>
              {u.name}
            </span>
            {u.id === selfId && (
              <span className="shrink-0 rounded-full border px-2 py-0.5 text-xs font-medium text-muted-foreground">
                {strings.users.you}
              </span>
            )}
          </span>
          <span className="truncate text-xs text-muted-foreground" title={u.email}>
            {u.email}
          </span>
        </div>
      ),
    },
    { id: "role", header: strings.users.role, cell: (u) => strings.roles[u.role] ?? u.role },
    {
      id: "status",
      header: strings.users.status,
      cell: (u) => (
        <div className="grid justify-items-start gap-1 max-md:justify-items-end">
          <StatusBadge status={u.status} />
          {u.must_change_password && <span className="text-xs text-muted-foreground">{strings.users.passwordPending}</span>}
        </div>
      ),
    },
    {
      id: "last_login",
      header: strings.users.lastSignIn,
      cell: (u) =>
        u.last_login_at ? (
          <DateTimeText value={u.last_login_at} />
        ) : (
          <span className="text-muted-foreground">{strings.users.never}</span>
        ),
    },
    { id: "created", header: strings.users.created, cell: (u) => <DateTimeText value={u.created_at} /> },
    {
      id: "actions",
      header: strings.common.actions,
      card: "actions",
      align: "right",
      cell: (u) => <UserRowActions user={u} isSelf={u.id === selfId} onAction={onAction} />,
    },
  ];

  const addButton = (
    <Button onClick={() => setAdding(true)}>
      <PlusIcon aria-hidden="true" />
      {strings.users.add}
    </Button>
  );

  const confirmPending = reset.isPending || disable.isPending || enable.isPending;
  const target = confirming?.user;
  const copy = confirming ? confirmCopy[confirming.action] : null;

  return (
    <Section id="users-heading" title={strings.users.title} description={strings.users.description} actions={addButton}>
      <DataTable
        caption={strings.users.title}
        columns={columns}
        data={users.data?.data}
        getRowId={(u) => u.id}
        isLoading={users.isPending || resetting}
        error={resetting ? null : users.error}
        onRetry={() => void users.refetch()}
        pagination={{ nextCursor: users.data?.next_cursor, onPageChange: setCursor }}
        resetKey={resetKey}
        emptyState={
          <EmptyState
            icon={UsersIcon}
            headingLevel={3}
            title={strings.users.emptyTitle}
            description={strings.users.emptyBody}
            action={addButton}
          />
        }
      />

      <AddUserDialog
        open={adding}
        onOpenChange={setAdding}
        onAdded={(user, password) => {
          setAdding(false);
          toast.success(strings.users.added(user.email));
          setRevealed({ email: user.email, password });
        }}
      />
      <EditUserDialog
        user={editing}
        onClose={() => setEditing(null)}
        onSaved={(user) => {
          setEditing(null);
          toast.success(strings.users.updated(user.name));
        }}
      />
      <ConfirmDialog
        open={confirming !== null}
        onOpenChange={(open) => !open && setConfirming(null)}
        title={copy && target ? copy.title(target.name) : ""}
        description={copy?.body ?? ""}
        confirmText={copy?.typeEmail ? target?.email : undefined}
        confirmLabel={copy?.label ?? ""}
        pendingLabel={copy?.pendingLabel}
        pending={confirmPending}
        destructive={copy?.destructive}
        onConfirm={confirm}
      />
      <SecretRevealDialog
        open={revealed !== null}
        title={strings.users.secretTitle}
        description={revealed ? strings.users.secretDescription(revealed.email) : ""}
        secrets={revealed ? [{ label: strings.users.temporaryPassword, value: revealed.password }] : []}
        warning={strings.users.secretWarning}
        onClose={() => setRevealed(null)}
      />
    </Section>
  );
}
