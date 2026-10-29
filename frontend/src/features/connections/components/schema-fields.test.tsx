import { zodResolver } from "@hookform/resolvers/zod";
import { screen, within } from "@testing-library/react";
import { useForm } from "react-hook-form";
import { describe, expect, it } from "vitest";
import { z } from "zod";
import { ApiError } from "@/shared/api/errors";
import { Form } from "@/shared/ui/form";
import { provider } from "@/test/fixtures";
import { renderWithProviders } from "@/test/render";
import type { SchemaField } from "../api/connections";
import { applyConnectionErrors } from "../schemas/connection-form";
import { hasUnsupportedField, schemaDefaults, schemaFormSchema, type SchemaValues } from "../schemas/schema-form";
import { SchemaFields } from "./schema-fields";

interface Values {
  credentials: SchemaValues;
  settings: SchemaValues;
}

function Harness({
  credentials,
  settings,
  mode = "create",
  serverError,
}: {
  credentials: SchemaField[];
  settings: SchemaField[];
  mode?: "create" | "replace";
  serverError?: ApiError;
}) {
  const form = useForm<Values>({
    resolver: zodResolver(
      z.object({ credentials: schemaFormSchema(credentials, mode), settings: schemaFormSchema(settings) }),
    ),
    defaultValues: { credentials: schemaDefaults(credentials), settings: schemaDefaults(settings) },
  });
  const unsupported = hasUnsupportedField(credentials) || hasUnsupportedField(settings);
  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(() => undefined)} noValidate>
        <SchemaFields
          control={form.control}
          prefix="credentials"
          fields={credentials}
          mode={mode}
          hints={{ secret_key: "a1b2", callback_token: null }}
        />
        <SchemaFields control={form.control} prefix="settings" fields={settings} />
        <button type="submit" disabled={unsupported}>
          Submit
        </button>
        {serverError && (
          <button
            type="button"
            onClick={() => applyConnectionErrors(serverError, form.setError, { sections: { credentials, settings } })}
          >
            Server error
          </button>
        )}
      </form>
    </Form>
  );
}

const { credential_schema, settings_schema } = provider();

describe("SchemaFields", () => {
  it("renders a secret as a password input with a toggle and no autocomplete", async () => {
    const { user } = renderWithProviders(
      <Harness credentials={credential_schema.fields} settings={settings_schema.fields} />,
    );
    const secret = screen.getByLabelText(/Secret API key/, { selector: "input" });
    expect(secret).toHaveAttribute("type", "password");
    expect(secret).toHaveAttribute("autocomplete", "off");
    expect(secret).toHaveAttribute("spellcheck", "false");
    expect(secret).toHaveAttribute("placeholder", "xnd_development_…");
    await user.type(secret, "xnd_development_abc");
    await user.click(screen.getByRole("button", { name: "Show value: Secret API key" }));
    expect(secret).toHaveAttribute("type", "text");
    await user.click(screen.getByRole("button", { name: "Hide value: Secret API key" }));
    expect(secret).toHaveAttribute("type", "password");
  });

  it("marks required fields and shows descriptions", () => {
    renderWithProviders(<Harness credentials={credential_schema.fields} settings={settings_schema.fields} />);
    const secret = screen.getByLabelText(/Secret API key/, { selector: "input" });
    expect(secret).toHaveAttribute("aria-required", "true");
    expect(screen.getByText("Secret API key").closest("label")).toHaveTextContent("Secret API key*");
    expect(screen.getByText("Dashboard → Settings → API keys.")).toBeInTheDocument();
    // Optional settings carry no marker.
    expect(screen.getByText("Invoice lifetime (seconds)").closest("label")).toHaveTextContent(/\)$/);
    expect(screen.getByLabelText("Invoice lifetime (seconds)")).not.toHaveAttribute("aria-required");
  });

  it("renders integer, enum with a Default choice, and boolean fields", async () => {
    const settings: SchemaField[] = [
      ...settings_schema.fields,
      {
        key: "capture",
        type: "boolean",
        label: "Capture automatically",
        description: "",
        secret: false,
        required: false,
        default: true,
      },
    ];
    const { user } = renderWithProviders(<Harness credentials={[]} settings={settings} />);
    expect(screen.getByLabelText("Invoice lifetime (seconds)")).toHaveAttribute("inputmode", "numeric");
    expect(screen.getByRole("switch", { name: "Capture automatically" })).toBeChecked();
    const locale = screen.getByRole("combobox", { name: "Checkout language" });
    expect(locale).toHaveTextContent("Default");
    await user.click(locale);
    expect(await screen.findByRole("option", { name: "Default" })).toBeInTheDocument();
    await user.click(screen.getByRole("option", { name: "English" }));
    expect(screen.getByRole("combobox", { name: "Checkout language" })).toHaveTextContent("English");
  });

  it("shows client rule messages on submit", async () => {
    const { user } = renderWithProviders(
      <Harness credentials={credential_schema.fields} settings={settings_schema.fields} />,
    );
    await user.type(screen.getByLabelText(/Secret API key/, { selector: "input" }), "sk_nope");
    await user.type(screen.getByLabelText("Invoice lifetime (seconds)"), "10");
    await user.click(screen.getByRole("button", { name: "Submit" }));
    expect(await screen.findByText("Starts with xnd_development_ (test) or xnd_production_ (live).")).toBeInTheDocument();
    expect(screen.getByText("This field is required.")).toBeInTheDocument();
    expect(screen.getByText("Use 60 or more.")).toBeInTheDocument();
  });

  it("uses masked hints as placeholders in replace mode, without required markers", () => {
    renderWithProviders(<Harness credentials={credential_schema.fields} settings={[]} mode="replace" />);
    const secret = screen.getByLabelText("Secret API key");
    expect(secret).toHaveAttribute("placeholder", "•••• a1b2");
    expect(secret).toHaveValue("");
    expect(screen.getByLabelText("Webhook verification token")).toHaveAttribute("placeholder", "Stored");
    expect(secret).not.toHaveAttribute("aria-required");
  });

  it("disables submit for a field type it doesn't know", () => {
    const future = { ...credential_schema.fields[1]!, key: "certificate", label: "Certificate", type: "file" };
    renderWithProviders(
      <Harness credentials={[...credential_schema.fields, future as unknown as SchemaField]} settings={[]} />,
    );
    const unsupported = screen.getByTestId("unsupported-field");
    expect(unsupported).toHaveTextContent("Certificate");
    expect(within(unsupported).getByText("This provider needs a newer console.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Submit" })).toBeDisabled();
  });

  it("puts server field errors on their fields", async () => {
    const error = new ApiError({
      status: 400,
      code: "invalid_request",
      message: "invalid request",
      details: {
        fields: [
          { field: "credentials.secret_key", rule: "environment" },
          { field: "settings.invoice_duration", rule: "maximum" },
        ],
      },
    });
    const { user } = renderWithProviders(
      <Harness credentials={credential_schema.fields} settings={settings_schema.fields} serverError={error} />,
    );
    await user.click(screen.getByRole("button", { name: "Server error" }));
    const secret = screen.getByLabelText(/Secret API key/, { selector: "input" });
    expect(secret).toHaveAttribute("aria-invalid", "true");
    expect(secret).toHaveAccessibleDescription(/This key is for the other environment\./);
    const lifetime = screen.getByLabelText("Invoice lifetime (seconds)");
    expect(lifetime).toHaveAttribute("aria-invalid", "true");
    expect(lifetime).toHaveAccessibleDescription(/Use 31\.536\.000 or less\./);
  });
});
