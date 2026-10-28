import { zodResolver } from "@hookform/resolvers/zod";
import { ChevronDownIcon, CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useId, useRef, useState } from "react";
import { useForm, type FieldErrors, type Resolver } from "react-hook-form";
import { z } from "zod";
import { errorMessage } from "@/shared/api/error-message";
import { useMode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { RadioGroup, RadioGroupItem } from "@/shared/ui/radio-group";
import {
  CONNECTION_ENVIRONMENTS,
  isConnectionEnvironment,
  useCreateConnection,
  type Connection,
  type CreateConnectionRequest,
  type Provider,
} from "../../api/connections";
import { applyConnectionErrors, connectionNameSchema, normalizeName, suggestName } from "../../schemas/connection-form";
import {
  credentialPayload,
  hasUnsupportedField,
  schemaDefaults,
  schemaFormSchema,
  schemaPayload,
  type SchemaValues,
} from "../../schemas/schema-form";
import { SchemaFields } from "../schema-fields";
import { WizardFrame } from "./wizard-frame";

interface WizardValues {
  provider: string;
  environment: "test" | "live";
  name: string;
  credentials: SchemaValues;
  settings: SchemaValues;
}

const STEP_ONE_FIELDS = ["provider", "environment", "name"] as const;

function fieldsOf(provider: Provider | undefined) {
  return {
    credentials: provider?.credential_schema.fields ?? [],
    settings: provider?.settings_schema.fields ?? [],
  };
}

/** Validates with the schemas of whichever provider is selected at the time. */
function wizardResolver(providers: readonly Provider[]): Resolver<WizardValues> {
  return (values, context, options) => {
    const { credentials, settings } = fieldsOf(providers.find((p) => p.name === values.provider));
    const schema = z.object({
      provider: z.string().min(1, strings.connections.providerRequired),
      environment: z.enum(["test", "live"], { message: strings.connections.environmentInvalid }),
      name: connectionNameSchema,
      credentials: schemaFormSchema(credentials),
      settings: schemaFormSchema(settings),
    });
    return zodResolver(schema)(values, context, options);
  };
}

interface CreateConnectionStepsProps {
  providers: readonly Provider[];
  /** `?environment=` from the link that opened the wizard; the mode when absent or invalid. */
  initialEnvironment: string | null;
  onCreated: (connection: Connection) => void;
}

/** Steps 1 (provider, environment, name) and 2 (credentials, settings) in one form; ends with the create request. */
export function CreateConnectionSteps({ providers, initialEnvironment, onCreated }: CreateConnectionStepsProps) {
  const { mode } = useMode();
  const create = useCreateConnection();
  const [step, setStep] = useState<1 | 2>(1);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  // The name follows provider and environment until the user types in it.
  const nameEdited = useRef(false);
  const environmentLabelId = useId();
  const providerLabelId = useId();
  const advancedId = useId();

  const only = providers.length === 1 ? providers[0] : undefined;
  const environment = isConnectionEnvironment(initialEnvironment) ? initialEnvironment : mode;
  const form = useForm<WizardValues>({
    resolver: wizardResolver(providers),
    defaultValues: {
      provider: only?.name ?? "",
      environment,
      name: only ? suggestName(only.name, environment) : "",
      credentials: schemaDefaults(only?.credential_schema.fields ?? []),
      settings: schemaDefaults(only?.settings_schema.fields ?? []),
    },
  });
  const pending = create.isPending;
  const selected = providers.find((p) => p.name === form.watch("provider"));
  const { credentials: credentialFields, settings: settingsFields } = fieldsOf(selected);
  const unsupported = hasUnsupportedField(credentialFields) || hasUnsupportedField(settingsFields);

  function suggest(providerName: string, env: string) {
    if (!nameEdited.current && providerName) form.setValue("name", suggestName(providerName, env));
  }

  function chooseProvider(name: string) {
    const provider = providers.find((p) => p.name === name);
    form.setValue("provider", name, { shouldValidate: form.formState.isSubmitted });
    form.setValue("credentials", schemaDefaults(provider?.credential_schema.fields ?? []));
    form.setValue("settings", schemaDefaults(provider?.settings_schema.fields ?? []));
    suggest(name, form.getValues("environment"));
  }

  async function next() {
    if (await form.trigger([...STEP_ONE_FIELDS], { shouldFocus: true })) setStep(2);
  }

  function onSubmit(values: WizardValues) {
    setFormError(null);
    const settings = schemaPayload(settingsFields, values.settings);
    const body: CreateConnectionRequest = {
      provider: values.provider,
      name: normalizeName(values.name),
      environment: values.environment,
      credentials: credentialPayload(credentialFields, values.credentials),
      ...(Object.keys(settings).length > 0 ? { settings } : {}),
    };
    create.mutate(body, {
      onSuccess: (result) => onCreated(result.connection),
      onError: (error) => {
        const placed = applyConnectionErrors(error, form.setError, {
          fieldNames: STEP_ONE_FIELDS,
          sections: { credentials: credentialFields, settings: settingsFields },
        });
        if (placed.fields.some((f) => (STEP_ONE_FIELDS as readonly string[]).includes(f))) setStep(1);
        if (placed.fields.some((f) => f.startsWith("settings."))) setAdvancedOpen(true);
        if (!placed.complete) setFormError(errorMessage(error, strings.connections.createFailed));
      },
    });
  }

  /** Client errors may sit in step 1 or in the collapsed settings; show them. */
  function onInvalid(errors: FieldErrors<WizardValues>) {
    if (STEP_ONE_FIELDS.some((f) => errors[f])) setStep(1);
    if (errors.settings) setAdvancedOpen(true);
  }

  // Steps 1 and 2 share one form so Back keeps what was typed.
  return (
    <WizardFrame
      step={step}
      title={step === 1 ? strings.connections.providerTitle : strings.connections.credentialsTitle}
      description={step === 2 ? strings.connections.credentialsBody : undefined}
    >
      <Form {...form}>
        <form
          onSubmit={(event) => {
            if (step === 1) {
              event.preventDefault();
              void next();
              return;
            }
            void form.handleSubmit(onSubmit, onInvalid)(event);
          }}
          noValidate
          className="grid gap-5"
        >
          {formError && (
            <Alert variant="danger">
              <CircleAlertIcon aria-hidden="true" />
              <AlertDescription>{formError}</AlertDescription>
            </Alert>
          )}

          <fieldset disabled={pending} className={cn("grid gap-5", step !== 1 && "hidden")}>
            <FormField
              control={form.control}
              name="provider"
              render={({ field }) => (
                <FormItem>
                  <FormLabel id={providerLabelId}>{strings.connections.provider}</FormLabel>
                  <FormControl>
                    <RadioGroup
                      aria-labelledby={providerLabelId}
                      value={field.value}
                      onValueChange={chooseProvider}
                      className="grid gap-2 sm:grid-cols-2"
                    >
                      {providers.map((provider) => {
                        const itemId = `${providerLabelId}-${provider.name}`;
                        return (
                          <Label
                            key={provider.name}
                            htmlFor={itemId}
                            className="flex cursor-pointer items-start gap-3 rounded-lg border bg-card p-3 font-normal has-[[data-state=checked]]:border-primary"
                          >
                            <RadioGroupItem id={itemId} value={provider.name} className="mt-0.5" />
                            <span className="grid gap-0.5">
                              <span className="font-medium">{provider.display_name}</span>
                              {provider.description && (
                                <span className="text-xs text-muted-foreground">{provider.description}</span>
                              )}
                            </span>
                          </Label>
                        );
                      })}
                    </RadioGroup>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="environment"
              render={({ field }) => (
                <FormItem>
                  <FormLabel id={environmentLabelId}>{strings.connections.environment}</FormLabel>
                  <FormControl>
                    <RadioGroup
                      aria-labelledby={environmentLabelId}
                      value={field.value}
                      onValueChange={(value) => {
                        field.onChange(value);
                        suggest(form.getValues("provider"), value);
                      }}
                      className="flex flex-wrap gap-x-6 gap-y-2"
                    >
                      {CONNECTION_ENVIRONMENTS.map((env) => {
                        const itemId = `${environmentLabelId}-${env}`;
                        return (
                          <div key={env} className="flex items-center gap-2 max-md:min-h-11">
                            <RadioGroupItem id={itemId} value={env} />
                            <Label htmlFor={itemId}>{strings.environments[env]}</Label>
                          </div>
                        );
                      })}
                    </RadioGroup>
                  </FormControl>
                  <FormDescription>{strings.connections.environmentHint}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.connections.name}</FormLabel>
                  <FormControl>
                    <Input
                      autoComplete="off"
                      autoCapitalize="off"
                      spellCheck={false}
                      className="font-mono"
                      {...field}
                      onChange={(event) => {
                        nameEdited.current = true;
                        field.onChange(event);
                      }}
                    />
                  </FormControl>
                  <FormDescription>{strings.connections.nameHint}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </fieldset>

          {step === 2 && (
            <fieldset disabled={pending} className="grid gap-5">
              <legend className="sr-only">{strings.connections.credentialsTitle}</legend>
              <SchemaFields control={form.control} prefix="credentials" fields={credentialFields} disabled={pending} />
              {settingsFields.length > 0 && (
                <div className="grid gap-4 border-t pt-4">
                  <Button
                    variant="ghost"
                    className="w-fit px-2"
                    aria-expanded={advancedOpen}
                    aria-controls={advancedId}
                    onClick={() => setAdvancedOpen((open) => !open)}
                  >
                    <ChevronDownIcon
                      aria-hidden="true"
                      className={cn("transition-transform", !advancedOpen && "-rotate-90")}
                    />
                    {strings.connections.advancedSettings}
                  </Button>
                  <div id={advancedId} hidden={!advancedOpen} className="grid gap-5">
                    <SchemaFields control={form.control} prefix="settings" fields={settingsFields} disabled={pending} />
                  </div>
                </div>
              )}
            </fieldset>
          )}

          <div className="flex flex-wrap justify-end gap-2">
            {step === 2 && (
              <Button variant="outline" onClick={() => setStep(1)} disabled={pending}>
                {strings.connections.back}
              </Button>
            )}
            {step === 1 ? (
              <Button type="submit">{strings.connections.next}</Button>
            ) : (
              <Button type="submit" disabled={pending || unsupported}>
                {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
                {pending ? strings.connections.creating : strings.connections.createAndTest}
              </Button>
            )}
          </div>
        </form>
      </Form>
    </WizardFrame>
  );
}
